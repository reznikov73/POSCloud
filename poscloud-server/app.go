package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows/registry"
)

type FileItem struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	MTime    int64  `json:"mtime"`
}

type ServerState struct {
	Running    bool   `json:"running"`
	Port       int    `json:"port"`
	PortFree   bool   `json:"portFree"`
	DataDir    string `json:"dataDir"`
	URL        string `json:"url"`
	LocalIP    string `json:"localIP"`
	Count      int    `json:"count"`
	Users      int    `json:"users"`
	Error      string `json:"error"`
	ConfigPath string `json:"configPath"`
}

type App struct {
	ctx context.Context
	mu  sync.Mutex

	subMu sync.Mutex
	subs  map[string]map[chan int64]bool

	httpSrv *http.Server
	srvRun  bool
	srvPort int
	srvDir  string
	srvErr  string

	auth *authStore

	logs     []string
	logFile  *os.File
	dataRoot string
	rootOnce sync.Once

	flagPort     int
	flagData     string
	autoStart    bool
	autoRun      bool // запускать сервер сразу при открытии программы
	autoStartSet bool // -autostart передали явно в командной строке
}

func NewApp() *App {
	home, _ := os.UserHomeDir()
	return &App{
		srvPort: 8090,
		srvDir:  filepath.Join(home, "POSCloud", "server-data"),
		autoRun: true,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.openLogFile()
	a.loadConfig()
	a.auth = newAuthStore(filepath.Join(a.root(), "users.json"))
	if a.flagPort > 0 {
		a.srvPort = a.flagPort
	}
	if a.flagData != "" {
		a.srvDir = a.flagData
	}
	// Явно переданный -autostart важнее сохранённой настройки.
	if a.autoStartSet {
		a.autoRun = a.autoStart
	}
	a.log("Сервер POSCloud инициализирован")
	if a.autoRun {
		a.StartServer(a.srvPort, a.srvDir)
	}
}

// ---------- сохранение настроек ----------

// resolveBaseDir — каталог рядом с исполняемым файлом (там будут настройки и логи).
// Если писать в него нельзя — откат на пользовательский каталог конфигурации.
func resolveBaseDir() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		d := filepath.Dir(exe)
		if f, err := os.CreateTemp(d, ".write-test-*"); err == nil {
			name := f.Name()
			_ = f.Close()
			_ = os.Remove(name)
			return d
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "POSCloud")
}

func (a *App) root() string {
	a.rootOnce.Do(func() { a.dataRoot = resolveBaseDir() })
	return a.dataRoot
}

type savedConfig struct {
	Port    int    `json:"port"`
	DataDir string `json:"dataDir"`
	// nil — запуск при открытии программы включён по умолчанию
	AutoRun *bool `json:"autoRun,omitempty"`
}

func (a *App) loadConfig() {
	data, err := os.ReadFile(filepath.Join(a.root(), "server.json"))
	if err != nil {
		return
	}
	var c savedConfig
	if json.Unmarshal(data, &c) == nil {
		if c.Port > 0 {
			a.srvPort = c.Port
		}
		if c.DataDir != "" {
			a.srvDir = c.DataDir
		}
		if c.AutoRun != nil {
			a.autoRun = *c.AutoRun
		}
	}
}

func (a *App) persistConfig() {
	p := filepath.Join(a.root(), "server.json")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	b, _ := json.MarshalIndent(savedConfig{Port: a.srvPort, DataDir: a.srvDir, AutoRun: &a.autoRun}, "", "  ")
	_ = os.WriteFile(p, b, 0644)
}

// SaveSettings — сохранить настройки, не запуская сервер.
func (a *App) SaveSettings(port int, dataDir string) ServerState {
	a.mu.Lock()
	if port > 0 {
		a.srvPort = port
	}
	if dataDir != "" {
		a.srvDir = dataDir
	}
	a.mu.Unlock()
	a.persistConfig()
	a.log("настройки сохранены (порт " + fmt.Sprintf("%d", port) + ", каталог " + dataDir + ")")
	return a.GetServerState()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	srv := a.httpSrv
	a.mu.Unlock()
	if srv != nil {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(c)
		cancel()
	}
}

