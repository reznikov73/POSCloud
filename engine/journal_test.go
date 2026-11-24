package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// changeEventServer — сервер для теста событий: отдаёт изменения (новый файл,
// удалённый файл), принимает выгрузку и отдаёт скачивание.
func changeEventServer(t *testing.T, newData []byte, newName string) (*httptest.Server, int) {
	t.Helper()
	sum := sha256.Sum256(newData)
	hash := hex.EncodeToString(sum[:])
	deletedData := []byte("old version")
	dsum := sha256.Sum256(deletedData)
	dhash := hex.EncodeToString(dsum[:])
	var mu sync.Mutex
	uploads := map[string]bool{}
	_ = uploads
	mux := http.NewServeMux()
	mux.HandleFunc("/changes", func(w http.ResponseWriter, r *http.Request) {
		changes := []ManifestFile{
			{Name: newName, Size: int64(len(newData)), Hash: hash, Version: 7},
			{Name: "gone.txt", Size: int64(len(deletedData)), Hash: dhash, Version: 5, Deleted: true},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 7, "changes": changes})
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		uploads[r.URL.Query().Get("name")] = true
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": r.URL.Query().Get("name"), "size": 0, "mtime": 1, "hash": "h", "version": 8,
		})
	})
	mux.HandleFunc("/blocks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != newName {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(newData)
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

// TestSyncOnceDeliversChangeEvents — за цикл приёмник получает одну пачку
// событий: выгрузка, скачивание и удаление — по одному событию на файл.
func TestSyncOnceDeliversChangeEvents(t *testing.T) {
	newData := []byte("from another device")
	_, port := changeEventServer(t, newData, "папка/от-сервера.txt")

	dir := t.TempDir()
	// локальный новый файл: уедет на сервер (событие "up")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "мой-файл.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	// удалённый на сервере файл с совпадающим содержимым: молча удалится (событие "delete")
	deleted := []byte("old version")
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), deleted, 0o644); err != nil {
		t.Fatal(err)
	}
	dsum := sha256.Sum256(deleted)
	dhash := hex.EncodeToString(dsum[:])
	st := State{ServerVersion: 5, Files: map[string]StateFile{
		"gone.txt": {Hash: dhash, Size: int64(len(deleted)), MTime: 1, Version: 4},
	}}
	SaveState(dir, st)

	var got []ChangeEvent
	var mu sync.Mutex
	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	s.SetChangeFunc(func(evts []ChangeEvent) {
		mu.Lock()
		got = append(got, evts...)
		mu.Unlock()
	})
	s.SyncOnce(false)

	mu.Lock()
	defer mu.Unlock()
	byOp := map[string]string{}
	for _, e := range got {
		if prev, ok := byOp[e.Op]; ok && prev != e.Name {
			t.Fatalf("операция %s повторилась для разных файлов: %s и %s", e.Op, prev, e.Name)
		}
		byOp[e.Op] = e.Name
	}
	if byOp["up"] != "мой-файл.txt" {
		t.Errorf("событие выгрузки: %+v (все: %+v)", byOp["up"], got)
	}
	if byOp["down"] != "папка/от-сервера.txt" {
		t.Errorf("событие скачивания: %+v (все: %+v)", byOp["down"], got)
	}
	if byOp["delete"] != "gone.txt" {
		t.Errorf("событие удаления: %+v (все: %+v)", byOp["delete"], got)
	}
	for _, e := range got {
		if e.Time <= 0 {
			t.Errorf("событие без времени: %+v", e)
		}
	}
}

// TestSyncOnceNoReceiverNoEvents — без приёмника события просто не собираются,
// цикл работает как раньше.
func TestSyncOnceNoReceiverNoEvents(t *testing.T) {
	newData := []byte("x")
	_, port := changeEventServer(t, newData, "new.txt")
	dir := t.TempDir()
	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	s.SyncOnce(false)
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatalf("файл не скачан: %v", err)
	}
}
