package client

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer — a minimal emulation of the POSCloud server's HTTP API: enough
// for the engine to perform a full sync cycle (reconciliation, upload, state).
type fakeServer struct {
	mu      sync.Mutex
	version int64
	files   map[string]map[string]any // name -> {size, mtime, hash, version, dir}
	uploads []string
}

func newFakeServer() *fakeServer {
	return &fakeServer{files: map[string]map[string]any{}}
}

func (s *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/changes", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"version": s.version, "changes": []any{}})
	})
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		list := make([]map[string]any, 0, len(s.files))
		for _, f := range s.files {
			list = append(list, f)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": s.version, "files": list})
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		data := make([]byte, 0, 4096)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			data = append(data, buf[:n]...)
			if err != nil {
				break
			}
		}
		s.mu.Lock()
		s.version++
		rec := map[string]any{
			"name": name, "size": len(data), "mtime": time.Now().Unix(),
			"hash": fmt.Sprintf("h%d", len(data)), "version": s.version,
		}
		s.files[name] = rec
		s.uploads = append(s.uploads, name)
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(rec)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/blocks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"blocks": []any{}, "size": 0})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // without SSE the engine simply works on a timer
	})
	mux.HandleFunc("/mkdir", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.version++
		name := r.URL.Query().Get("name")
		s.files[name] = map[string]any{"name": name, "dir": true, "version": s.version}
		v := s.version
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"version": v})
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		name := r.URL.Query().Get("name")
		delete(s.files, name)
		s.version++
		s.mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "test user"})
	})
	return mux
}

func (s *fakeServer) uploaded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploads...)
}

// TestSyncEndToEnd spins up a fake server, starts synchronization
// through the controller and verifies that the file reaches the server, the status shows
// online, and the log has an upload entry.
func TestSyncEndToEnd(t *testing.T) {
	srv := newFakeServer()
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello, POSCloud"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewController(t.TempDir())
	if err := c.Start("127.0.0.1", port, dir, 1, "test-token", false, ""); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	// wait for the first sync cycle
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		st := c.Status()
		if st.Online && st.LastSync != "" {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	st := c.Status()
	if !st.Online {
		t.Fatalf("no connection to the server: %+v", st)
	}
	if st.LastSync == "" {
		t.Fatalf("synchronization did not run: %+v", st)
	}
	if st.Count < 1 {
		t.Fatalf("file did not make it into the state: %+v", st)
	}
	ups := srv.uploaded()
	if !containsStr(strings.Join(ups, ","), "hello.txt") {
		t.Fatalf("file did not reach the server, uploads: %v", ups)
	}

	// the log contains entries about the start and the upload
	logs := c.Logs()
	joined := strings.Join(logs, "\n")
	if !containsStr(joined, "синхронизация запущена") {
		t.Fatalf("the log has no entry about the start:\n%s", joined)
	}
	if !containsStr(joined, "вверх: hello.txt") {
		t.Fatalf("the log has no entry about the upload:\n%s", joined)
	}
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
