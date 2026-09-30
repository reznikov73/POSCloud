package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Полная уборка: пустые каталоги исчезают, файлы и служебные папки остаются.
func TestPruneEmptyTree(t *testing.T) {
	root := t.TempDir()

	keep := filepath.Join(root, "keep", "a.txt")
	if err := os.MkdirAll(filepath.Dir(keep), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "empty", "deep", "deeper"), 0755); err != nil {
		t.Fatal(err)
	}

	// служебный каталог с точкой трогать нельзя
	blocks := filepath.Join(root, ".blocks")
	if err := os.MkdirAll(blocks, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocks, "aa"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	removed := pruneEmptyTree(root)
	if removed == 0 {
		t.Fatal("ни один пустой каталог не удалён")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("файл в непустом каталоге пострадал")
	}
	if _, err := os.Stat(filepath.Join(root, "keep")); err != nil {
		t.Fatal("каталог с файлом удалён")
	}
	if _, err := os.Stat(filepath.Join(root, "empty")); !os.IsNotExist(err) {
		t.Fatal("пустой скелет каталогов остался")
	}
	if _, err := os.Stat(filepath.Join(blocks, "aa")); err != nil {
		t.Fatal("служебный каталог .blocks пострадал")
	}
}

// Уборка после удаления файла идёт вверх по дереву и останавливается
// перед непустым каталогом.
func TestPruneEmptyDirs(t *testing.T) {
	root := t.TempDir()

	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "x.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Имитируем удаление файла — так же, как это делает обработчик удаления.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	pruneEmptyDirs(root, file)

	if _, err := os.Stat(filepath.Join(root, "a", "b")); !os.IsNotExist(err) {
		t.Fatal("пустые подкаталоги остались")
	}
	if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
		t.Fatal("каталог с другим файлом удалён")
	}
	if _, err := os.Stat(filepath.Join(root, "a", "keep.txt")); err != nil {
		t.Fatal("соседний файл пострадал")
	}
}
