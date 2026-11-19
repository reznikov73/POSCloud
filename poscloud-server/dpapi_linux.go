//go:build linux

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ---------- защита токенов на Linux ----------
//
// В Windows токены шифруются DPAPI: ключ привязан к пользователю и хранится
// в системе, приложению его знать не нужно. В Linux такого единого механизма нет.
//
// Варианты были такие:
//   - libsecret / Secret Service через D-Bus — правильный путь, но требует
//     запущенного gnome-keyring/kwallet; на headless-машине и в контейнере
//     сервиса нет, и сервер не смог бы выдать ни одного токена;
//   - не хранить токен вовсе — но интерфейс показывает токен повторно
//     («список устройств»), это существующее поведение;
//   - AES-256-GCM с ключом в файле 0600 рядом с настройками — выбрано.
//
// Компромисс осознанный: ключ лежит рядом с данными, поэтому защита — от
// случайного чтения файла users.json, а не от процесса под тем же пользователем.
// В Windows защита сильнее (ключ вне каталога приложения).

// tokenKeyFileName — файл с ключом шифрования токенов.
const tokenKeyFileName = "token.key"

// tokenKeyPath возвращает путь к файлу ключа.
func tokenKeyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", herr
		}
		dir = filepath.Join(home, ".config")
	}
	// Ключ кладём в каталог приложения в конфигурации пользователя:
	// он не должен лежать рядом с exe (каталог может быть общим для всех).
	return filepath.Join(dir, "poscloud-server", tokenKeyFileName), nil
}

// loadOrCreateTokenKey читает ключ, создавая его при первом запуске.
func loadOrCreateTokenKey() ([]byte, error) {
	p, err := tokenKeyPath()
	if err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(p); err == nil && len(b) == 32 {
		return b, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("не удалось создать ключ: %w", err)
	}
	// 0600: ключ читает только владелец.
	if err := os.WriteFile(p, key, 0600); err != nil {
		return nil, err
	}
	// WriteFile не меняет права существующего файла — выставляем явно.
	_ = os.Chmod(p, 0600)
	return key, nil
}

// protectToken шифрует токен ключом пользователя (AES-256-GCM).
func protectToken(token string) (string, error) {
	if token == "" {
		return "", errors.New("пустой токен")
	}
	key, err := loadOrCreateTokenKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	// nonce дописывается в начало шифротекста: для расшифровки он нужен рядом.
	sealed := gcm.Seal(nonce, nonce, []byte(token), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// unprotectToken расшифровывает токен, сохранённый protectToken.
func unprotectToken(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("некорректный шифротекст токена: %w", err)
	}
	key, err := loadOrCreateTokenKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("шифротекст токена короче nonce")
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", errors.New("не удалось расшифровать токен: ключ не подходит")
	}
	return string(plain), nil
}
