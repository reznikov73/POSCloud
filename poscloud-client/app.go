package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows/registry"
)

type FileItem struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

type ClientState struct {
	Running    bool   `json:"running"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Token      string `json:"token"`
	ServerURL  string `json:"serverURL"`
	LocalDir   string `json:"localDir"`
	Interval   int    `json:"interval"`
	LastSync   string `json:"lastSync"`
	Count      int    `json:"count"`
	Online     bool   `json:"online"`
	Error      string `json:"error"`
	ConfigPath string `json:"configPath"`
}

// manifestFile — запись о файле, как её отдаёт сервер.
type manifestFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	Hash    string `json:"hash"`
	Version int64  `json:"version"`
	Deleted bool   `json:"deleted"`
}

// stateFile — что клиент знает о файле (локальное состояние синхронизации).
type stateFile struct {
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	Version int64  `json:"version"`
}

type clientState struct {
	ServerVersion int64                `json:"serverVersion"`
	Files         map[string]stateFile `json:"files"`
}

type App struct {
	ctx context.Context
	mu  sync.Mutex

	syncRun  bool
	syncMu   sync.Mutex
	host     string
	port     int
	localDir string
	interval int
	token    string
	shotSync bool // сохранять снимки экрана в папку синхронизации
	stopCh   chan struct{}
	lastSync string
	errMsg   string
	count    int
	online   bool

	logs     []string
	logFile  *os.File
	dataRoot string
	rootOnce sync.Once

	flagHost     string
	flagPort     int
	flagDir      string
	flagInterval int
	flagToken    string
	autoStart    bool
}

func NewApp() *App {
	home, _ := os.UserHomeDir()
	return &App{
		host:     "localhost",
		port:     8090,
		localDir: filepath.Join(home, "POSCloud", "sync"),
		interval: 3,
		shotSync: true,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.openLogFile()
	a.loadConfig()
	a.applyFlagOverrides()
	a.startHotkeys()
	a.log("Клиент POSCloud инициализирован")
	if a.autoStart {
		a.startOnLaunch()
	}
}

// startOnLaunch — автозапуск синхронизации при открытии программы.
// Без токена доступа сервер отвечает 401 на все запросы, поэтому вместо
// бесполезного цикла ошибок синхронизация не запускается, а причина
// пишется в журнал — окно остаётся открытым для правки настроек.
func (a *App) startOnLaunch() {
	if strings.TrimSpace(a.token) == "" {
		a.log("автозапуск пропущен: не задан токен доступа — укажите токен и нажмите «Запустить»")
		return
	}
	if strings.TrimSpace(a.localDir) == "" {
		a.log("автозапуск пропущен: не выбрана папка синхронизации")
		return
	}
	a.StartSync(a.host, a.port, a.localDir, a.interval, a.token)
}

// applyFlagOverrides применяет значения из командной строки поверх сохранённых
// настроек. Пустое значение означает «флага не было», поэтому настройки из
// client.json остаются в силе; явно переданный аргумент всё равно побеждает.
func (a *App) applyFlagOverrides() {
	if a.flagHost != "" {
		a.host = a.flagHost
	}
	if a.flagPort > 0 {
		a.port = a.flagPort
	}
	if a.flagDir != "" {
		a.localDir = a.flagDir
	}
	if a.flagInterval > 0 {
		a.interval = a.flagInterval
	}
	if a.flagToken != "" {
		a.token = a.flagToken
	}
}

// ---------- сохранение настроек ----------

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
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Dir      string `json:"dir"`
	Interval int    `json:"interval"`
	Token    string `json:"token"`
	// nil — настройка ещё не сохранялась, считаем включённой
	ScreenshotSync *bool `json:"screenshotSync,omitempty"`
}

func (a *App) loadConfig() {
	data, err := os.ReadFile(filepath.Join(a.root(), "client.json"))
	if err != nil {
		return
	}
	var c savedConfig
	if json.Unmarshal(data, &c) == nil {
		if c.Host != "" {
			a.host = c.Host
		}
		if c.Port > 0 {
			a.port = c.Port
		}
		if c.Dir != "" {
			a.localDir = c.Dir
		}
		if c.Interval > 0 {
			a.interval = c.Interval
		}
		if c.Token != "" {
			a.token = c.Token
		}
		if c.ScreenshotSync != nil {
			a.shotSync = *c.ScreenshotSync
		}
	}
}

func (a *App) persistConfig() {
	p := filepath.Join(a.root(), "client.json")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	b, _ := json.MarshalIndent(savedConfig{Host: a.host, Port: a.port, Dir: a.localDir, Interval: a.interval, Token: a.token, ScreenshotSync: &a.shotSync}, "", "  ")
	_ = os.WriteFile(p, b, 0644)
}

// SaveSettings — сохранить настройки, не запуская синхронизацию.
func (a *App) SaveSettings(host string, port int, localDir string, interval int, token string) ClientState {
	a.mu.Lock()
	if host != "" {
		a.host = host
	}
	if port > 0 {
		a.port = port
	}
	if localDir != "" {
		a.localDir = localDir
	}
	if interval > 0 {
		a.interval = interval
	}
	a.token = strings.TrimSpace(token)
	a.mu.Unlock()
	a.persistConfig()
	a.log("настройки сохранены")
	return a.GetClientState()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	stop := a.stopCh
	a.stopCh = nil
	a.mu.Unlock()
	if stop != nil {
		close(stop)
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

func (a *App) logPath() string { return filepath.Join(a.root(), "client.log") }

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
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Папка синхронизации"})
	if err != nil {
		return ""
	}
	return dir
}

// ---------- утилиты ----------

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
		out = append(out, FileItem{Name: filepath.ToSlash(rel), Size: info.Size(), Modified: info.ModTime().Format("2006-01-02 15:04:05")})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (a *App) baseURL() string {
	a.mu.Lock()
	h, p := a.host, a.port
	a.mu.Unlock()
	return fmt.Sprintf("http://%s:%d", h, p)
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

// ---------- локальное состояние и хэши ----------

func statePath(dir string) string { return filepath.Join(dir, ".poscloud-state.json") }

func loadState(dir string) clientState {
	s := clientState{Files: map[string]stateFile{}}
	data, err := os.ReadFile(statePath(dir))
	if err != nil {
		return s
	}
	var ss clientState
	if json.Unmarshal(data, &ss) == nil {
		s = ss
		if s.Files == nil {
			s.Files = map[string]stateFile{}
		}
	}
	return s
}

func saveState(dir string, s clientState) {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(statePath(dir), b, 0644)
}

func hashPath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// localScan возвращает карту «имя → состояние» по локальной папке.
// Если файл не менялся (совпадают размер и время), хэш берётся из состояния без пересчёта.
func localScan(dir string, st clientState) map[string]stateFile {
	out := map[string]stateFile{}
	ign := loadIgnore(dir)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if d.IsDir() {
			if p == dir {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || ign.match(key) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || ign.match(key) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		size := info.Size()
		mt := info.ModTime().Unix()
		if prev, ok := st.Files[key]; ok && prev.Size == size && prev.MTime == mt && prev.Hash != "" {
			out[key] = stateFile{Hash: prev.Hash, Size: size, MTime: mt, Version: prev.Version}
			return nil
		}
		h, herr := hashPath(p)
		if herr != nil {
			return nil
		}
		out[key] = stateFile{Hash: h, Size: size, MTime: mt}
		return nil
	})
	return out
}

func conflictName(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return base + " (конфликт " + time.Now().Format("2006-01-02_15-04-05") + ")" + ext
}

// ---------- состояние и управление ----------

func (a *App) GetClientState() ClientState {
	a.mu.Lock()
	st := ClientState{Running: a.syncRun, Host: a.host, Port: a.port, Token: a.token, LocalDir: a.localDir,
		Interval: a.interval, LastSync: a.lastSync, Error: a.errMsg, Count: a.count, Online: a.online}
	a.mu.Unlock()
	st.ServerURL = fmt.Sprintf("http://%s:%d", st.Host, st.Port)
	if st.Count == 0 {
		st.Count = len(listFiles(st.LocalDir))
	}
	st.ConfigPath = filepath.Join(a.root(), "client.json")
	return st
}

func (a *App) TestConnection(host string, port int, token string) string {
	if host == "" {
		host = a.host
	}
	if port <= 0 {
		port = a.port
	}
	if token == "" {
		token = a.token
	}
	cl := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/whoami", host, port), nil)
	if err != nil {
		return "Ошибка: " + err.Error()
	}
	authHeader(req, token)
	resp, err := cl.Do(req)
	if err != nil {
		return "Ошибка: нет связи с " + host + ":" + fmt.Sprintf("%d", port)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var r struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return "OK: связь есть, пользователь - " + r.Name
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "Ошибка: неверный или отозванный токен"
	}
	return fmt.Sprintf("Ошибка: статус %d", resp.StatusCode)
}

func (a *App) StartSync(host string, port int, localDir string, interval int, token string) ClientState {
	a.mu.Lock()
	if a.syncRun {
		a.mu.Unlock()
		st := a.GetClientState()
		st.Error = "синхронизация уже запущена"
		return st
	}
	if host != "" {
		a.host = host
	}
	if port > 0 {
		a.port = port
	}
	if localDir != "" {
		a.localDir = localDir
	}
	a.token = strings.TrimSpace(token)
	if interval <= 0 {
		interval = 3
	}
	a.interval = interval
	a.stopCh = make(chan struct{})
	stop := a.stopCh
	dir := a.localDir
	a.syncRun = true
	a.errMsg = ""
	a.mu.Unlock()

	_ = os.MkdirAll(dir, 0755)

	_ = os.RemoveAll(filepath.Join(dir, ".poscloud-cache"))

	go func() {
		a.log("старт синхронизации: " + dir)
		go a.watchLoop(dir, stop)
		go a.eventsLoop(stop)
		a.syncOnce(true)
		tk := time.NewTicker(time.Duration(interval) * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				a.log("синхронизация остановлена")
				return
			case <-tk.C:
				a.syncOnce(false)
			}
		}
	}()
	a.persistConfig()
	return a.GetClientState()
}

func (a *App) StopSync() ClientState {
	a.mu.Lock()
	stop := a.stopCh
	a.stopCh = nil
	a.syncRun = false
	a.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	return a.GetClientState()
}

func (a *App) SyncNow() ClientState {
	a.log("ручная синхронизация")
	go a.syncOnce(true)
	return a.GetClientState()
}

// syncOnce — один цикл двусторонней синхронизации (B1: хэши/версии, B2: удаления/конфликты).
func (a *App) syncOnce(verbose bool) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	a.mu.Lock()
	dir := a.localDir
	tok := a.token
	a.mu.Unlock()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0755)
	base := a.baseURL()

	st := loadState(dir)

	srvVer, changes, err := fetchChanges(base, tok, st.ServerVersion)
	if err != nil {
		a.mu.Lock()
		a.errMsg = "нет связи с сервером"
		a.online = false
		a.mu.Unlock()
		a.log("нет связи с " + base + " - " + err.Error())
		return
	}
	a.mu.Lock()
	a.errMsg = ""
	a.online = true
	a.mu.Unlock()

	srv := map[string]manifestFile{}
	for _, c := range changes {
		if _, ok := sanitizeRel(c.Name); !ok {
			continue
		}
		srv[c.Name] = c
	}
	local := localScan(dir, st)

	names := map[string]bool{}
	for n := range local {
		names[n] = true
	}
	for n := range st.Files {
		if _, ok := sanitizeRel(n); !ok {
			continue
		}
		names[n] = true
	}
	for n := range srv {
		names[n] = true
	}

	next := clientState{ServerVersion: st.ServerVersion, Files: map[string]stateFile{}}
	up, down, del := 0, 0, 0

	for name := range names {
		ls, hasLocal := local[name]
		ss, hasState := st.Files[name]
		cs, hasSrv := srv[name]

		switch {
		// --- сервер удалил файл (tombstone) ---
		case hasSrv && cs.Deleted:
			if hasLocal {
				if hasState && ls.Hash == ss.Hash {
					_ = os.Remove(localPath(dir, name))
					pruneEmptyDirs(dir, localPath(dir, name))
					del++
					a.log("вниз удаление: " + name)
				} else {
					cf := conflictName(name)
					_ = os.Rename(localPath(dir, name), localPath(dir, cf))
					if h, v, mt, err := uploadFile(base, dir, cf, tok); err == nil {
						next.Files[cf] = stateFile{Hash: h, Size: fileSize(localPath(dir, cf)), MTime: mt, Version: v}
						up++
					}
					a.log("конфликт (удаление vs правка): " + name + " -> " + cf)
				}
			}

		// --- у сервера есть файл ---
		case hasSrv && !cs.Deleted:
			if !hasLocal {
				if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
					next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
					down++
					a.log("вниз: " + name)
				}
			} else if ls.Hash == cs.Hash {
				next.Files[name] = stateFile{Hash: ls.Hash, Size: ls.Size, MTime: ls.MTime, Version: cs.Version}
			} else if !hasState {
				// новый клиент: локальная и серверная версии расходятся - делаем конфликтную копию,
				// чтобы не затереть серверную версию
				cf := conflictName(name)
				if data, err := os.ReadFile(localPath(dir, name)); err == nil {
					_ = os.WriteFile(localPath(dir, cf), data, 0644)
					if h, v, mt, err := uploadFile(base, dir, cf, tok); err == nil {
						next.Files[cf] = stateFile{Hash: h, Size: fileSize(localPath(dir, cf)), MTime: mt, Version: v}
						up++
					}
				}
				if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
					next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
					down++
				}
				a.log("конфликт (первый синк): " + name + " -> " + cf)
			} else if hasState && ls.Hash == ss.Hash {
				// локально без изменений, сервер новее -> скачиваем
				if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
					next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
					down++
					a.log("вниз: " + name)
				}
			} else if hasState && cs.Version > ss.Version {
				// обе стороны изменили -> конфликт
				cf := conflictName(name)
				if data, err := os.ReadFile(localPath(dir, name)); err == nil {
					_ = os.WriteFile(localPath(dir, cf), data, 0644)
					if h, v, mt, err := uploadFile(base, dir, cf, tok); err == nil {
						next.Files[cf] = stateFile{Hash: h, Size: fileSize(localPath(dir, cf)), MTime: mt, Version: v}
						up++
					}
				}
				if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
					next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
					down++
				}
				a.log("конфликт (правки): " + name + " -> " + cf)
			} else {
				// локально изменено, сервер не менял -> выгружаем
				if h, v, mt, err := uploadFile(base, dir, name, tok); err == nil {
					next.Files[name] = stateFile{Hash: h, Size: fileSize(localPath(dir, name)), MTime: mt, Version: v}
					up++
					a.log("вверх: " + name)
				}
			}

		// --- сервер не менял этот файл с прошлого раза ---
		default:
			if hasLocal {
				if hasState && ls.Hash == ss.Hash {
					next.Files[name] = stateFile{Hash: ls.Hash, Size: ls.Size, MTime: ls.MTime, Version: ss.Version}
				} else if h, v, mt, err := uploadFile(base, dir, name, tok); err == nil {
					next.Files[name] = stateFile{Hash: h, Size: fileSize(localPath(dir, name)), MTime: mt, Version: v}
					up++
					a.log("вверх: " + name)
				}
			} else if hasState {
				// локально удалён, сервер не менял -> удаляем на сервере (tombstone)
				if err := deleteRemote(base, name, tok); err == nil {
					del++
					a.log("вверх удаление: " + name)
					pruneEmptyDirs(dir, localPath(dir, name))
				}
			}
		}
	}

	maxv := srvVer
	for _, f := range next.Files {
		if f.Version > maxv {
			maxv = f.Version
		}
	}
	next.ServerVersion = maxv
	saveState(dir, next)
	a.mu.Lock()
	a.lastSync = time.Now().Format("15:04:05")
	a.count = len(next.Files)
	a.mu.Unlock()
	if verbose {
		a.log(fmt.Sprintf("синк: сервер %s, локальных %d, изменений %d, вверх %d, вниз %d, удалено %d",
			base, len(local), len(changes), up, down, del))
	}
}

func (a *App) LocalFiles() []FileItem {
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()
	return listFiles(dir)
}

func (a *App) DeleteLocalFile(name string) error {
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()
	clean, ok := sanitizeRel(name)
	if !ok {
		return fmt.Errorf("недопустимое имя")
	}
	return os.Remove(localPath(dir, clean))
}

// ---------- HTTP ----------

func authHeader(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func urlQ(s string) string {
	r := strings.NewReplacer(" ", "%20", "+", "%2B", "&", "%26", "#", "%23", "?", "%3F")
	return r.Replace(s)
}

func fetchChanges(base, token string, since int64) (int64, []manifestFile, error) {
	url := strings.TrimRight(base, "/") + "/changes?since=" + fmt.Sprintf("%d", since)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, nil, fmt.Errorf("неверный или отозванный токен")
	}
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("статус %d", resp.StatusCode)
	}
	var payload struct {
		Version int64          `json:"version"`
		Changes []manifestFile `json:"changes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, nil, err
	}
	return payload.Version, payload.Changes, nil
}

