package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
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

type App struct {
	ctx context.Context
	mu  sync.Mutex

	syncRun              bool
	syncMu               sync.Mutex
	host                 string
	port                 int
	localDir             string
	interval             int
	token                string
	shotSync             bool // сохранять снимки экрана в папку синхронизации
	editAfterShot        bool // открывать редактор после снимка
	autoRun              bool // начинать синхронизацию сразу при открытии программы
	hotkeys              HotkeyConfig
	folderRules          []FolderRule // режимы папок первого уровня (выборочная синхронизация)
	rulesFree            bool         // режим «без правил»: синхронизировать всё в обе стороны
	ruleDefaultsMigrated bool         // разовая миграция: закрепить режимы уже синхронизируемых папок
	needManifest         bool         // перечитать полный манифест на следующем цикле
	stopCh               chan struct{}
	lastSync             string
	errMsg               string
	count                int
	online               bool

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
	return &App{
		host:     "localhost",
		port:     8090,
		localDir: filepath.Join(home, "POSCloud", "sync"),
		interval: 3,
		shotSync: true,
		autoRun:  true,
		hotkeys:  DefaultHotkeys(),
	}
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
		a.folderRules = c.FolderRules
		if c.RulesFree != nil {
			a.rulesFree = *c.RulesFree
		}
		if c.RuleDefaultsMigrated != nil {
			a.ruleDefaultsMigrated = *c.RuleDefaultsMigrated
		}
	}
}

