package engine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConflictNameReplacesSuffix — повторный конфликт не достраивает второй
// суффикс: имя остаётся читаемым и разбирается как одна копия.
func TestConflictNameReplacesSuffix(t *testing.T) {
	stamp := time.Date(2026, 11, 24, 10, 11, 12, 0, time.Local).Format(conflictStampLayout)
	nested := "папка/заметка (конфликт " + stamp + ").md"
	cf := ConflictName(nested)
	if strings.Count(cf, conflictMark) != 1 {
		t.Fatalf("суффикс задвоен: %q", cf)
	}
	if orig, ok := ConflictOriginalName(cf); !ok || orig != "папка/заметка.md" {
		t.Fatalf("разбор %q: %q, %v", cf, orig, ok)
	}
}

// TestConflictOriginalNameStrict — разбор суффикса « (конфликт …)» строгий:
// правильная метка времени распознаётся, слово «конфликт» в обычном имени — нет.
func TestConflictOriginalNameStrict(t *testing.T) {
	cases := []struct {
		name   string
		orig   string
		isConf bool
	}{
		{"docs/report (конфликт 2026-11-24_12-00-00).docx", "docs/report.docx", true},
		{"README (конфликт 2026-01-02_03-04-05)", "README", true},
		{"a.b.c (конфликт 2026-01-02_03-04-05).txt", "a.b.c.txt", true},
		{"Протокол конфликта.docx", "", false},
		{"report (конфликт не-дата).docx", "", false},
		{"report.docx", "", false},
		{"(конфликт 2026-01-02_03-04-05)", "", false},
	}
	for _, c := range cases {
		orig, ok := ConflictOriginalName(c.name)
		if ok != c.isConf || orig != c.orig {
			t.Errorf("ConflictOriginalName(%q) = %q, %v; хочу %q, %v", c.name, orig, ok, c.orig, c.isConf)
		}
	}
	// круг: имя, собранное ConflictName-подобным суффиксом, разбирается обратно
	stamp := time.Date(2026, 11, 24, 10, 11, 12, 0, time.Local).Format(conflictStampLayout)
	cf := "папка/заметка (конфликт " + stamp + ").md"
	orig, ok := ConflictOriginalName(cf)
	if !ok || orig != "папка/заметка.md" {
		t.Errorf("обратный разбор: %q, %v", orig, ok)
	}
}

// conflictTestServer — сервер, принимающий выгрузку и удаление, и запоминающий
// вызовы (имя -> сколько раз).
func conflictTestServer(t *testing.T) (*httptest.Server, int) {
	t.Helper()
	var mu sync.Mutex
	uploads := map[string]int{}
	deletes := map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		uploads[r.URL.Query().Get("name")]++
		n := uploads[r.URL.Query().Get("name")]
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    r.URL.Query().Get("name"),
			"size":    0,
			"mtime":   1,
			"hash":    "abc",
			"version": 100 + int64(n),
		})
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deletes[r.URL.Query().Get("name")]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
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
	t.Cleanup(func() {
		mu.Lock()
		t.Logf("вызовы сервера: выгрузки %v, удаления %v", uploads, deletes)
		mu.Unlock()
	})
	return srv, port
}

// TestConflictsList — список конфликтных пар: только файлы с суффиксом,
// скрытые не считаются, Uploaded видно по состоянию.
func TestConflictsList(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, data string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cf := "docs/report (конфликт 2026-11-24_10-00-00).docx"
	write("docs/report.docx", "server version")
	write(cf, "local edit")
	write("docs/обычный.txt", "not a conflict")
	write("Протокол конфликта.docx", "user file with word conflict")
	SaveState(dir, State{ServerVersion: 5, Files: map[string]StateFile{
		cf: {Hash: "h", Version: 8},
	}})

	s := NewSyncer(Config{Host: "x", Port: 1, Dir: dir, Token: "t"}, nil, nil)
	list := s.Conflicts()
	if len(list) != 1 {
		t.Fatalf("ожидалась одна конфликтная копия, получено %d: %v", len(list), list)
	}
	p := list[0]
	if p.Conflict != cf || p.Original != "docs/report.docx" {
		t.Errorf("пара: %+v", p)
	}
	if !p.Uploaded {
		t.Error("копия с записью состояния (версия 8) должна быть Uploaded")
	}
	if p.Size != int64(len("local edit")) {
		t.Errorf("размер: %d", p.Size)
	}
}

