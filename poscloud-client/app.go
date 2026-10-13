package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

type App struct {
	ctx context.Context
	mu  sync.Mutex

	syncRun       bool
	syn           *syncer // движок синхронизации: правила, состояние, обмен с сервером
	host          string
	port          int
	localDir      string
	interval      int
	token         string
	shotSync      bool // сохранять снимки экрана в папку синхронизации
	editAfterShot bool // открывать редактор после снимка
	autoRun       bool // начинать синхронизацию сразу при открытии программы
	hotkeys       HotkeyConfig

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
	autoStartSet bool // -autostart передали явно в командной строке
}

func NewApp() *App {
	home, _ := os.UserHomeDir()
	a := &App{
		host:     "localhost",
		port:     8090,
		localDir: filepath.Join(home, "POSCloud", "sync"),
		interval: 3,
		shotSync: true,
		autoRun:  true,
		hotkeys:  DefaultHotkeys(),
	}
	a.syn = newSyncer(a.syncConfig(), func(s string) { a.log(s) }, func(syncConfig) { a.persistConfig() })
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.openLogFile()
	go a.logPruneLoop()
	a.loadConfig()
	a.applyFlagOverrides()
	// Явно переданный -autostart важнее сохранённой настройки.
	if a.autoStartSet {
		a.autoRun = a.autoStart
	}
	a.startHotkeys()
	a.log("Клиент POSCloud инициализирован")
	if a.autoRun {
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
	// nil — запуск при открытии программы включён по умолчанию
	AutoRun *bool `json:"autoRun,omitempty"`
	// nil — сочетания не менялись, остаются стандартные
	Hotkeys *HotkeyConfig `json:"hotkeys,omitempty"`
	// nil — редактор после снимка выключен (по умолчанию сразу сохраняем)
	EditAfterShot *bool `json:"editAfterShot,omitempty"`
	// nil — правила папок не задавались (новые папки ничего не передают)
	FolderRules []FolderRule `json:"folderRules,omitempty"`
	// nil — режим «без правил» не включался
	RulesFree *bool `json:"rulesFree,omitempty"`
	// nil — разовая миграция правил ещё не выполнялась
	RuleDefaultsMigrated *bool `json:"ruleDefaultsMigrated,omitempty"`
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
		if c.AutoRun != nil {
			a.autoRun = *c.AutoRun
		}
		if c.Hotkeys != nil {
			a.hotkeys = *c.Hotkeys
		}
		if c.EditAfterShot != nil {
			a.editAfterShot = *c.EditAfterShot
		}
		cfg := a.syn.Config()
		cfg.Host, cfg.Port, cfg.Dir, cfg.Interval, cfg.Token = a.host, a.port, a.localDir, a.interval, a.token
		cfg.Rules = c.FolderRules
		if c.RulesFree != nil {
			cfg.RulesFree = *c.RulesFree
		}
		if c.RuleDefaultsMigrated != nil {
			cfg.RuleDefaultsMigrated = *c.RuleDefaultsMigrated
		}
		a.syn.Update(cfg)
	}
}

// syncConfig собирает движковую часть настроек из полей приложения и движка.
func (a *App) syncConfig() syncConfig {
	cfg := syncConfig{Host: a.host, Port: a.port, Dir: a.localDir, Interval: a.interval, Token: a.token}
	if a.syn != nil {
		cur := a.syn.Config()
		cfg.Rules, cfg.RulesFree, cfg.RuleDefaultsMigrated = cur.Rules, cur.RulesFree, cur.RuleDefaultsMigrated
	}
	return cfg
}

