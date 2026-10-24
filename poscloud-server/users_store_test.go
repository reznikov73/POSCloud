package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateUsersStore(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy-users.json")
	dst := filepath.Join(dir, "data", "users.json")
	if err := os.WriteFile(legacy, []byte(`{"users":[{"id":"andrey"}]}`), 0644); err != nil {
		t.Fatal(err)
	}

	moved, err := migrateUsersStore(legacy, dst)
	if err != nil {
		t.Fatalf("migrateUsersStore: %v", err)
	}
	if !moved {
		t.Fatal("ожидался перенос users.json в dataDir")
	}
	b, err := os.ReadFile(dst)
	if err != nil || !strings.Contains(string(b), "andrey") {
		t.Fatalf("перенесённый файл не содержит пользователя: %v, %q", err, string(b))
	}

	// Существующий файл в dataDir не перезаписывается.
	if err := os.WriteFile(dst, []byte(`{"users":[{"id":"new"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	moved, err = migrateUsersStore(legacy, dst)
	if err != nil {
		t.Fatalf("повторный migrateUsersStore: %v", err)
	}
	if moved {
		t.Fatal("существующий users.json не должен перезаписываться")
	}
	b, _ = os.ReadFile(dst)
	if !strings.Contains(string(b), "new") {
		t.Fatal("существующий users.json был изменён")
	}
}