func (a *App) log(s string) {
	line := time.Now().Format("15:04:05") + "  " + s
	a.mu.Lock()
	a.logs = append(a.logs, line)
	if len(a.logs) > 500 {
		a.logs = a.logs[len(a.logs)-500:]
	}
	if a.logFile != nil {
		_, _ = a.logFile.WriteString(time.Now().Format("2006-01-02 ") + line + "\n")
	}
	a.mu.Unlock()
}

func (a *App) logPath() string { return filepath.Join(a.root(), "server.log") }

func (a *App) openLogFile() {
	p := a.logPath()
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		a.mu.Lock()
		a.logFile = f
		a.mu.Unlock()
	}
}

// OpenLogFile — открыть файл журнала в Проводнике (выделив его).
func (a *App) OpenLogFile() {
	p := a.logPath()
	_ = exec.Command("explorer", "/select,"+p).Start()
}

func (a *App) GetLogs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.logs))
	copy(out, a.logs)
	return out
}

func (a *App) ClearLogs() {
	a.mu.Lock()
	a.logs = nil
	a.mu.Unlock()
}

func (a *App) PickDirectory() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Каталог хранения файлов"})
	if err != nil {
		return ""
	}
	return dir
}

// ---------- утилиты ----------

func portFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func (a *App) CheckPort(port int) bool { return portFree(port) }

func localIPv4() string {
	var ips []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, ad := range addrs {
			if ipnet, ok := ad.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if v4 := ipnet.IP.To4(); v4 != nil {
					ips = append(ips, v4.String())
				}
			}
		}
	}
	return strings.Join(ips, ", ")
}

// sanitizeRel проверяет относительный путь и приводит его к виду "a/b".
func sanitizeRel(p string) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.Trim(p, "/")
	if p == "" {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	return p, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func listFiles(dir string) []FileItem {
	out := []FileItem{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		out = append(out, FileItem{Name: filepath.ToSlash(rel), Size: info.Size(), Modified: info.ModTime().Format("2006-01-02 15:04:05"), MTime: info.ModTime().Unix()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), ".") {
			n++
		}
		return nil
	})
	return n
}

// ---------- HTTP-сервер ----------

// storageRoot — корень хранилища; внутри — папка на каждого пользователя.
func (a *App) storageRoot() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.srvDir
}

// userDir возвращает каталог хранилища пользователя из контекста запроса.
func (a *App) userDir(r *http.Request) (string, bool) {
	uid := userIDFromCtx(r)
	if uid == "" {
		return "", false
	}
	dir := filepath.Join(a.storageRoot(), uid)
	_ = os.MkdirAll(dir, 0755)
	return dir, true
}

// statusRecorder фиксирует код ответа, чтобы логировать только значимые запросы.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withLog — пропускает запрос дальше и пишет в журнал только значимое (ошибки,
// включая отказ авторизации). Рутинный опрос (например /changes) не логируется.
func (a *App) withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path != "/health" && rec.status >= http.StatusBadRequest {
			a.log(fmt.Sprintf("отклонён %s %s от %s (код %d)", r.Method, r.URL.Path, r.RemoteAddr, rec.status))
		}
	})
}

