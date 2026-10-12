package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Эти тесты проверяют устойчивость реестра пользователей (users.json)
// к внезапному выключению ПК: обрыв записи не должен приводить к потере
// всех заведённых пользователей.

func TestUsersSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s := newAuthStore(path)
	if _, err := s.createUser("Андрей"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createUser("Сергей"); err != nil {
		t.Fatal(err)
	}
	s2 := newAuthStore(path)
	if got := len(s2.list()); got != 2 {
		t.Fatalf("после перезапуска ожидалось 2 пользователя, получено %d", got)
	}
}

func TestUsersSurviveCorruptMain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s := newAuthStore(path)
	if _, err := s.createUser("Андрей"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createUser("Сергей"); err != nil {
		t.Fatal(err)
	}
	// Обрыв записи при сбое питания: файл остался обрезанным.
	if err := os.WriteFile(path, []byte("{\"users\": [{\"id\": \"and"), 0600); err != nil {
		t.Fatal(err)
	}
	s2 := newAuthStore(path)
	got := len(s2.list())
	t.Logf("пользователей после перезапуска: %d", got)
	if got == 0 {
		t.Fatalf("ПОТЕРЯ: все пользователи пропали — повреждённый users.json прочитан как пустой реестр")
	}
}

func TestUsersSurviveEmptyMain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s := newAuthStore(path)
	if _, err := s.createUser("Андрей"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createUser("Сергей"); err != nil {
		t.Fatal(err)
	}
	// Классический след сбоя питания: файл остался нулевой длины.
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s2 := newAuthStore(path)
	got := len(s2.list())
	t.Logf("пользователей после перезапуска: %d", got)
	if got == 0 {
		t.Fatalf("ПОТЕРЯ: пустой users.json — пользователи пропали без попытки восстановления")
	}
}

func TestCorruptFilePreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = newAuthStore(path)
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) == 0 {
		t.Fatalf("повреждённый файл не сохранён в сторону: его затёрла бы первая же запись")
	}
}

func TestBackupKeepsPreviousState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s := newAuthStore(path)
	if _, err := s.createUser("Андрей"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createUser("Сергей"); err != nil {
		t.Fatal(err)
	}
	bak, ok, _ := readUsersFile(path + ".bak")
	if !ok || len(bak) != 1 || bak[0].Name != "Андрей" {
		t.Fatalf("резервная копия должна хранить предыдущее состояние: ok=%v len=%d", ok, len(bak))
	}
	main, ok, _ := readUsersFile(path)
	if !ok || len(main) != 2 {
		t.Fatalf("основной файл должен содержать актуальное состояние: ok=%v len=%d", ok, len(main))
	}
}

func TestRecoveredMainFileIsRepairedOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s := newAuthStore(path)
	if _, err := s.createUser("Андрей"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createUser("Сергей"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	s2 := newAuthStore(path)
	users, ok, _ := readUsersFile(path)
	if !ok || len(users) != len(s2.list()) || len(users) == 0 {
		t.Fatalf("после восстановления основной файл должен быть исправен: ok=%v len=%d", ok, len(users))
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("повреждённый файл должен быть сохранён в сторону (найдено %d)", len(matches))
	}
}

func TestCorruptWithoutBackupStartsClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newAuthStore(path)
	if got := len(s.list()); got != 0 {
		t.Fatalf("без резервной копии реестр пуст, получено %d", got)
	}
	if users, ok, _ := readUsersFile(path); !ok || len(users) != 0 {
		t.Fatalf("основной файл должен остаться исправным (пустым): ok=%v len=%d", ok, len(users))
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("повреждённый файл должен быть сохранён в сторону (найдено %d)", len(matches))
	}
}

func TestLastSeenWriteDeferredUntilFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s := newAuthStore(path)
	u, err := s.createUser("Андрей")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.issueToken(u.ID, "ПК")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	uid, _, ok := s.findUserByToken(tok)
	if !ok || uid != u.ID {
		t.Fatal("токен не распознан")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("обращение с токеном не должно переписывать users.json — запись откладывается")
	}
	u2, _ := s.get(u.ID)
	if len(u2.Tokens) != 1 || u2.Tokens[0].LastSeen == "" {
		t.Fatal("LastSeen должен обновляться в памяти сразу")
	}
	s.flush()
	after2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after2) {
		t.Fatal("после flush изменённый LastSeen должен попасть на диск")
	}
}

func TestAtomicReplaceFileLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	if err := atomicReplaceFile(p, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplaceFile(p, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "two" {
		t.Fatalf("файл должен содержать последнюю версию, err=%v", err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("временный файл после подмены должен быть удалён")
	}
}
