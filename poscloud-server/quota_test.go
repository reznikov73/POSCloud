package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsageBytesIncludesHistory(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "andrey")
	_ = os.MkdirAll(userDir, 0755)
	_ = os.WriteFile(filepath.Join(userDir, "a.txt"), make([]byte, 100), 0644)
	if err := snapshotUserFile(userDir, "a.txt", "version", 1, 100, 1, "h"); err != nil {
		t.Fatal(err)
	}
	used := usageBytes(userDir)
	if used < 200 {
		t.Fatalf("usageBytes=%d, ожидалось >=200 (файл + история)", used)
	}
}

func TestCheckQuota(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "andrey")
	_ = os.MkdirAll(userDir, 0755)
	_ = os.WriteFile(filepath.Join(userDir, "a.txt"), make([]byte, 10), 0644)
	a := &App{srvDir: root, quotaGB: 0}
	if err := a.checkQuota(userDir, 1<<40); err != nil {
		t.Fatalf("без квоты ошибка: %v", err)
	}
	a.quotaGB = 0
	if err := a.checkQuota(userDir, 1); err != nil {
		t.Fatalf("нулевая квота должна быть безлимитной: %v", err)
	}
}