func (a *App) serverMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"service": "poscloud-server", "status": "ok"})
	})
	mux.HandleFunc("GET /whoami", func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromCtx(r)
		name := uid
		if u, ok := a.auth.get(uid); ok {
			name = u.Name
		}
		writeJSON(w, 200, map[string]any{"user": uid, "name": name})
	})
	mux.HandleFunc("GET /list", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		writeJSON(w, 200, map[string]any{"files": listFiles(dir)})
	})
	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		name := r.URL.Query().Get("name")
		var src io.Reader
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			file, fh, err := r.FormFile("file")
			if err != nil {
				writeJSON(w, 400, map[string]any{"error": "поле 'file' отсутствует"})
				return
			}
			defer file.Close()
			if name == "" {
				name = fh.Filename
			}
			src = file
		} else {
			src = r.Body
		}
		clean, ok := sanitizeRel(name)
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "недопустимое имя"})
			return
		}
		dst := filepath.Join(dir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		transfers.begin(clean, "up", r.ContentLength)
		defer transfers.end(clean)
		tmp, err := os.CreateTemp(filepath.Dir(dst), ".up-*")
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		tn := tmp.Name()
		n, err := io.Copy(tmp, &countingReader{r: src, fn: func(n int64) { transfers.add(clean, n) }})
		tmp.Close()
		if err != nil {
			os.Remove(tn)
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		if err := os.Rename(tn, dst); err != nil {
			os.Remove(tn)
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		var mt int64
		var sz int64
		if fi, err := os.Stat(dst); err == nil {
			mt = fi.ModTime().Unix()
			sz = fi.Size()
		}
		hash, _ := fileHash(dst)
		a.mu.Lock()
		m := loadManifest(dir)
		if prev, ok := m.Files[clean]; ok && !prev.Deleted && prev.Hash == hash {
			a.mu.Unlock()
			writeJSON(w, 200, map[string]any{"name": clean, "size": prev.Size, "mtime": prev.MTime, "hash": prev.Hash, "version": prev.Version})
			return
		}
		ver := upsertFile(dir, &m, clean, sz, mt, hash, nil)
		_ = saveManifest(dir, m)
		gcBlocks(dir, &m)
		a.notifyVersion(userIDFromCtx(r), ver)
		a.mu.Unlock()
		a.log(fmt.Sprintf("принят файл %s (%d байт) от пользователя %s", clean, n, userIDFromCtx(r)))
		writeJSON(w, 200, map[string]any{"name": clean, "size": sz, "mtime": mt, "hash": hash, "version": ver})
	})
	mux.HandleFunc("GET /download", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		clean, ok := sanitizeRel(r.URL.Query().Get("name"))
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "недопустимое имя"})
			return
		}
		path := filepath.Join(dir, filepath.FromSlash(clean))
		if _, err := os.Stat(path); err != nil {
			writeJSON(w, 404, map[string]any{"error": "файл не найден"})
			return
		}
		a.mu.Lock()
		m := loadManifest(dir)
		mf := m.Files[clean]
		a.mu.Unlock()
		if mf.Hash != "" {
			w.Header().Set("X-PC-Hash", mf.Hash)
			w.Header().Set("X-PC-Version", strconv.FormatInt(mf.Version, 10))
		}
		f, err := os.Open(path)
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": "файл не найден"})
			return
		}
		defer f.Close()
		fi, _ := f.Stat()
		transfers.begin(clean, "down", fi.Size())
		defer transfers.end(clean)
		w.Header().Set("Content-Disposition", "attachment; filename=\""+clean+"\"")
		a.log(fmt.Sprintf("отдан файл %s пользователю %s", clean, userIDFromCtx(r)))
		_, _ = io.Copy(&countingWriter{w: w, fn: func(n int64) { transfers.add(clean, n) }}, f)
	})
	mux.HandleFunc("DELETE /delete", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		clean, ok := sanitizeRel(r.URL.Query().Get("name"))
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "недопустимое имя"})
			return
		}
		a.mu.Lock()
		m := loadManifest(dir)
		ver := markDeleted(dir, &m, clean)
		_ = saveManifest(dir, m)
		gcBlocks(dir, &m)
		a.notifyVersion(userIDFromCtx(r), ver)
		a.mu.Unlock()
		target := filepath.Join(dir, filepath.FromSlash(clean))
		_ = os.Remove(target)
		pruneEmptyDirs(dir, target)
		a.log("удалён " + clean + " (пользователь " + userIDFromCtx(r) + ")")
		writeJSON(w, 200, map[string]any{"deleted": clean, "version": ver})
	})
	mux.HandleFunc("GET /manifest", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		a.mu.Lock()
		m := loadManifest(dir)
		if reconcileManifest(dir, &m) {
			_ = saveManifest(dir, m)
		}
		files := make([]ManifestFile, 0, len(m.Files))
		for _, f := range m.Files {
			if !f.Deleted {
				f.Blocks = nil
				files = append(files, f)
			}
		}
		ver := m.Version
		a.mu.Unlock()
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		writeJSON(w, 200, map[string]any{"version": ver, "files": files})
	})
	mux.HandleFunc("GET /changes", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		a.mu.Lock()
		m := loadManifest(dir)
		ch := make([]ManifestFile, 0)
		for _, f := range m.Files {
			if f.Version > since {
				f.Blocks = nil
				ch = append(ch, f)
			}
		}
		ver := m.Version
		a.mu.Unlock()
		sort.Slice(ch, func(i, j int) bool { return ch[i].Version < ch[j].Version })
		if len(ch) > 0 {
			a.log(fmt.Sprintf("изменения для %s: файлов %d (версия %d)", userIDFromCtx(r), len(ch), ver))
		}
		writeJSON(w, 200, map[string]any{"version": ver, "changes": ch})
	})
	// --- блочная (дельта) передача ---
	mux.HandleFunc("GET /blocks", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		clean, ok := sanitizeRel(r.URL.Query().Get("name"))
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "недопустимое имя"})
			return
		}
		a.mu.Lock()
		m := loadManifest(dir)
		mf := m.Files[clean]
		a.mu.Unlock()
		blocks := mf.Blocks
		if blocks == nil {
			blocks = []string{}
		}
		writeJSON(w, 200, map[string]any{"version": mf.Version, "size": mf.Size, "blocks": blocks})
	})
	mux.HandleFunc("GET /block", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		h := r.URL.Query().Get("hash")
		if !validHash(h) {
			writeJSON(w, 400, map[string]any{"error": "неверный хэш блока"})
			return
		}
		p := blockPath(dir, h)
		if _, err := os.Stat(p); err != nil {
			writeJSON(w, 404, map[string]any{"error": "блок не найден"})
			return
		}
		f, err := os.Open(p)
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": "блок не найден"})
			return
		}
		defer f.Close()
		fi, _ := f.Stat()
		bname := r.URL.Query().Get("name")
		transfers.begin(bname, "down", fi.Size())
		defer transfers.end(bname)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.Copy(&countingWriter{w: w, fn: func(n int64) { transfers.add(bname, n) }}, f)
	})
	mux.HandleFunc("PUT /block", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		h := r.URL.Query().Get("hash")
		if !validHash(h) {
			writeJSON(w, 400, map[string]any{"error": "неверный хэш блока"})
			return
		}
		if err := os.MkdirAll(blocksDir(dir), 0755); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		tmp, err := os.CreateTemp(blocksDir(dir), ".blk-*")
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		tn := tmp.Name()
		bname := r.URL.Query().Get("name")
		transfers.begin(bname, "up", r.ContentLength)
		defer transfers.end(bname)
		n, err := io.Copy(tmp, &countingReader{r: r.Body, fn: func(n int64) { transfers.add(bname, n) }})
		tmp.Close()
		if err != nil {
			os.Remove(tn)
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		got, err := fileHash(tn)
		if err != nil || got != h {
			os.Remove(tn)
			writeJSON(w, 400, map[string]any{"error": "хэш блока не совпал"})
			return
		}
		if err := os.Rename(tn, blockPath(dir, h)); err != nil {
			os.Remove(tn)
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"hash": h, "size": n})
	})
	mux.HandleFunc("POST /commit", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		clean, ok := sanitizeRel(r.URL.Query().Get("name"))
		if !ok {
			writeJSON(w, 400, map[string]any{"error": "недопустимое имя"})
			return
		}
		var req struct {
			Size   int64    `json:"size"`
			MTime  int64    `json:"mtime"`
			Blocks []string `json:"blocks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		if len(req.Blocks) == 0 {
			writeJSON(w, 400, map[string]any{"error": "пустой список блоков"})
			return
		}
		var missing []string
		for _, h := range req.Blocks {
			if !validHash(h) {
				writeJSON(w, 400, map[string]any{"error": "неверный хэш блока"})
				return
			}
			if _, err := os.Stat(blockPath(dir, h)); err != nil {
				missing = append(missing, h)
			}
		}
		if len(missing) > 0 {
			writeJSON(w, 400, map[string]any{"error": "missing blocks", "missing": missing})
			return
		}
		dst := filepath.Join(dir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		tmp, err := os.CreateTemp(filepath.Dir(dst), ".cm-*")
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		tn := tmp.Name()
		transfers.begin(clean, "up", int64(len(req.Blocks)))
		defer transfers.end(clean)
		assembled := true
		for _, h := range req.Blocks {
			src, err := os.Open(blockPath(dir, h))
			if err != nil {
				assembled = false
				break
			}
			_, err = io.Copy(tmp, src)
			src.Close()
			transfers.add(clean, 1)
			if err != nil {
				assembled = false
				break
			}
		}
		tmp.Close()
		if !assembled {
			os.Remove(tn)
			writeJSON(w, 500, map[string]any{"error": "не удалось собрать файл"})
			return
		}
		if err := os.Rename(tn, dst); err != nil {
			os.Remove(tn)
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		var sz int64
		var mt int64
		if fi, err := os.Stat(dst); err == nil {
			sz = fi.Size()
			if req.MTime > 0 {
				t := time.Unix(req.MTime, 0)
				_ = os.Chtimes(dst, t, t)
				mt = req.MTime
			} else {
				mt = fi.ModTime().Unix()
			}
		}
		hash, _ := fileHash(dst)
		a.mu.Lock()
		m := loadManifest(dir)
		ver := upsertFile(dir, &m, clean, sz, mt, hash, req.Blocks)
		_ = saveManifest(dir, m)
		gcBlocks(dir, &m)
		a.notifyVersion(userIDFromCtx(r), ver)
		a.mu.Unlock()
		a.log(fmt.Sprintf("собран файл %s (%d байт, блоков %d) от пользователя %s", clean, sz, len(req.Blocks), userIDFromCtx(r)))
		writeJSON(w, 200, map[string]any{"name": clean, "size": sz, "mtime": mt, "hash": hash, "version": ver})
	})
	// --- push (SSE): поток изменений для клиента ---
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromCtx(r)
		if uid == "" {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, 500, map[string]any{"error": "стриминг не поддерживается"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		fl.Flush()

		var cur int64
		if dir, pok := a.userDir(r); pok {
			a.mu.Lock()
			m := loadManifest(dir)
			cur = m.Version
			a.mu.Unlock()
		}
		fmt.Fprintf(w, "data: {\"version\":%d}\n\n", cur)
		fl.Flush()

		ch := a.subscribe(uid)
		defer a.unsubscribe(uid, ch)

		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case v := <-ch:
				fmt.Fprintf(w, "data: {\"version\":%d}\n\n", v)
				fl.Flush()
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("GET /blocks-all", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := a.userDir(r)
		if !ok {
			writeJSON(w, 401, map[string]any{"error": "нет пользователя"})
			return
		}
		a.mu.Lock()
		m := loadManifest(dir)
		seen := map[string]bool{}
		out := []string{}
		for _, f := range m.Files {
			if f.Deleted {
				continue
			}
			for _, h := range f.Blocks {
				if !seen[h] {
					seen[h] = true
					out = append(out, h)
				}
			}
		}
		a.mu.Unlock()
		writeJSON(w, 200, map[string]any{"blocks": out})
	})
	return mux
}

func (a *App) GetServerState() ServerState {
	a.mu.Lock()
	st := ServerState{Running: a.srvRun, Port: a.srvPort, DataDir: a.srvDir, Error: a.srvErr}
	a.mu.Unlock()
	if st.Port == 0 {
		st.Port = 8090
	}
	st.PortFree = portFree(st.Port)
	st.URL = fmt.Sprintf("http://%s:%d", firstIPOrLocalhost(), st.Port)
	st.LocalIP = localIPv4()
	st.Count = countFiles(st.DataDir)
	if a.auth != nil {
		st.Users = len(a.auth.list())
	}
	st.ConfigPath = filepath.Join(a.root(), "server.json")
	return st
}

func firstIPOrLocalhost() string {
	ips := localIPv4()
	if ips == "" {
		return "localhost"
	}
	if i := strings.Index(ips, ","); i >= 0 {
		return strings.TrimSpace(ips[:i])
	}
	return ips
}

func (a *App) StartServer(port int, dataDir string) ServerState {
	a.mu.Lock()
	if a.srvRun {
		a.mu.Unlock()
		st := a.GetServerState()
		st.Error = "сервер уже запущен"
		return st
	}
	if dataDir == "" {
		dataDir = a.srvDir
	}
	a.mu.Unlock()

	if port <= 0 {
		port = 8090
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		a.mu.Lock()
		a.srvErr = "не удалось создать каталог: " + err.Error()
		a.mu.Unlock()
		return a.GetServerState()
	}

	// Разовая уборка при запуске: раньше удаление файлов оставляло в хранилище
	// пустой скелет каталогов (файлы удалялись, а папки — нет).
	go a.pruneStore()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           a.withLog(a.withAuth(a.serverMux())),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		a.mu.Lock()
		a.srvErr = fmt.Sprintf("порт %d занят", port)
		a.mu.Unlock()
		a.log(fmt.Sprintf("не удалось занять порт %d", port))
		return a.GetServerState()
	}

	a.mu.Lock()
	a.httpSrv = srv
	a.srvRun = true
	a.srvPort = port
	a.srvDir = dataDir
	a.srvErr = ""
	a.mu.Unlock()

	go func() {
		err := srv.Serve(ln)
		a.mu.Lock()
		a.srvRun = false
		a.httpSrv = nil
		if err != nil && err != http.ErrServerClosed {
			a.srvErr = err.Error()
		}
		a.mu.Unlock()
	}()

	a.log(fmt.Sprintf("сервер запущен: порт %d, каталог %s", port, dataDir))
	a.persistConfig()
	return a.GetServerState()
}

func (a *App) StopServer() ServerState {
	a.mu.Lock()
	srv := a.httpSrv
	a.mu.Unlock()
	if srv != nil {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(c)
		cancel()
	}
	a.mu.Lock()
	a.srvRun = false
	a.httpSrv = nil
	a.srvErr = ""
	a.mu.Unlock()
	a.log("сервер остановлен, порт освобождён")
	return a.GetServerState()
}

// ---------- Управление пользователями и токенами (локальная админка) ----------

func (a *App) ListUsers() []UserInfo {
	root := a.storageRoot()
	out := make([]UserInfo, 0)
	for _, u := range a.auth.list() {
		out = append(out, UserInfo{
			ID: u.ID, Name: u.Name, Enabled: u.Enabled, CreatedAt: u.CreatedAt,
			TokenCount: len(u.Tokens), FileCount: countFiles(filepath.Join(root, u.ID)),
		})
	}
	return out
}

func (a *App) CreateUser(name string) (UserInfo, error) {
	u, err := a.auth.createUser(name)
	if err != nil {
		return UserInfo{}, err
	}
	a.log("создан пользователь " + u.Name + " (" + u.ID + ")")
	return UserInfo{ID: u.ID, Name: u.Name, Enabled: u.Enabled, CreatedAt: u.CreatedAt}, nil
}

func (a *App) SetUserEnabled(id string, enabled bool) error {
	if err := a.auth.setUserEnabled(id, enabled); err != nil {
		return err
	}
	a.log(fmt.Sprintf("пользователь %s %s", id, map[bool]string{true: "включён", false: "отключён"}[enabled]))
	return nil
}

func (a *App) DeleteUser(id string) error {
	if err := a.auth.deleteUser(id); err != nil {
		return err
	}
	a.log("удалён пользователь " + id + " (файлы на диске сохранены)")
	return nil
}

// IssueToken создаёт токен устройства и возвращает его открытый текст (показывается один раз).
func (a *App) IssueToken(userID, deviceName string) (string, error) {
	tok, err := a.auth.issueToken(userID, deviceName)
	if err != nil {
		return "", err
	}
	a.log("выдан токен для " + userID + " (" + deviceName + ")")
	return tok, nil
}

func (a *App) ListTokens(userID string) []TokenInfo {
	u, ok := a.auth.get(userID)
	if !ok {
		return []TokenInfo{}
	}
	out := make([]TokenInfo, 0, len(u.Tokens))
	for _, t := range u.Tokens {
		out = append(out, TokenInfo{ID: t.ID, Name: t.Name, Enabled: t.Enabled, CreatedAt: t.CreatedAt, LastSeen: t.LastSeen})
	}
	return out
}

func (a *App) RevokeToken(userID, tokenID string) error {
	if err := a.auth.revokeToken(userID, tokenID); err != nil {
		return err
	}
	a.log("отозван токен " + tokenID + " у " + userID)
	return nil
}

// pruneEmptyDirs удаляет опустевшие каталоги, поднимаясь от файла вверх до root.
// Протокол удаляет только файлы, поэтому без такой чистки в хранилище
// остаётся пустой скелет каталогов. Ровно так же это сделано у клиента.
func pruneEmptyDirs(root, filePath string) {
	root = filepath.Clean(root)
	dir := filepath.Dir(filepath.Clean(filePath))
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// pruneEmptyTree убирает все пустые каталоги внутри root и возвращает их число.
// Нужна при запуске сервера, чтобы вычистить скелет, накопившийся раньше.
// Служебные каталоги (с точкой в начале, например .blocks) не трогаем.
func pruneEmptyTree(root string) int {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() {
			return nil
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if p != root {
			dirs = append(dirs, p)
		}
		return nil
	})

	// Сначала самые глубокие: иначе родитель не станет пустым вовремя.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	removed := 0
	for _, d := range dirs {
		if err := os.Remove(d); err == nil { // непустой каталог не удалится
			removed++
		}
	}
	return removed
}

// pruneStore убирает пустые каталоги у всех пользователей хранилища.
func (a *App) pruneStore() {
	if a.auth == nil {
		return
	}
	root := a.storageRoot()
	for _, u := range a.auth.list() {
		if n := pruneEmptyTree(filepath.Join(root, u.ID)); n > 0 {
			a.log(fmt.Sprintf("уборка: удалено пустых каталогов — %d (пользователь %s)", n, u.ID))
		}
	}
}

func (a *App) UserFiles(userID string) []FileItem {
	return listFiles(filepath.Join(a.storageRoot(), userID))
}

func (a *App) DeleteUserFile(userID, name string) error {
	clean, ok := sanitizeRel(name)
	if !ok {
		return fmt.Errorf("недопустимое имя")
	}
	dir := filepath.Join(a.storageRoot(), userID)
	a.mu.Lock()
	m := loadManifest(dir)
	ver := markDeleted(dir, &m, clean)
	_ = saveManifest(dir, m)
	gcBlocks(dir, &m)
	a.notifyVersion(userID, ver)
	a.mu.Unlock()
	target := filepath.Join(dir, filepath.FromSlash(clean))
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	pruneEmptyDirs(dir, target)
	a.log("удалён " + clean + " у пользователя " + userID + " (tombstone)")
	return nil
}

// notifyVersion сообщает подписчикам пользователя о новой версии пространства.
func (a *App) notifyVersion(uid string, version int64) {
	if uid == "" {
		return
	}
	a.subMu.Lock()
	for ch := range a.subs[uid] {
		select {
		case ch <- version:
		default:
		}
	}
	a.subMu.Unlock()
}

func (a *App) subscribe(uid string) chan int64 {
	ch := make(chan int64, 8)
	a.subMu.Lock()
	if a.subs == nil {
		a.subs = map[string]map[chan int64]bool{}
	}
	if a.subs[uid] == nil {
		a.subs[uid] = map[chan int64]bool{}
	}
	a.subs[uid][ch] = true
	a.subMu.Unlock()
	return ch
}

func (a *App) unsubscribe(uid string, ch chan int64) {
	a.subMu.Lock()
	if m := a.subs[uid]; m != nil {
		delete(m, ch)
		if len(m) == 0 {
			delete(a.subs, uid)
		}
	}
	a.subMu.Unlock()
}

// AutostartEnabled сообщает, включён ли автозапуск при входе в Windows.
// AutoRunEnabled сообщает, запускается ли сервер сразу при открытии программы.
func (a *App) AutoRunEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.autoRun
}

// SetAutoRun включает и выключает старт сервера при открытии программы.
func (a *App) SetAutoRun(on bool) error {
	a.mu.Lock()
	a.autoRun = on
	a.mu.Unlock()
	a.persistConfig()
	return nil
}

func (a *App) AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("POSCloud Server")
	return err == nil
}

// SetAutostart включает/выключает автозапуск при входе в Windows (реестр HKCU\...\Run).
func (a *App) SetAutostart(enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return k.SetStringValue("POSCloud Server", "\""+exe+"\" -autostart")
	}
	_ = k.DeleteValue("POSCloud Server")
	return nil
}

// --- прогресс передач (для интерфейса) ---

// Transfer — активная передача файла (для интерфейса).
type Transfer struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	Pct  int    `json:"pct"`
}

type transferState struct {
	name string
	dir  string
	done int64
	tot  int64
}

type transferStore struct {
	mu sync.Mutex
	m  map[string]*transferState
}

var transfers = &transferStore{m: map[string]*transferState{}}

func (s *transferStore) begin(name, dir string, total int64) {
	if name == "" || total <= 0 {
		return
	}
	s.mu.Lock()
	s.m[name] = &transferState{name: name, dir: dir, tot: total}
	s.mu.Unlock()
}

func (s *transferStore) add(name string, n int64) {
	s.mu.Lock()
	if t := s.m[name]; t != nil {
		t.done += n
		if t.done > t.tot {
			t.done = t.tot
		}
	}
	s.mu.Unlock()
}

func (s *transferStore) end(name string) {
	s.mu.Lock()
	delete(s.m, name)
	s.mu.Unlock()
}

func (s *transferStore) snapshot() []Transfer {
	s.mu.Lock()
	out := make([]Transfer, 0, len(s.m))
	for _, t := range s.m {
		pct := 0
		if t.tot > 0 {
			pct = int(t.done * 100 / t.tot)
		}
		if pct > 100 {
			pct = 100
		}
		out = append(out, Transfer{Name: t.name, Dir: t.dir, Pct: pct})
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *transferStore) active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m) > 0
}

// GetTransfers возвращает список активных передач (для интерфейса).
func (a *App) GetTransfers() []Transfer { return transfers.snapshot() }

// countingReader считает прочитанные байты (для прогресса приёма).
type countingReader struct {
	r  io.Reader
	fn func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.fn != nil {
		c.fn(int64(n))
	}
	return n, err
}

// countingWriter считает записанные байты (для прогресса отдачи).
type countingWriter struct {
	w  io.Writer
	fn func(int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.fn != nil {
		c.fn(int64(n))
	}
	return n, err
}
