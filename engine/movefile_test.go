package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestMoveFile — обычный перенос работает переименованием.
func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	if b, err := os.ReadFile(dst); err != nil || string(b) != "data" {
		t.Fatalf("содержимое: %q, %v", b, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("исходник не удалён: %v", err)
	}
}

// TestMoveFileFallback — когда переименование запрещено (на Android в общей
// памяти — «operation not permitted»), файл доезжает копированием.
func TestMoveFileFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("снимок"), 0o644); err != nil {
		t.Fatal(err)
	}
	denied := func(string, string) error { return errors.New("operation not permitted") }
	if err := moveFileWith(denied, src, dst); err != nil {
		t.Fatalf("moveFileWith: %v", err)
	}
	if b, err := os.ReadFile(dst); err != nil || string(b) != "снимок" {
		t.Fatalf("содержимое: %q, %v", b, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("исходник не удалён: %v", err)
	}
}
