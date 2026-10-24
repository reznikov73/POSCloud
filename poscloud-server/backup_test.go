package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRoundTrip(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(data, "users.json")
	auth := newAuthStore(authPath)
	u, err := auth.createUser("Дом")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.issueToken(u.ID, "PC"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(savedConfig{Port: 8099, DataDir: data})
	if err := os.WriteFile(filepath.Join(root, "server.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{srvDir: data, auth: auth}
	a.rootOnce.Do(func() { a.dataRoot = root })

	backupPath := filepath.Join(root, "backup.zip")
	if err := a.writeBackup(backupPath); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(backupPath); err != nil || fi.Size() == 0 {
		t.Fatalf("backup missing: %v", err)
	}

	// Меняем реестр, затем восстанавливаем копию.
	u2, _ := auth.createUser("Второй")
	if _, err := auth.issueToken(u2.ID, "OTHER"); err != nil {
		t.Fatal(err)
	}
	res := a.restoreBackup(backupPath)
	if res.Error != "" {
		t.Fatalf("restore error: %s", res.Error)
	}
	restored := newAuthStore(authPath)
	if _, ok := restored.get(u.ID); !ok {
		t.Fatal("restored user missing")
	}
	if _, ok := restored.get(u2.ID); ok {
		t.Fatal("user created after backup survived restore")
	}
	restored.stopFlusher()
	if res.SafetyPath == "" {
		t.Fatal("safety backup path is empty")
	}
	if _, err := os.Stat(filepath.Join(data, "users.json")); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRejectsUnknownZipEntry(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "bad.zip")
	if err := os.WriteFile(backupPath, []byte("not a zip"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBackup(backupPath); err == nil {
		t.Fatal("invalid archive accepted")
	}
}