func (a *App) persistConfig() {
	p := filepath.Join(a.root(), "client.json")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	b, _ := json.MarshalIndent(savedConfig{Host: a.host, Port: a.port, Dir: a.localDir, Interval: a.interval, Token: a.token, ScreenshotSync: &a.shotSync, AutoRun: &a.autoRun, Hotkeys: &a.hotkeys, EditAfterShot: &a.editAfterShot, FolderRules: a.folderRules, RulesFree: &a.rulesFree, RuleDefaultsMigrated: &a.ruleDefaultsMigrated}, "", "  ")
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

func (a *App) baseURL() string {
	a.mu.Lock()
	h, p := a.host, a.port
	a.mu.Unlock()
	return fmt.Sprintf("http://%s:%d", h, p)
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
	list := append([]FolderRule(nil), a.folderRules...)
	free := a.rulesFree
	needMF := a.needManifest
	a.needManifest = false
	a.mu.Unlock()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0755)
	base := a.baseURL()
	rules := parseFolderRules(list)
	rules.Free = free

	st := loadState(dir)
	if a.migrateRuleDefaults(st) {
		a.mu.Lock()
		list = append([]FolderRule(nil), a.folderRules...)
		a.mu.Unlock()
		rules = parseFolderRules(list)
		rules.Free = free
	}

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
	if needMF {
		if mv, mf, err := fetchManifest(base, tok); err == nil {
			for _, m := range mf {
				if _, ok := sanitizeRel(m.Name); !ok {
					continue
				}
				if _, ok := srv[m.Name]; !ok {
					srv[m.Name] = m
				}
			}
			if mv > srvVer {
				srvVer = mv
			}
			a.log(fmt.Sprintf("полная сверка с сервером: файлов %d", len(mf)))
		} else {
			a.log("не удалось перечитать манифест: " + err.Error())
		}
	}
	local := localScan(dir, st, rules)

	dirNames := map[string]bool{}
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
	up, down, del, skipped := 0, 0, 0, 0

	for name := range names {
		ls, hasLocal := local[name]
		ss, hasState := st.Files[name]
		cs, hasSrv := srv[name]

		// записи о папках обрабатываем отдельным проходом ниже
		if (hasSrv && cs.Dir) || (hasState && ss.Dir) {
			dirNames[name] = true
			continue
		}
		// файл-артефакт от устаревшего клиента на месте локальной папки:
		// разбираемся в отдельном проходе, как с папками
		if hasSrv && !cs.Dir {
			if fi, err := os.Stat(localPath(dir, name)); err == nil && fi.IsDir() {
				dirNames[name] = true
				continue
			}
		}

		mode := rules.ModeForFile(name)
		if mode == folderOff {
			// папка отключена: файл не трогаем, прежнее состояние бережём на случай включения
			if hasState {
				next.Files[name] = ss
			}
			skipped++
			continue
		}
		// carry переносит прежнее состояние, когда файл сейчас не трогаем.
		carry := func() {
			if hasState {
				next.Files[name] = ss
			}
		}
		allowUp := mode == folderBoth || mode == folderUp
		allowDown := mode == folderBoth || mode == folderDown

		switch {
		// --- сервер удалил файл (tombstone) ---
		case hasSrv && cs.Deleted:
			if !allowDown {
				// «только отправлять»: удаления с сервера не применяем
				carry()
				skipped++
				continue
			}
			if hasLocal {
				if hasState && ls.Hash == ss.Hash {
					_ = os.Remove(localPath(dir, name))
					pruneEmptyDirs(dir, localPath(dir, name))
					del++
					a.log("вниз удаление: " + name)
				} else {
					cf := conflictName(name)
					_ = os.Rename(localPath(dir, name), localPath(dir, cf))
					if mode == folderBoth {
						if h, v, mt, err := uploadFile(base, dir, cf, tok); err == nil {
							next.Files[cf] = stateFile{Hash: h, Size: fileSize(localPath(dir, cf)), MTime: mt, Version: v}
							up++
						}
					}
					a.log("конфликт (удаление vs правка): " + name + " -> " + cf)
				}
			}

		// --- у сервера есть файл ---
		case hasSrv && !cs.Deleted:
			switch {
			case !hasLocal:
				if !allowDown {
					// «только отправлять»: вниз не качаем; если файл был и удалён у нас — удаляем и на сервере
					if hasState {
						if err := deleteRemote(base, name, tok); err == nil {
							del++
							a.log("вверх удаление: " + name)
						} else {
							carry()
						}
					}
					skipped++
					continue
				}
				if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
					next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
					down++
					a.log("вниз: " + name)
				} else {
					carry()
				}
			case ls.Hash == cs.Hash:
				next.Files[name] = stateFile{Hash: ls.Hash, Size: ls.Size, MTime: ls.MTime, Version: cs.Version}
			case !hasState:
				// первый синк: локальная и серверная версии расходятся
				switch mode {
				case folderUp:
					// «только отправлять»: источник — это устройство, выгружаем локальную версию
					if h, v, mt, err := uploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = stateFile{Hash: h, Size: fileSize(localPath(dir, name)), MTime: mt, Version: v}
						up++
						a.log("вверх: " + name)
					}
				case folderDown:
					// «только получать»: локальные правки не выгружаем, но бережём копией
					cf := conflictName(name)
					if data, err := os.ReadFile(localPath(dir, name)); err == nil {
						_ = os.WriteFile(localPath(dir, cf), data, 0644)
					}
					if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
					}
					a.log("конфликт (первый синк, получающая папка): " + name + " -> " + cf)
				default:
					// в обе стороны: делаем конфликтную копию, чтобы не затереть серверную версию
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
				}
			case hasState && ls.Hash == ss.Hash:
				// локально без изменений, сервер новее
				if allowDown {
					if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
						a.log("вниз: " + name)
					} else {
						carry()
					}
				} else {
					// «только отправлять»: новую серверную версию не принимаем
					carry()
					skipped++
				}
			case hasState && cs.Version > ss.Version:
				// обе стороны изменили
				switch mode {
				case folderUp:
					// «только отправлять»: побеждает локальная версия
					if h, v, mt, err := uploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = stateFile{Hash: h, Size: fileSize(localPath(dir, name)), MTime: mt, Version: v}
						up++
						a.log("вверх (приоритет устройства): " + name)
					}
				case folderDown:
					// «только получать»: ставим серверную версию, локальные правки сохраняем копией
					cf := conflictName(name)
					_ = os.Rename(localPath(dir, name), localPath(dir, cf))
					if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
					}
					a.log("конфликт (получающая папка): " + name + " -> " + cf)
				default:
					// в обе стороны: конфликтная копия + серверная версия
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
				}
			default:
				// локально изменено, сервер не менял -> выгружаем
				if allowUp {
					if h, v, mt, err := uploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = stateFile{Hash: h, Size: fileSize(localPath(dir, name)), MTime: mt, Version: v}
						up++
						a.log("вверх: " + name)
					} else {
						carry()
					}
				} else {
					// «только получать»: локальные правки возвращаем к серверной версии
					if err := downloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = stateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
						a.log("вниз (восстановление): " + name)
					} else {
						carry()
					}
				}
			}

		// --- сервер не менял этот файл с прошлого раза ---
		default:
			if hasLocal {
				if hasState && ls.Hash == ss.Hash {
					next.Files[name] = stateFile{Hash: ls.Hash, Size: ls.Size, MTime: ls.MTime, Version: ss.Version}
				} else if allowUp {
					if h, v, mt, err := uploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = stateFile{Hash: h, Size: fileSize(localPath(dir, name)), MTime: mt, Version: v}
						up++
						a.log("вверх: " + name)
					} else {
						carry()
					}
				} else if hasState {
					// «только получать»: изменённый файл возвращаем к серверной версии
					if err := downloadFile(base, dir, name, ss.MTime, tok); err == nil {
						next.Files[name] = ss
						down++
						a.log("вниз (восстановление): " + name)
					} else {
						carry()
					}
				} else {
					// «только получать»: новый локальный файл не выгружаем — оставляем как есть
					carry()
					skipped++
				}
			} else if hasState {
				if allowUp {
					// локально удалён, сервер не менял -> удаляем на сервере (tombstone)
					if err := deleteRemote(base, name, tok); err == nil {
						del++
						a.log("вверх удаление: " + name)
						pruneEmptyDirs(dir, localPath(dir, name))
					} else {
						carry()
					}
				} else {
					// «только получать»: локальное удаление не уходит на сервер — файл возвращается
					if err := downloadFile(base, dir, name, ss.MTime, tok); err == nil {
						next.Files[name] = ss
						down++
						a.log("вниз (восстановление): " + name)
					} else if strings.Contains(err.Error(), "404") {
						// файла на сервере уже нет — считаем, что удаление состоялось
						del++
					} else {
						carry()
					}
				}
			}
		}
	}

	// ==== папки: перенос пустых каталогов ====
	for n := range dirNames {
		ss, hasState := st.Files[n]
		cs, hasSrv := srv[n]
		mode := rules.ModeFor(n)
		if mode == folderOff {
			if hasState {
				next.Files[n] = ss
			}
			skipped++
			continue
		}
		allowUp := mode == folderBoth || mode == folderUp
		allowDown := mode == folderBoth || mode == folderDown
		lp := localPath(dir, n)
		_, statErr := os.Stat(lp)
		hasLocal := statErr == nil
		switch {
		case hasSrv && cs.Deleted && cs.Dir:
			if !allowDown {
				if hasState {
					next.Files[n] = ss
				}
				skipped++
				continue
			}
			removedLocal := !hasLocal
			if hasLocal {
				if err := os.Remove(lp); err == nil {
					pruneEmptyDirs(dir, lp)
					del++
					a.log("вниз удаление (папка): " + n)
					removedLocal = true
				}
			}
			if removedLocal {
				a.removeRuleForDeletedFolder(n)
			}
		case hasSrv && !cs.Deleted && cs.Dir:
			if !hasLocal && allowDown {
				if err := os.MkdirAll(lp, 0755); err == nil {
					next.Files[n] = stateFile{Dir: true, Version: cs.Version}
					down++
					a.log("вниз папка: " + n)
				} else if hasState {
					next.Files[n] = ss
				}
			} else if hasLocal {
				next.Files[n] = stateFile{Dir: true, Version: cs.Version}
			} else if hasState {
				next.Files[n] = ss
			}
		case hasSrv && !cs.Dir:
			// на сервере на месте папки файл-артефакт — заново регистрируем папку
			if hasLocal && allowUp {
				if v, err := mkdirRemote(base, n, tok); err == nil {
					next.Files[n] = stateFile{Dir: true, Version: v}
					up++
					a.log("вверх папка (заново): " + n)
				} else if hasState {
					next.Files[n] = ss
				}
			} else if hasState {
				next.Files[n] = ss
			}
		default:
			if hasLocal {
				if hasState {
					next.Files[n] = ss
				} else if allowUp {
					if v, err := mkdirRemote(base, n, tok); err == nil {
						next.Files[n] = stateFile{Dir: true, Version: v}
						up++
						a.log("вверх папка: " + n)
					}
				}
			} else if hasState {
				if allowUp {
					if err := deleteRemote(base, n, tok); err == nil {
						del++
						a.log("вверх удаление (папка): " + n)
						a.removeRuleForDeletedFolder(n)
					} else {
						next.Files[n] = ss
					}
				} else {
					// «только получать»: удаление папки не уходит на сервер
					if err := os.MkdirAll(lp, 0755); err == nil {
						down++
					}
					next.Files[n] = ss
				}
			}
		}
	}
	// новые локальные папки, которых ещё нет ни в состоянии, ни на сервере
	for _, n := range localDirs(dir, rules) {
		if dirNames[n] || stateCoversDir(next.Files, n) {
			continue
		}
		mode := rules.ModeFor(n)
		if mode != folderBoth && mode != folderUp {
			continue
		}
		if v, err := mkdirRemote(base, n, tok); err == nil {
			next.Files[n] = stateFile{Dir: true, Version: v}
			up++
			a.log("вверх папка: " + n)
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
		a.log(fmt.Sprintf("синк: сервер %s, локальных %d, изменений %d, вверх %d, вниз %d, удалено %d, пропущено по режимам %d",
			base, len(local), len(changes), up, down, del, skipped))
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

			if rel, rerr := filepath.Rel(dir, ev.Name); rerr == nil && a.folderRulesSnapshot().IsOff(filepath.ToSlash(rel)) {

				continue // папка отключена от синхронизации

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

// ---------- режимы папок (выборочная синхронизация) ----------

// folderRuleIndexLocked ищет правило по пути без учёта регистра. Вызывать под a.mu.
func (a *App) folderRuleIndexLocked(p string) int {
	low := strings.ToLower(p)
	for i, r := range a.folderRules {
		if strings.ToLower(strings.Trim(r.Path, "/")) == low {
			return i
		}
	}
	return -1
}

func (a *App) folderRulesSnapshot() folderRules {
	a.mu.Lock()
	list := append([]FolderRule(nil), a.folderRules...)
	a.mu.Unlock()
	return parseFolderRules(list)
}

// ListFolderRules возвращает правила папок (для интерфейса).
func (a *App) ListFolderRules() []FolderRule {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]FolderRule, 0, len(a.folderRules))
	out = append(out, a.folderRules...)
	return out
}

// AddFolderRule добавляет правило для подпапки; если правило уже есть — меняет режим.
func (a *App) AddFolderRule(path, mode string) error {
	clean, ok := normalizeRulePath(path)
	if !ok {
		return fmt.Errorf("недопустимый путь папки")
	}
	if !validFolderMode(mode) {
		return fmt.Errorf("недопустимый режим")
	}
	a.mu.Lock()
	if i := a.folderRuleIndexLocked(clean); i >= 0 {
		a.folderRules[i].Mode = mode
	} else {
		a.folderRules = append(a.folderRules, FolderRule{Path: clean, Mode: mode})
	}
	a.mu.Unlock()
	a.persistConfig()
	a.log("папка «" + clean + "»: режим — " + folderModeTitle(mode))
	a.markManifestResync()
	go a.syncOnce(false)
	return nil
}

// SetFolderRuleMode меняет режим существующего правила.
func (a *App) SetFolderRuleMode(path, mode string) error {
	clean, ok := normalizeRulePath(path)
	if !ok {
		return fmt.Errorf("недопустимый путь папки")
	}
	if !validFolderMode(mode) {
		return fmt.Errorf("недопустимый режим")
	}
	a.mu.Lock()
	i := a.folderRuleIndexLocked(clean)
	if i < 0 {
		a.mu.Unlock()
		return fmt.Errorf("правило для папки не найдено")
	}
	a.folderRules[i].Mode = mode
	a.mu.Unlock()
	a.persistConfig()
	a.log("папка «" + clean + "»: режим — " + folderModeTitle(mode))
	a.markManifestResync()
	go a.syncOnce(false)
	return nil
}

// RemoveFolderRule убирает правило: папка снова синхронизируется в обе стороны.
func (a *App) RemoveFolderRule(path string) error {
	clean, ok := normalizeRulePath(path)
	if !ok {
		return fmt.Errorf("недопустимый путь папки")
	}
	a.mu.Lock()
	i := a.folderRuleIndexLocked(clean)
	if i < 0 {
		a.mu.Unlock()
		return fmt.Errorf("правило для папки не найдено")
	}
	a.folderRules = append(a.folderRules[:i], a.folderRules[i+1:]...)
	a.mu.Unlock()
	a.persistConfig()
	a.log("папка «" + clean + "»: правило убрано")
	a.markManifestResync()
	go a.syncOnce(false)
	return nil
}

// removeRuleForDeletedFolder убирает правило папки, когда её удаление уже
// разъехалось по устройствам: чтобы в списке не оставалась строка-призрак.
func (a *App) removeRuleForDeletedFolder(rel string) {
	a.mu.Lock()
	i := a.folderRuleIndexLocked(rel)
	var removed string
	if i >= 0 {
		removed = a.folderRules[i].Path
		a.folderRules = append(a.folderRules[:i], a.folderRules[i+1:]...)
	}
	a.mu.Unlock()
	if removed != "" {
		a.persistConfig()
		a.log("папка «" + removed + "» удалена — правило убрано")
	}
}

// markManifestResync ставит пометку «перечитать полный манифест»: так после
// смены правил клиент сразу подтягивает всё, что теперь разрешено.
func (a *App) markManifestResync() {
	a.mu.Lock()
	a.needManifest = true
	a.mu.Unlock()
}

// migrateRuleDefaults — разовая миграция при первом запуске новой схемы:
//  1. правила глубже первого уровня больше не поддерживаются — убираем их;
//  2. папкам первого уровня, уже участвовавшим в синхронизации (есть записи
//     состояния), закрепляем «в обе стороны», чтобы их поведение не изменилось.
//     Все новые папки при этом остаются с режимом «не синхронизировать».
//
// Возвращает true, если правила изменились.
func (a *App) migrateRuleDefaults(st clientState) bool {
	a.mu.Lock()
	if a.ruleDefaultsMigrated {
		a.mu.Unlock()
		return false
	}
	a.ruleDefaultsMigrated = true
	existing := append([]FolderRule(nil), a.folderRules...)
	a.mu.Unlock()

	// корневые папки, уже освоенные этим компьютером (по записям состояния):
	// файлы внутри папок и записи самих папок
	roots := map[string]string{} // нижний регистр -> имя как в состоянии
	for k, sf := range st.Files {
		k = filepath.ToSlash(k)
		if i := strings.Index(k, "/"); i > 0 {
			roots[strings.ToLower(k[:i])] = k[:i]
		} else if sf.Dir {
			roots[strings.ToLower(k)] = k
		}
	}

	var kept []FolderRule
	dropped := 0
	for _, r := range existing {
		clean, ok := normalizeRulePath(r.Path)
		if !ok || strings.Contains(clean, "/") {
			dropped++
			continue
		}
		kept = append(kept, FolderRule{Path: clean, Mode: r.Mode})
	}
	has := func(p string) bool {
		for _, r := range kept {
			if strings.EqualFold(r.Path, p) {
				return true
			}
		}
		return false
	}
	pinned := 0
	for _, name := range roots {
		if has(name) {
			continue
		}
		kept = append(kept, FolderRule{Path: name, Mode: folderBoth})
		pinned++
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Path < kept[j].Path })

	a.mu.Lock()
	a.folderRules = kept
	a.mu.Unlock()
	a.persistConfig()
	if dropped > 0 || pinned > 0 {
		a.log(fmt.Sprintf("миграция правил папок: убрано вложенных %d, закреплено прежних %d", dropped, pinned))
	}
	return dropped > 0 || pinned > 0
}

