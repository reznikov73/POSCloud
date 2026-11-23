package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// tombstoneServer — сервер, который отдаёт изменения для одного файла:
// либо надгробие (deleted), либо живую версию, и принимает загрузки.
func tombstoneServer(t *testing.T, name string, data []byte, deleted bool) (*httptest.Server, int) {
	t.Helper()
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/changes", func(w http.ResponseWriter, r *http.Request) {
		changes := []ManifestFile{{
			Name:    name,
			Size:    int64(len(data)),
			Hash:    hash,
			Version: 7,
			Deleted: deleted,
		}}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 7, "changes": changes})
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		body, _ := os.CreateTemp(t.TempDir(), "up-*")
		defer body.Close()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    r.URL.Query().Get("name"),
			"size":    0,
			"mtime":   1,
			"hash":    hash,
			"version": 8,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("адрес тестового сервера: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("порт тестового сервера: %v", err)
	}
	return srv, port
}

// TestSyncTombstoneDeletesRecreatedFile — файл появился заново уже после того,
// как его удалили на другом устройстве, и содержимое совпадает с удалённой
// версией: движок удаляет его молча и НЕ делает конфликтную копию. Иначе
// удалённые на компьютере снимки экрана возвращались бы с телефона.
func TestSyncTombstoneDeletesRecreatedFile(t *testing.T) {
	data := []byte("screenshot bytes")
	name := "Снимки экрана/Screenshot_1.jpg"
	dir := t.TempDir()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, port := tombstoneServer(t, name, data, true)

	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	s.SyncOnce(false)

	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("файл не удалён: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(full))
	for _, e := range entries {
		if strings.Contains(e.Name(), "конфликт") {
			t.Fatalf("создана конфликтная копия: %s", e.Name())
		}
	}
	if st := LoadState(dir); len(st.Files) != 0 {
		t.Fatalf("в состоянии остались записи: %v", st.Files)
	}
}

// TestSyncTombstoneKeepsChangedFile — если содержимое другое (файл действительно
// правили), прежнее поведение сохраняется: локальная копия уезжает в конфликтную
// и выгружается, данные не теряются.
func TestSyncTombstoneKeepsChangedFile(t *testing.T) {
	deletedData := []byte("old version")
	name := "Снимки экрана/Screenshot_2.jpg"
	dir := t.TempDir()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("edited on this device"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, port := tombstoneServer(t, name, deletedData, true)

	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	s.SyncOnce(false)

	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("исходное имя должно было уйти в конфликтную копию: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(full))
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "конфликт") {
			found = true
		}
	}
	if !found {
		t.Fatal("правка потерялась: конфликтная копия не создана")
	}
}