func uploadFile(base, dir, name, token string) (string, int64, int64, error) {
	local := localPath(dir, name)
	if fi, err := os.Stat(local); err == nil && fi.Size() >= blockSize {
		if h, v, mt, err := uploadFileBlockwise(base, dir, name, token); err == nil {
			return h, v, mt, nil
		}
	}
	f, err := os.Open(local)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()
	var clen int64
	if fi, err := f.Stat(); err == nil {
		clen = fi.Size()
	}
	transfers.begin(name, "up", clen)
	defer transfers.end(name)
	req, err := http.NewRequest("POST", strings.TrimRight(base, "/")+"/upload?name="+urlQ(name), &countingReader{r: f, fn: func(n int64) { transfers.add(name, n) }})
	if err != nil {
		return "", 0, 0, err
	}
	if clen > 0 {
		req.ContentLength = clen
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, 0, fmt.Errorf("статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		MTime   int64  `json:"mtime"`
		Hash    string `json:"hash"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, 0, err
	}
	if r.MTime > 0 {
		t := time.Unix(r.MTime, 0)
		_ = os.Chtimes(local, t, t)
	}
	return r.Hash, r.Version, r.MTime, nil
}

func downloadFile(base, dir, name string, mtime int64, token string) error {
	if blocks, size, err := fetchBlockList(base, name, token); err == nil && len(blocks) > 0 && size >= blockSize {
		if err := downloadFileBlockwise(base, dir, name, mtime, token, blocks, size); err == nil {
			return nil
		}
	}
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/download?name="+urlQ(name), nil)
	if err != nil {
		return err
	}
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("статус %d", resp.StatusCode)
	}
	_ = os.MkdirAll(dir, 0755)
	dst := localPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmpf, err := os.CreateTemp(filepath.Dir(dst), ".poscloud-dl-*")
	if err != nil {
		return err
	}
	tmp := tmpf.Name()
	transfers.begin(name, "down", resp.ContentLength)
	defer transfers.end(name)
	if _, err := io.Copy(tmpf, &countingReader{r: resp.Body, fn: func(n int64) { transfers.add(name, n) }}); err != nil {
		tmpf.Close()
		os.Remove(tmp)
		return err
	}
	if err := tmpf.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	if mtime > 0 {
		t := time.Unix(mtime, 0)
		_ = os.Chtimes(dst, t, t)
	}
	return nil
}

func deleteRemote(base, name, token string) error {
	req, err := http.NewRequest("DELETE", strings.TrimRight(base, "/")+"/delete?name="+urlQ(name), nil)
	if err != nil {
		return err
	}
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("статус %d", resp.StatusCode)
	}
	return nil
}

// watchLoop следит за изменениями в папке синхронизации и запускает цикл синхронизации
// с задержкой (debounce). Останавливается при закрытии stop. Если watcher создать не
// удалось — просто выходим: синхронизация продолжит работать по таймеру.
func (a *App) watchLoop(dir string, stop chan struct{}) {

	w, err := fsnotify.NewWatcher()

	if err != nil {

		a.log("слежение недоступно: " + err.Error() + " (работаем по таймеру)")

		return

	}

	if err := w.Add(dir); err != nil {

		a.log("не удалось следить за папкой: " + err.Error() + " (работаем по таймеру)")

		_ = w.Close()

		return

	}

	a.log("слежение за изменениями включено: " + dir)

	var timer *time.Timer

	defer func() {

		if timer != nil {

			timer.Stop()

		}

		_ = w.Close()

	}()

	for {

		select {

		case <-stop:

			return

		case ev, ok := <-w.Events:

			if !ok {

				return

			}

			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {

				continue

			}

			if strings.HasPrefix(filepath.Base(ev.Name), ".") {

				continue // служебные и временные файлы пропускаем

			}

			if timer != nil {

				timer.Stop()

			}

			timer = time.AfterFunc(500*time.Millisecond, func() { a.syncOnce(false) })

		case err, ok := <-w.Errors:

			if !ok {

				return

			}

			a.log("ошибка слежения: " + err.Error())

		}

	}
}

// localPath превращает относительный путь синхронизации ("a/b") в путь на диске.
func localPath(dir, name string) string {
	return filepath.Join(dir, filepath.FromSlash(name))
}

// ignoreList — шаблоны из .poscloudignore.
type ignoreList struct{ patterns []string }

func loadIgnore(dir string) ignoreList {
	data, err := os.ReadFile(filepath.Join(dir, ".poscloudignore"))
	if err != nil {
		return ignoreList{}
	}
	return parseIgnoreText(string(data))
}

// parseIgnoreText разбирает текст списка исключений в набор шаблонов.
func parseIgnoreText(text string) ignoreList {
	var il ignoreList
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(line, "/")
		if line != "" {
			il.patterns = append(il.patterns, line)
		}
	}
	return il
}

// match проверяет относительный путь "a/b" по шаблонам; шаблон без "/" сопоставляется с любым сегментом.
func (il ignoreList) match(rel string) bool {
	if len(il.patterns) == 0 || rel == "" {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, pat := range il.patterns {
		if strings.Contains(pat, "/") {
			if globMatch(pat, rel) {
				return true
			}
			continue
		}
		for _, s := range segs {
			if globMatch(pat, s) {
				return true
			}
		}
	}
	return false
}

// globMatch сопоставляет шаблон (*, ?, [...], **) со строкой пути.
func globMatch(pat, s string) bool {
	return matchSegs(strings.Split(pat, "/"), strings.Split(s, "/"))
}

func matchSegs(p, s []string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == "**" {
		if matchSegs(p[1:], s) {
			return true
		}
		for i := 1; i <= len(s); i++ {
			if matchSegs(p[1:], s[i:]) {
				return true
			}
		}
		return false
	}
	if len(s) == 0 {
		return false
	}
	ok, err := path.Match(p[0], s[0])
	if err != nil || !ok {
		return false
	}
	return matchSegs(p[1:], s[1:])
}

// pruneEmptyDirs удаляет опустевшие каталоги вверх до корня синхронизации.
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

// GetIgnore возвращает текущее содержимое .poscloudignore (пустая строка, если файла нет).
func (a *App) GetIgnore() string {
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, ".poscloudignore"))
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveIgnore записывает .poscloudignore в папку синхронизации (пустой текст = удалить файл).
func (a *App) SaveIgnore(text string) error {
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()
	if dir == "" {
		return fmt.Errorf("не задана папка синхронизации")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	p := filepath.Join(dir, ".poscloudignore")
	if strings.TrimSpace(text) == "" {
		_ = os.Remove(p)
	} else {
		if err := os.WriteFile(p, []byte(text), 0644); err != nil {
			return err
		}
	}
	go a.syncOnce(false)
	return nil
}

// blockSize — размер блока для дельта-передачи (4 МиБ).
const blockSize = 4 << 20

// --- content-defined chunking (CDC) ---
const (
	chunkMin  = 1 << 20
	chunkAvg  = 4 << 20
	chunkMax  = 16 << 20
	chunkMask = chunkAvg - 1
)

// gear — таблица скользящего хэша (детерминированная, splitmix64).
var gear [256]uint64

func init() {
	var x uint64 = 0x9E3779B97F4A7C15
	for i := 0; i < 256; i++ {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z = z ^ (z >> 31)
		gear[i] = z
	}
}

// chunkRef — границы и хэш одного чанка.
type chunkRef struct {
	Offset int64
	Size   int
	Hash   string
}

// splitChunks режет файл на чанки переменного размера; границы определяются содержимым,
// поэтому вставка/сдвиг в начале файла не сдвигает все последующие чанки.
func splitChunks(path string) ([]chunkRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []chunkRef
	var offset int64
	rbuf := make([]byte, 256*1024)
	cbuf := make([]byte, chunkMax)
	clen := 0
	var h uint64
	cut := func() {
		sum := sha256.Sum256(cbuf[:clen])
		out = append(out, chunkRef{Offset: offset, Size: clen, Hash: hex.EncodeToString(sum[:])})
		offset += int64(clen)
		clen = 0
		h = 0
	}
	for {
		n, rerr := f.Read(rbuf)
		for i := 0; i < n; i++ {
			b := rbuf[i]
			cbuf[clen] = b
			clen++
			h = (h << 1) + gear[b]
			if clen >= chunkMin && (h&chunkMask) == 0 {
				cut()
				continue
			}
			if clen >= chunkMax {
				cut()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	if clen > 0 {
		cut()
	}
	return out, nil
}

// readChunk читает чанк (offset, size) из файла.
func readChunk(path string, off int64, size int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, size)
	n, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

// fetchBlockList запрашивает у сервера список блоков файла и его размер.
func fetchBlockList(base, name, token string) ([]string, int64, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/blocks?name="+urlQ(name), nil)
	if err != nil {
		return nil, 0, err
	}
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("статус %d", resp.StatusCode)
	}
	var p struct {
		Version int64    `json:"version"`
		Size    int64    `json:"size"`
		Blocks  []string `json:"blocks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, 0, err
	}
	return p.Blocks, p.Size, nil
}

// putBlock отправляет один блок на сервер.
// putBlock отправляет один блок на сервер.
func putBlock(base, name, hash string, data []byte, token string) error {
	req, err := http.NewRequest("PUT", strings.TrimRight(base, "/")+"/block?hash="+hash+"&name="+urlQ(name), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// getBlock скачивает один блок с сервера.
// getBlock скачивает один блок с сервера.
func getBlock(base, name, hash, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/block?hash="+hash+"&name="+urlQ(name), nil)
	if err != nil {
		return nil, err
	}
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// commitBlocks фиксирует новый список блоков файла на сервере.
// Если сервер вернул список отсутствующих блоков — возвращает их в missing при err == nil.
func commitBlocks(base, name string, size, mtime int64, blocks []string, token string) (string, int64, int64, []string, error) {
	body, _ := json.Marshal(map[string]any{"size": size, "mtime": mtime, "blocks": blocks})
	req, err := http.NewRequest("POST", strings.TrimRight(base, "/")+"/commit?name="+urlQ(name), bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	authHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		var e struct {
			Error   string   `json:"error"`
			Missing []string `json:"missing"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "missing blocks" {
			return "", 0, 0, e.Missing, nil
		}
		return "", 0, 0, nil, fmt.Errorf("статус 400: %s", e.Error)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, 0, nil, fmt.Errorf("статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		MTime   int64  `json:"mtime"`
		Hash    string `json:"hash"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, 0, nil, err
	}
	return r.Hash, r.Version, r.MTime, nil, nil
}

// uploadFileBlockwise загружает файл блоками; при неудаче вызывающий откатится к целому пути.
// sendMissingChunks догружает отсутствующие на сервере чанки.
// sendMissingChunks догружает отсутствующие на сервере чанки.
func sendMissingChunks(base, name, local string, chunks []chunkRef, have map[string]bool, token string) error {
	for _, c := range chunks {
		if have[c.Hash] {
			continue
		}
		data, err := readChunk(local, c.Offset, c.Size)
		if err != nil {
			return err
		}
		if err := putBlock(base, name, c.Hash, data, token); err != nil {
			return err
		}
		transfers.add(name, int64(len(data)))
		have[c.Hash] = true
	}
	return nil
}

// uploadFileBlockwise загружает файл чанками (content-defined); при неудаче вызывающий откатится к целому пути.
// uploadFileBlockwise загружает файл чанками (content-defined); при неудаче вызывающий откатится к целому пути.
func uploadFileBlockwise(base, dir, name, token string) (string, int64, int64, error) {
	local := localPath(dir, name)
	fi, err := os.Stat(local)
	if err != nil {
		return "", 0, 0, err
	}
	chunks, err := splitChunks(local)
	if err != nil {
		return "", 0, 0, err
	}
	if len(chunks) == 0 {
		return "", 0, 0, fmt.Errorf("нет чанков")
	}
	hashes := make([]string, 0, len(chunks))
	for _, c := range chunks {
		hashes = append(hashes, c.Hash)
	}
	srv, _, err := fetchBlockList(base, name, token)
	if err != nil {
		return "", 0, 0, err
	}
	have := map[string]bool{}
	for _, h := range srv {
		have[h] = true
	}
	var total int64
	for _, c := range chunks {
		if !have[c.Hash] {
			total += int64(c.Size)
		}
	}
	transfers.begin(name, "up", total)
	defer transfers.end(name)
	if err := sendMissingChunks(base, name, local, chunks, have, token); err != nil {
		return "", 0, 0, err
	}
	hash, ver, mt, missing, err := commitBlocks(base, name, fi.Size(), fi.ModTime().Unix(), hashes, token)
	if err != nil {
		return "", 0, 0, err
	}
	if len(missing) > 0 {
		want := map[string]bool{}
		for _, h := range missing {
			want[h] = true
		}
		miss := make([]chunkRef, 0, len(missing))
		for _, c := range chunks {
			if want[c.Hash] {
				miss = append(miss, c)
			}
		}
		if err := sendMissingChunks(base, name, local, miss, have, token); err != nil {
			return "", 0, 0, err
		}
		hash, ver, mt, missing, err = commitBlocks(base, name, fi.Size(), fi.ModTime().Unix(), hashes, token)
		if err != nil {
			return "", 0, 0, err
		}
		if len(missing) > 0 {
			return "", 0, 0, fmt.Errorf("сервер не принял чанки")
		}
	}
	return hash, ver, mt, nil
}

// downloadFileBlockwise собирает файл из блоков, докачивая отсутствующие в локальный кэш.
// downloadFileBlockwise собирает файл из блоков, докачивая отсутствующие в локальный кэш.
// downloadFileBlockwise собирает файл из чанков: неизменённые берёт из текущей локальной версии файла,
// отсутствующие докачивает с сервера. Локальный кэш блоков не используется.
func downloadFileBlockwise(base, dir, name string, mtime int64, token string, blocks []string, size int64) error {
	dst := localPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	byHash := map[string]chunkRef{}
	var oldF *os.File
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		if lay, err := splitChunks(dst); err == nil {
			for _, c := range lay {
				byHash[c.Hash] = c
			}
			if f, err := os.Open(dst); err == nil {
				oldF = f
			}
		}
	}
	var toFetch int64
	for _, h := range blocks {
		if _, ok := byHash[h]; !ok {
			toFetch++
		}
	}
	transfers.begin(name, "down", toFetch)
	defer transfers.end(name)
	tmpf, err := os.CreateTemp(filepath.Dir(dst), ".poscloud-dl-*")
	if err != nil {
		if oldF != nil {
			oldF.Close()
		}
		return err
	}
	tn := tmpf.Name()
	ok := true
	for _, h := range blocks {
		if oldF != nil {
			if c, found := byHash[h]; found {
				buf := make([]byte, c.Size)
				if n, err := oldF.ReadAt(buf, c.Offset); err == nil && n == c.Size {
					if _, err := tmpf.Write(buf); err == nil {
						continue
					}
				}
			}
		}
		data, err := getBlock(base, name, h, token)
		if err != nil {
			ok = false
			break
		}
		if _, err := tmpf.Write(data); err != nil {
			ok = false
			break
		}
		transfers.add(name, 1)
	}
	if oldF != nil {
		oldF.Close()
	}
	if err := tmpf.Close(); err != nil {
		ok = false
	}
	if !ok {
		os.Remove(tn)
		return fmt.Errorf("не удалось собрать файл из чанков")
	}
	if size > 0 {
		if fi, err := os.Stat(tn); err == nil && fi.Size() != size {
			os.Remove(tn)
			return fmt.Errorf("размер собранного файла не совпал")
		}
	}
	if err := os.Rename(tn, dst); err != nil {
		os.Remove(tn)
		return err
	}
	if mtime > 0 {
		t := time.Unix(mtime, 0)
		_ = os.Chtimes(dst, t, t)
	}
	return nil
}

// eventsLoop держит подключение к /events и запускает синхронизацию по событию.
func (a *App) eventsLoop(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		a.mu.Lock()
		base := fmt.Sprintf("http://%s:%d", a.host, a.port)
		tok := a.token
		a.mu.Unlock()
		runEventsOnce(base, tok, stop, func() { a.syncOnce(false) })
		select {
		case <-stop:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// runEventsOnce читает поток /events до обрыва и вызывает onEvent на каждое событие.
func runEventsOnce(base, token string, stop chan struct{}, onEvent func()) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/events", nil)
	if err != nil {
		return
	}
	authHeader(req, token)
	cl := &http.Client{}
	resp, err := cl.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Version int64 `json:"version"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev) == nil {
			onEvent()
		}
	}
}

// AutostartEnabled сообщает, включён ли автозапуск при входе в Windows.
func (a *App) AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("POSCloud Client")
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
		return k.SetStringValue("POSCloud Client", "\""+exe+"\" -autostart")
	}
	_ = k.DeleteValue("POSCloud Client")
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

// countingReader считает прочитанные байты (для прогресса загрузки).
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