// RulesFreeEnabled сообщает, включён ли режим «без правил» (синхронизируется всё).
func (a *App) RulesFreeEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rulesFree
}

// SetRulesFree включает/выключает режим «без правил»: всё содержимое папки
// синхронизируется в обе стороны, правила папок не действуют.
func (a *App) SetRulesFree(on bool) error {
	a.mu.Lock()
	a.rulesFree = on
	a.mu.Unlock()
	a.persistConfig()
	if on {
		a.log("режим без правил включён: синхронизируется всё в обе стороны")
	} else {
		a.log("режим без правил выключен: действуют правила папок")
	}
	a.markManifestResync()
	go a.syncOnce(false)
	return nil
}

// ResyncServer перечитывает полный список файлов сервера и запускает синхронизацию.
func (a *App) ResyncServer() {
	a.markManifestResync()
	go a.syncOnce(true)
}

// ServerFolderNode — строка списка синхронизации: папка первого уровня со статусом.
type ServerFolderNode struct {
	Path     string `json:"path"`
	Files    int    `json:"files"`    // файлов на сервере (по всей папке)
	Bytes    int64  `json:"bytes"`    // суммарный размер на сервере
	LocFiles int    `json:"locFiles"` // файлов на этом ПК (по всей папке)
	LocBytes int64  `json:"locBytes"` // суммарный размер на этом ПК
	Local    bool   `json:"local"`    // копия есть на этом ПК
	Rule     string `json:"rule"`     // явно заданный режим ("" — не задан)
	OnlyRule bool   `json:"onlyRule"` // только правило: папки нет нигде
	New      bool   `json:"new"`      // ещё не синхронизировалась и режим не выбран
	Mode     string `json:"mode"`     // действующий режим
}

