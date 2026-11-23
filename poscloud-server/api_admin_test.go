package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Пароль администратора должен проверяться строго: верный проходит,
// неверный — нет. Ошибка в PBKDF2 однажды уже пропустила любой пароль.
func TestAdminPasswordVerify(t *testing.T) {
	salt, hash, err := hashAdminPassword("Apostol1$")
	if err != nil {
		t.Fatalf("hashAdminPassword: %v", err)
	}
	st := &adminStore{Salt: salt, Hash: hash}

	if !st.verify("Apostol1$") {
		t.Fatal("верный пароль отвергнут")
	}
	for _, wrong := range []string{"", "apostol1$", "Apostol1", "неверный", "Apostol1$$"} {
		if st.verify(wrong) {
			t.Fatalf("неверный пароль %q принят", wrong)
		}
	}
}

// Один пароль с разными солями даёт разные хеши, но оба проверяются.
func TestAdminPasswordUsesSalt(t *testing.T) {
	s1, h1, err := hashAdminPassword("парольТест1")
	if err != nil {
		t.Fatal(err)
	}
	s2, h2, err := hashAdminPassword("парольТест1")
	if err != nil {
		t.Fatal(err)
	}
	if s1 == s2 {
		t.Fatal("соль повторяется — она должна быть случайной")
	}
	if h1 == h2 {
		t.Fatal("хеши совпали при разных солях — соль не участвует в расчёте")
	}
	if !(&adminStore{Salt: s1, Hash: h1}).verify("парольТест1") {
		t.Fatal("первый хеш не проверяется")
	}
	if !(&adminStore{Salt: s2, Hash: h2}).verify("парольТест1") {
		t.Fatal("второй хеш не проверяется")
	}
}

// Хеш не должен быть обратимым: в нём нет открытого пароля.
func TestAdminHashIsNotPlaintext(t *testing.T) {
	const pw = "секретныйПароль42"
	salt, hash, err := hashAdminPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if hash == pw {
		t.Fatal("хеш совпал с паролем")
	}
	if len(hash) != 64 {
		t.Fatalf("длина хеша %d, ожидалось 64 hex-символа (SHA-256)", len(hash))
	}
	if salt == "" {
		t.Fatal("соль пуста")
	}
}

// loadAdmin создаёт файл при первом запуске и не перезаписывает существующий.
func TestAdminStorePersistence(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	a.dataRoot = dir
	a.rootOnce.Do(func() {}) // не пересчитывать каталог

	p := a.adminPath()
	if filepath.Dir(p) != dir {
		t.Fatalf("admin.json должен лежать в каталоге данных: %s", p)
	}

	st, err := a.loadAdmin()
	if err != nil {
		t.Fatalf("loadAdmin: %v", err)
	}
	if !st.verify(defaultAdminPW) {
		t.Fatal("пароль по умолчанию не проходит проверку")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("файл admin.json не создан: %v", err)
	}
	// Права 0600: хеш не должен читаться другими пользователями.
	// Windows не хранит POSIX-права: os.Chmod там переключает только признак
	// «только чтение», а os.Stat всегда отдаёт 0666. Проверяем на Unix.
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(p); err == nil {
			if perm := fi.Mode().Perm(); perm != 0600 {
				t.Fatalf("права на admin.json = %o, ожидались 0600", perm)
			}
		}
	}

	// Смена пароля и повторное чтение.
	salt, hash, err := hashAdminPassword("новыйПароль99")
	if err != nil {
		t.Fatal(err)
	}
	st.Salt, st.Hash, st.NotFirst = salt, hash, true
	if err := st.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	st2, err := a.loadAdmin()
	if err != nil {
		t.Fatal(err)
	}
	if !st2.verify("новыйПароль99") {
		t.Fatal("новый пароль не сохранился")
	}
	if st2.verify(defaultAdminPW) {
		t.Fatal("старый пароль по умолчанию всё ещё работает")
	}
	if !st2.NotFirst {
		t.Fatal("признак смены пароля потерян")
	}
}