func (a *App) persistConfig() {
	cfg := a.syn.Config()
	p := filepath.Join(a.root(), "client.json")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	b, _ := json.MarshalIndent(savedConfig{Host: a.host, Port: a.port, Dir: a.localDir, Interval: a.interval, Token: a.token, ScreenshotSync: &a.shotSync, AutoRun: &a.autoRun, Hotkeys: &a.hotkeys, EditAfterShot: &a.editAfterShot, FolderRules: cfg.Rules, RulesFree: &cfg.RulesFree, RuleDefaultsMigrated: &cfg.RuleDefaultsMigrated}, "", "  ")
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
	a.syn.Stop()
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

// pruneLogFile оставляет в файле журнала только последние 3 дня.
func (a *App) pruneLogFile() {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.logPath()
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	kept := make([]string, 0, 512)
	for _, ln := range strings.Split(string(b), "\n") {
		if len(ln) >= 10 && ln[4] == '-' && ln[7] == '-' && ln[:10] < cutoff {
			continue
		}
		kept = append(kept, ln)
	}
	if len(kept) > 20000 {
		kept = kept[len(kept)-20000:]
	}
	res := []byte(strings.Join(kept, "\n"))
	if len(res) == len(b) {
		return
	}
	if a.logFile != nil {
		_ = a.logFile.Close()
		a.logFile = nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, res, 0644); err == nil {
		_ = os.Rename(tmp, p)
	}
	if f, oerr := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); oerr == nil {
		a.logFile = f
	}
}

// logPruneLoop режет журнал при запуске и затем раз в час.
func (a *App) logPruneLoop() {
	a.pruneLogFile()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		a.pruneLogFile()
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

// ---------- состояние и управление ----------

func (a *App) GetClientState() ClientState {
	stt := a.syn.Status()
	a.mu.Lock()
	st := ClientState{Running: a.syncRun, Host: a.host, Port: a.port, Token: a.token, LocalDir: a.localDir,
		Interval: a.interval, LastSync: stt.LastSync, Error: stt.ErrMsg, Count: stt.Count, Online: stt.Online}
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
	return testConnection(host, port, token)
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
	a.syncRun = true
	a.mu.Unlock()

	a.syn.Update(a.syncConfig())
	a.syn.Run()
	a.persistConfig()
	return a.GetClientState()
}

func (a *App) StopSync() ClientState {
	a.mu.Lock()
	a.syncRun = false
	a.mu.Unlock()
	a.syn.Stop()
	return a.GetClientState()
}

func (a *App) SyncNow() ClientState {
	a.log("ручная синхронизация")
	go a.syn.SyncOnce(true)
	return a.GetClientState()
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
	go a.syn.SyncOnce(false)
	return nil
}

// ---------- режимы папок: тонкие обёртки над движком ----------

// ListFolderRules возвращает правила папок (для интерфейса).
func (a *App) ListFolderRules() []FolderRule { return a.syn.ListRules() }

// AddFolderRule добавляет правило для папки; если правило уже есть — меняет режим.
func (a *App) AddFolderRule(path, mode string) error { return a.syn.AddRule(path, mode) }

// SetFolderRuleMode меняет режим существующего правила.
func (a *App) SetFolderRuleMode(path, mode string) error { return a.syn.SetRuleMode(path, mode) }

// RemoveFolderRule убирает правило папки.
func (a *App) RemoveFolderRule(path string) error { return a.syn.RemoveRule(path) }

// RulesFreeEnabled сообщает, включён ли режим «без правил».
func (a *App) RulesFreeEnabled() bool { return a.syn.RulesFreeEnabled() }

// SetRulesFree включает/выключает режим «без правил» (всё в обе стороны).
func (a *App) SetRulesFree(on bool) error { return a.syn.SetRulesFree(on) }

// ResyncServer перечитывает полный список файлов сервера и запускает синхронизацию.
func (a *App) ResyncServer() { a.syn.Resync() }

// ServerFolders отдаёт интерфейсу список папок первого уровня со статусами.
func (a *App) ServerFolders() ([]ServerFolderNode, error) { return a.syn.ServerFolders() }

// ClearLocalCopy удаляет локальные файлы папки — данные на сервере остаются.
func (a *App) ClearLocalCopy(rel string) (int, error) { return a.syn.ClearLocalCopy(rel) }

// DeleteServerFolder удаляет папку на сервере, её локальную копию и правило.
func (a *App) DeleteServerFolder(rel string) (int, error) { return a.syn.DeleteServerFolder(rel) }

// eventsLoop держит подключение к /events и запускает синхронизацию по событию.
// AutostartEnabled сообщает, включён ли автозапуск при входе в Windows.
// AutoRunEnabled сообщает, начинается ли синхронизация сразу при открытии программы.
func (a *App) AutoRunEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.autoRun
}

// SetAutoRun включает и выключает старт синхронизации при открытии программы.
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