// ServerFolders отдаёт интерфейсу список папок первого уровня со статусами:
// папки на сервере и на этом ПК плюс сохранённые правила. Вложенные папки в
// список не попадают — они наследуют режим своей папки первого уровня.
// Полный манифест запрашивается по требованию (при открытии закладки).
func (a *App) ServerFolders() ([]ServerFolderNode, error) {
	a.mu.Lock()
	dir := a.localDir
	tok := a.token
	base := fmt.Sprintf("http://%s:%d", a.host, a.port)
	a.mu.Unlock()
	if dir == "" {
		return nil, fmt.Errorf("не задана папка синхронизации")
	}
	_, files, err := fetchManifest(base, tok)
	if err != nil {
		return nil, err
	}
	st := loadState(dir)
	a.migrateRuleDefaults(st)
	a.mu.Lock()
	list := append([]FolderRule(nil), a.folderRules...)
	free := a.rulesFree
	a.mu.Unlock()
	rules := parseFolderRules(list)
	rules.Free = free

	type topRow struct {
		name  string
		srv   bool
		files int
		bytes int64
		loc   bool
	}
	rows := map[string]*topRow{}
	get := func(raw string) *topRow {
		k := strings.ToLower(raw)
		r := rows[k]
		if r == nil {
			r = &topRow{name: raw}
			rows[k] = r
		}
		return r
	}
	for _, f := range files {
		if _, ok := sanitizeRel(f.Name); !ok {
			continue
		}
		parts := strings.Split(f.Name, "/")
		if len(parts) < 2 && !f.Dir {
			continue // файлы в корне в списке папок не показываем
		}
		r := get(parts[0])
		r.srv = true
		if !f.Dir {
			r.files++
			r.bytes += f.Size
		}
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			get(e.Name()).loc = true
		}
	}
	// правила для папок, которых сейчас нет ни на сервере, ни здесь
	ruleOnly := map[string]bool{}
	for _, r := range list {
		p, ok := normalizeRulePath(r.Path)
		if !ok || strings.Contains(p, "/") {
			continue
		}
		k := strings.ToLower(p)
		if _, ok := rows[k]; !ok {
			rows[k] = &topRow{name: p}
			ruleOnly[k] = true
		}
	}
	// корневые папки, уже участвовавшие в синхронизации (по состоянию):
	// файлы внутри папок и записи самих папок
	stateTops := map[string]bool{}
	for k, sf := range st.Files {
		k = filepath.ToSlash(k)
		if i := strings.Index(k, "/"); i > 0 {
			stateTops[strings.ToLower(k[:i])] = true
		} else if sf.Dir {
			stateTops[strings.ToLower(k)] = true
		}
	}

	out := make([]ServerFolderNode, 0, len(rows))
	for k, r := range rows {
		node := ServerFolderNode{Path: r.name, Files: r.files, Bytes: r.bytes}
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(r.name))); err == nil && fi.IsDir() {
			node.Local = true
		} else if hasStatePrefix(st, r.name) {
			node.Local = true
		}
		if r.loc {
			node.LocFiles, node.LocBytes = dirStats(filepath.Join(dir, filepath.FromSlash(r.name)))
		}
		if m, ok := rules.Explicit(r.name); ok {
			node.Rule = m
		}
		node.Mode = rules.ModeFor(r.name)
		node.OnlyRule = ruleOnly[k] && !r.srv && !node.Local
		node.New = !rules.Covered(r.name) && !stateTops[k]
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ClearLocalCopy удаляет локальные файлы папки (данные остаются на сервере)
// и забывает её состояние, чтобы синхронизация не сочла файлы удалёнными на сервере.
func (a *App) ClearLocalCopy(rel string) (int, error) {
	clean, ok := normalizeRulePath(rel)
	if !ok {
		return 0, fmt.Errorf("недопустимый путь папки")
	}
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()
	if dir == "" {
		return 0, fmt.Errorf("не задана папка синхронизации")
	}
	root := filepath.Clean(filepath.Join(dir, filepath.FromSlash(clean)))
	if root == filepath.Clean(dir) || !strings.HasPrefix(root, filepath.Clean(dir)+string(filepath.Separator)) {
		return 0, fmt.Errorf("недопустимый путь папки")
	}
	n := 0
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			dirs = append(dirs, p)
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if os.Remove(p) == nil {
			n++
		}
		return nil
	})
	// убрать опустевшие каталоги (снизу вверх)
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	st := loadState(dir)
	lp := strings.ToLower(clean)
	prefix := lp + "/"
	removed := 0
	for k := range st.Files {
		lk := strings.ToLower(filepath.ToSlash(k))
		if lk == lp || strings.HasPrefix(lk, prefix) {
			delete(st.Files, k)
			removed++
		}
	}
	if removed > 0 {
		saveState(dir, st)
	}
	cur := root
	for cur != filepath.Clean(dir) && strings.HasPrefix(cur, filepath.Clean(dir)+string(filepath.Separator)) {
		if err := os.Remove(cur); err != nil {
			break
		}
		cur = filepath.Dir(cur)
	}
	a.log(fmt.Sprintf("убрана локальная копия папки «%s»: файлов %d", clean, n))
	return n, nil
}

