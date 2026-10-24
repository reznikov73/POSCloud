package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistorySnapshotAndPrune(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "andrey")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(userDir, "note.txt")
	if err := os.WriteFile(file, []byte("version-1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshotUserFile(userDir, "note.txt", "version", 1, 9, 1, "h1"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := os.WriteFile(file, []byte("version-2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshotUserFile(userDir, "note.txt", "version", 2, 9, 2, "h2"); err != nil {
		t.Fatalf("snapshot2: %v", err)
	}
	items := loadHistory(userDir)
	if len(items) != 2 {
		t.Fatalf("ожидалось 2 версии, получено %d", len(items))
	}
	if items[0].Version != 2 || items[1].Version != 1 {
		t.Fatalf("неверный порядок версий: %+v", items)
	}
	for _, it := range items {
		if _, err := os.Stat(historySnapshotPath(userDir, it.ID)); err != nil {
			t.Fatalf("снимок %s: %v", it.ID, err)
		}
	}

	// Больше 10 версий — старые удаляются.
	for i := 3; i <= 12; i++ {
		_ = os.WriteFile(file, []byte("next"), 0644)
		if err := snapshotUserFile(userDir, "note.txt", "version", int64(i), 4, int64(i), "h"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(loadHistory(userDir)); n != 10 {
		t.Fatalf("после prune ожидалось 10 версий, получено %d", n)
	}
}

func TestTrashSnapshotRoundTrip(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "andrey")
	_ = os.MkdirAll(userDir, 0755)
	file := filepath.Join(userDir, "old.txt")
	_ = os.WriteFile(file, []byte("old"), 0644)
	if err := snapshotUserFile(userDir, "old.txt", "trash", 5, 3, 1, "h"); err != nil {
		t.Fatal(err)
	}
	items := loadHistory(userDir)
	if len(items) != 1 || items[0].Kind != "trash" {
		t.Fatalf("корзина не создана: %+v", items)
	}
	if _, err := os.Stat(historySnapshotPath(userDir, items[0].ID)); err != nil {
		t.Fatal(err)
	}
}