// TestResolveConflictKeepLocal — «моя версия»: копия встаёт на место оригинала,
// выгружается на сервер, копия удаляется с сервера и из состояния.
func TestResolveConflictKeepLocal(t *testing.T) {
	_, port := conflictTestServer(t)
	dir := t.TempDir()
	cf := "report (конфликт 2026-11-24_10-00-00).txt"
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("server version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cf), []byte("my edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	SaveState(dir, State{ServerVersion: 5, Files: map[string]StateFile{
		"report.txt": {Hash: "hs", Version: 5},
		cf:           {Hash: "hl", Version: 6},
	}})

	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	if err := s.ResolveConflictKeepLocal(cf); err != nil {
		t.Fatalf("разрешение: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "report.txt")); string(b) != "my edit" {
		t.Errorf("под именем оригинала должна лежать локальная версия, там %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, cf)); !os.IsNotExist(err) {
		t.Error("конфликтная копия должна быть удалена")
	}
	st := LoadState(dir)
	if _, ok := st.Files[cf]; ok {
		t.Error("запись состояния копии должна быть удалена")
	}
	if st.Files["report.txt"].Version == 0 {
		t.Errorf("у оригинала должна быть версия выгрузки: %+v", st.Files["report.txt"])
	}
}

// TestResolveConflictKeepServer — «серверная»: копия удаляется локально,
// оригинал не трогается.
func TestResolveConflictKeepServer(t *testing.T) {
	_, port := conflictTestServer(t)
	dir := t.TempDir()
	cf := "report (конфликт 2026-11-24_10-00-00).txt"
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("server version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cf), []byte("my edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	SaveState(dir, State{ServerVersion: 5, Files: map[string]StateFile{
		"report.txt": {Hash: "hs", Version: 5},
		cf:           {Hash: "hl", Version: 6},
	}})

	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	if err := s.ResolveConflictKeepServer(cf); err != nil {
		t.Fatalf("разрешение: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cf)); !os.IsNotExist(err) {
		t.Error("конфликтная копия должна быть удалена")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "report.txt")); string(b) != "server version" {
		t.Errorf("оригинал не должен меняться, там %q", b)
	}
	if _, ok := LoadState(dir).Files[cf]; ok {
		t.Error("запись состояния копии должна быть удалена")
	}
}

// TestResolveConflictKeepLocalOffline — без связи «моя версия» не применяется:
// файл возвращается на место копии, конфликт остаётся в списке.
func TestResolveConflictKeepLocalOffline(t *testing.T) {
	srv, port := conflictTestServer(t)
	srv.Close() // порт закрыт: выгрузка не удастся
	_ = port
	dir := t.TempDir()
	cf := "report (конфликт 2026-11-24_10-00-00).txt"
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("server version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cf), []byte("my edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	SaveState(dir, State{ServerVersion: 5, Files: map[string]StateFile{
		"report.txt": {Hash: "hs", Version: 5},
	}})

	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	if err := s.ResolveConflictKeepLocal(cf); err == nil {
		t.Fatal("без связи должно вернуться ошибку")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, cf)); string(b) != "my edit" {
		t.Errorf("копия должна остаться на месте, там %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "report.txt")); string(b) != "server version" {
		t.Errorf("оригинал не должен меняться, там %q", b)
	}
	if got := len(s.Conflicts()); got != 1 {
		t.Errorf("конфликт должен остаться в списке, в списке %d", got)
	}
}

// TestResolveConflictKeepServerOffline — «серверная» без связи работает
// локально: копия удалена, оригинал на месте, ошибки нет (копию на сервере
// движок удалит при следующем цикле).
func TestResolveConflictKeepServerOffline(t *testing.T) {
	srv, port := conflictTestServer(t)
	srv.Close()
	_ = srv
	dir := t.TempDir()
	cf := "report (конфликт 2026-11-24_10-00-00).txt"
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("server version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cf), []byte("my edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	SaveState(dir, State{ServerVersion: 5, Files: map[string]StateFile{
		"report.txt": {Hash: "hs", Version: 5},
		cf:           {Hash: "hl", Version: 6},
	}})

	s := NewSyncer(Config{Host: "127.0.0.1", Port: port, Dir: dir, Token: "t", RulesFree: true}, nil, nil)
	if err := s.ResolveConflictKeepServer(cf); err != nil {
		t.Fatalf("без связи «серверная» не должна падать: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cf)); !os.IsNotExist(err) {
		t.Error("копия должна быть удалена локально")
	}
}

// TestResolveConflictRejectsNonConflict — разрешать можно только копию.
func TestResolveConflictRejectsNonConflict(t *testing.T) {
	s := NewSyncer(Config{Dir: t.TempDir(), Token: "t"}, nil, nil)
	if err := s.ResolveConflictKeepLocal("обычный.txt"); err == nil {
		t.Error("ожидалась ошибка для не-конфликтного имени")
	}
	if err := s.ResolveConflictKeepServer("обычный.txt"); err == nil {
		t.Error("ожидалась ошибка для не-конфликтного имени")
	}
	// путь с выходом за папку тоже отвергается
	if err := s.ResolveConflictKeepServer("../x (конфликт 2026-01-02_03-04-05).txt"); err == nil {
		t.Error("ожидалась ошибка для пути с «..»")
	}
}