// DeleteServerFolder удаляет папку на сервере (все её файлы), убирает локальную
// копию и правила для неё. Данные исчезают и с других устройств — кнопка
// «Удалить с сервера» на закладке «Синхронизация».
func (a *App) DeleteServerFolder(rel string) (int, error) {
	clean, ok := normalizeRulePath(rel)
	if !ok {
		return 0, fmt.Errorf("недопустимый путь папки")
	}
	a.mu.Lock()
	tok := a.token
	base := fmt.Sprintf("http://%s:%d", a.host, a.port)
	a.mu.Unlock()
	if tok == "" {
		return 0, fmt.Errorf("не задан токен доступа")
	}
	_, files, err := fetchManifest(base, tok)
	if err != nil {
		return 0, err
	}
	lp := strings.ToLower(clean)
	prefix := lp + "/"
	n := 0
	for _, f := range files {
		lf := strings.ToLower(f.Name)
		if lf != lp && !strings.HasPrefix(lf, prefix) {
			continue
		}
		if err := deleteRemote(base, f.Name, tok); err != nil {
			a.log(fmt.Sprintf("удаление папки «%s» прервано на файле %s: %v", clean, f.Name, err))
			return n, err
		}
		n++
	}
	if _, err := a.ClearLocalCopy(clean); err != nil {
		a.log("удаление папки «" + clean + "»: локальная копия не убрана: " + err.Error())
	}
	a.mu.Lock()
	out := a.folderRules[:0]
	removedRules := 0
	for _, r := range a.folderRules {
		rl := strings.ToLower(r.Path)
		if rl == lp || strings.HasPrefix(rl, lp+"/") {
			removedRules++
			continue
		}
		out = append(out, r)
	}
	a.folderRules = out
	a.mu.Unlock()
	if removedRules > 0 {
		a.persistConfig()
	}
	a.log(fmt.Sprintf("папка «%s» удалена с сервера: файлов %d, правил убрано %d", clean, n, removedRules))
	a.markManifestResync()
	return n, nil
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
