//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Токен должен пережить шифрование и расшифровку без потерь.
func TestProtectTokenRoundTrip(t *testing.T) {
	const tok = "pcs_abcdefghijklmnopqrstuvwxyz012345"
	enc, err := protectToken(tok)
	if err != nil {
		t.Fatalf("не удалось защитить токен: %v", err)
	}
	if enc == tok {
		t.Fatal("шифротекст совпадает с исходным токеном")
	}
	got, err := unprotectToken(enc)
	if err != nil {
		t.Fatalf("не удалось расшифровать токен: %v", err)
	}
	if got != tok {
		t.Fatalf("после расшифровки получен %q, ожидался %q", got, tok)
	}
}

// Один и тот же токен шифруется по-разному: nonce случайный.
func TestProtectTokenUsesRandomNonce(t *testing.T) {
	const tok = "pcs_same-token-value"
	a, err := protectToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	b, err := protectToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("два шифрования одного токена дали одинаковый результат — nonce не случаен")
	}
}

// Повреждённый шифротекст должен давать ошибку, а не панику.
func TestUnprotectTokenRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"не-base64-%%%",
		"AAAA", // валидный base64, но короче nonce
	}
	for _, c := range cases {
		if _, err := unprotectToken(c); err == nil {
			t.Fatalf("unprotectToken(%q) не вернул ошибку", c)
		}
	}
}

// Пустой токен защищать нечего.
func TestProtectTokenRejectsEmpty(t *testing.T) {
	if _, err := protectToken(""); err == nil {
		t.Fatal("protectToken(\"\") не вернул ошибку")
	}
}

// Ключ должен лежать вне каталога с данными и быть доступен только владельцу.
func TestTokenKeyFilePermissions(t *testing.T) {
	if _, err := loadOrCreateTokenKey(); err != nil {
		t.Fatalf("не удалось создать ключ: %v", err)
	}
	p, err := tokenKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("файл ключа не создан: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("права на файл ключа = %o, ожидались 0600", perm)
	}
	if len(info.Name()) == 0 {
		t.Fatal("пустое имя файла ключа")
	}
	if strings.HasPrefix(p, "/tmp/") {
		t.Fatalf("ключ не должен лежать в /tmp: %s", p)
	}
	if filepath.Dir(p) == "." {
		t.Fatalf("ключ должен лежать в отдельном каталоге: %s", p)
	}
}

// Ключ создаётся один раз и при повторном вызове читается тот же.
func TestTokenKeyIsStable(t *testing.T) {
	a, err := loadOrCreateTokenKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadOrCreateTokenKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("длина ключа = %d и %d, ожидалось 32", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("повторное чтение ключа вернуло другой ключ")
		}
	}
}
