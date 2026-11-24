package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestStorageStatsLocalOnly — без токена считаются только локальные цифры:
// скрытое пропускается, файлы в корне — отдельной строкой в конце.
func TestStorageStatsLocalOnly(t *testing.T) {
	dir := t.TempDir()
	writeStorageFile(t, dir, "документы/a.txt", 100)
	writeStorageFile(t, dir, "фото/b.jpg", 50)
	writeStorageFile(t, dir, "root.txt", 10)
	writeStorageFile(t, dir, ".hidden/x", 500)
	writeStorageFile(t, dir, ".poscloud-state.json", 300)

	c := NewController(t.TempDir())
	c.SaveSettings("127.0.0.1", 1, dir, 3, "", false, "", "", false, "", "", 0)

	st := c.StorageStats()
	if st.LocalFiles != 3 || st.LocalBytes != 160 {
		t.Fatalf("локально: файлов %d, байт %d, ожидалось 3/160", st.LocalFiles, st.LocalBytes)
	}
	if !errors.Is(st.ServerErr, ErrNoServer) {
		t.Fatalf("без токена ожидался ErrNoServer, получено: %v", st.ServerErr)
	}
	if st.ServerFiles != 0 || st.ServerBytes != 0 {
		t.Fatalf("без токена серверных цифр быть не должно: %+v", st)
	}
	if len(st.Folders) != 3 {
		t.Fatalf("строк разбивки: %d (%+v), ожидалось 3", len(st.Folders), st.Folders)
	}
	// по убыванию размера: документы 100, фото 50, корень 10
	if st.Folders[0].Path != "документы" || st.Folders[1].Path != "фото" {
		t.Fatalf("порядок папок: %+v", st.Folders)
	}
	root := st.Folders[2]
	if !root.Root || root.Path != "" || root.LocFiles != 1 || root.LocBytes != 10 {
		t.Fatalf("строка корня: %+v", root)
	}
}

// TestStorageStatsWithServer — серверные цифры берутся из манифеста без
// удалённых и папок и складываются с локальными в тех же строках разбивки.
func TestStorageStatsWithServer(t *testing.T) {
	dir := t.TempDir()
	writeStorageFile(t, dir, "документы/a.txt", 100)
	writeStorageFile(t, dir, "фото/b.jpg", 50)

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":5,"files":[
			{"name":"документы/a.txt","size":100,"version":1},
			{"name":"фото/big.jpg","size":1000,"version":2},
			{"name":"old.txt","size":5000,"version":3,"deleted":true},
			{"name":"документы","dir":true,"version":4}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	c := NewController(t.TempDir())
	c.SaveSettings("127.0.0.1", port, dir, 3, "t", false, "", "", false, "", "", 0)

	st := c.StorageStats()
	if st.ServerErr != nil {
		t.Fatal(st.ServerErr)
	}
	if st.ServerFiles != 2 || st.ServerBytes != 1100 {
		t.Fatalf("сервер: файлов %d, байт %d, ожидалось 2/1100", st.ServerFiles, st.ServerBytes)
	}
	want := []StorageFolder{
		{Path: "фото", Files: 1, Bytes: 1000, LocFiles: 1, LocBytes: 50},
		{Path: "документы", Files: 1, Bytes: 100, LocFiles: 1, LocBytes: 100},
	}
	if len(st.Folders) != 2 {
		t.Fatalf("строк разбивки: %d (%+v)", len(st.Folders), st.Folders)
	}
	for i, w := range want {
		if st.Folders[i] != w {
			t.Fatalf("строка %d: %+v, ожидалась %+v", i, st.Folders[i], w)
		}
	}

	// сервер недоступен: ошибка возвращается, локальные цифры — тоже
	srv.Close()
	st = c.StorageStats()
	if st.ServerErr == nil || errors.Is(st.ServerErr, ErrNoServer) {
		t.Fatalf("без сервера ожидалась ошибка связи, получено: %v", st.ServerErr)
	}
	if st.LocalFiles != 2 || st.LocalBytes != 150 || len(st.Folders) != 2 {
		t.Fatalf("без сервера локальные цифры должны возвращаться: %+v", st)
	}
}

func TestStorageStatsNoDir(t *testing.T) {
	c := NewController(t.TempDir())
	st := c.StorageStats()
	if !errors.Is(st.ServerErr, ErrNoServer) || st.LocalFiles != 0 || len(st.Folders) != 0 {
		t.Fatalf("без папки: %+v", st)
	}
}

func writeStorageFile(t *testing.T, dir, rel string, n int) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0644); err != nil {
		t.Fatal(err)
	}
}
