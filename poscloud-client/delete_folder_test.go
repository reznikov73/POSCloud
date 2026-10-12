package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
)

// TestDeleteServerFolder проверяет «Удалить с сервера»: удаляются только файлы
// выбранной папки, локальная копия и состояние чистятся, правила убираются
// вместе с вложенными, чужие правила и файлы остаются.
func TestDeleteServerFolder(t *testing.T) {
	var mu sync.Mutex
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/manifest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": 7,
				"files": []map[string]any{
					{"name": "Тест/а.txt", "size": 10},
					{"name": "Тест/б.txt", "size": 20},
					{"name": "Тест2/в.txt", "size": 30},
					{"name": "file.txt", "size": 1},
				},
			})
		case r.Method == "DELETE" && r.URL.Path == "/delete":
			mu.Lock()
			deleted = append(deleted, r.URL.Query().Get("name"))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Тест"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Тест", "а.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	st := clientState{Files: map[string]stateFile{
		"Тест/а.txt":  {Version: 1},
		"Тест2/в.txt": {Version: 2},
	}}
	saveState(dir, st)

	a := &App{}
	a.localDir = dir
	a.host = u.Hostname()
	a.port = port
	a.token = "pcs_test"
	a.folderRules = []FolderRule{
		{Path: "Тест", Mode: folderOff},
		{Path: "Тест/Подпапка", Mode: folderUp},
		{Path: "Тест2", Mode: folderDown},
	}

	n, err := a.DeleteServerFolder("Тест")
	if err != nil {
		t.Fatalf("DeleteServerFolder: %v", err)
	}
	if n != 2 {
		t.Fatalf("удалено файлов: %d, ожидалось 2", n)
	}
	mu.Lock()
	got := append([]string(nil), deleted...)
	mu.Unlock()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "Тест/а.txt" || got[1] != "Тест/б.txt" {
		t.Fatalf("запросы на удаление: %v, ожидались только тестовые файлы", got)
	}

	if _, err := os.Stat(filepath.Join(dir, "Тест", "а.txt")); !os.IsNotExist(err) {
		t.Fatalf("локальный файл не удалён (err=%v)", err)
	}
	st2 := loadState(dir)
	if _, ok := st2.Files["Тест/а.txt"]; ok {
		t.Fatalf("состояние «Тест/» не очищено")
	}
	if _, ok := st2.Files["Тест2/в.txt"]; !ok {
		t.Fatalf("состояние «Тест2/» не должно было очищаться")
	}
	if len(a.folderRules) != 1 || a.folderRules[0].Path != "Тест2" {
		t.Fatalf("правила после удаления: %+v", a.folderRules)
	}
}
