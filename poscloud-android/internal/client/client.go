// POSCloud Client for Android — controller: settings, sync engine,
// log and transfer progress. The logic does not depend on the interface, so the same
// controller is also suitable for a desktop build. Synchronization is performed by the shared
// engine poscloud-engine — the same one used by the desktop client and Lite.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	eng "poscloud-engine"

	"poscloud-client-android/internal/camera"
)

// androidConfig — Android client settings: the engine part (compatible with the
// client.json schema of the desktop client, so the file can be transferred) plus
// mobile additions.
type androidConfig struct {
	eng.Config
	AutoRun    *bool  `json:"autoRun,omitempty"`    // start synchronization when the app launches
	DeviceName string `json:"deviceName,omitempty"` // device name when connecting via code
}

// Controller links the sync engine with settings and the log.
type Controller struct {
	mu       sync.Mutex
	syn      *eng.Syncer
	cfg      androidConfig
	running  bool
	logs     []string
	root     string
	shotErr  string
	permAsks int // сколько раз просили разрешение на чтение фото

	logDirty bool
}

// NewController creates a controller in the app data directory root:
// client.json, client.log and the default sync folder are stored there.
func NewController(root string) *Controller {
	c := &Controller{root: root}
	_ = os.MkdirAll(root, 0755)
	c.cfg.Host = "localhost"
	c.cfg.Port = 8090
	c.cfg.Interval = 3
	c.cfg.Dir = c.DefaultSyncDir()
	c.loadConfig()
	// TLS настраиваем сразу: вкладка «Папки» ходит на сервер ещё до
	// запуска синхронизации и иначе падает на самоподписанном сертификате.
	if c.cfg.TLS {
		if err := eng.ConfigureTLS(c.cfg.TLSFingerprint); err != nil {
			c.Log("TLS: " + err.Error())
		}
	}
	c.syn = eng.NewSyncer(c.cfg.Config, c.Log, func(eng.Config) { c.persist() })
	c.permAsks = c.loadScreenshotState().PermAsks
	c.Log("Клиент POSCloud для Android запущен; данные: " + root)
	return c
}

// Root returns the app data directory.
func (c *Controller) Root() string { return c.root }

// DefaultSyncDir — the default sync folder (inside the app directory).
func (c *Controller) DefaultSyncDir() string { return filepath.Join(c.root, "sync") }

// appID — имя пакета Android: по нему находится внешний каталог приложения
// на общей памяти (Android/data/<пакет>/files).
const appID = "io.github.reznikov73.poscloud.client"

// ExternalSyncDir returns the sync folder on shared storage
// (…/Android/data/<package>/files/sync).
// Android lets an app write there without extra permissions, and the folder is
// visible over USB and in file managers — unlike the private app directory.
// An empty result means no writable shared directory was found.
func (c *Controller) ExternalSyncDir() string {
	bases := []string{strings.TrimSpace(os.Getenv("EXTERNAL_STORAGE")), "/storage/emulated/0", "/sdcard"}
	seen := make(map[string]bool, len(bases))
	for _, base := range bases {
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		dir := filepath.Join(base, "Android", "data", appID, "files", "sync")
		if writableDir(dir) {
			return dir
		}
	}
	return ""
}

// writableDir создаёт каталог при необходимости и проверяет запись пробным файлом.
func writableDir(dir string) bool {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// Config returns a copy of the current settings.
func (c *Controller) Config() androidConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

// HasCredentials reports whether the settings are sufficient to start synchronization.
func (c *Controller) HasCredentials() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.cfg.Token) != "" && strings.TrimSpace(c.cfg.Dir) != ""
}

// AutoRunEnabled reports whether synchronization should start when the app launches.
func (c *Controller) AutoRunEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.AutoRun == nil || *c.cfg.AutoRun
}

// DeviceName returns the device name for connection: the saved one, the host name
// or "Android" if the host name is unavailable (usually "localhost" on Android).
func (c *Controller) DeviceName() string {
	c.mu.Lock()
	name := strings.TrimSpace(c.cfg.DeviceName)
	c.mu.Unlock()
	if name != "" {
		return name
	}
	if h, err := os.Hostname(); err == nil {
		h = strings.TrimSpace(h)
		if h != "" && !strings.EqualFold(h, "localhost") {
			return h
		}
	}
	return "Android"
}

// ---------- loading and saving settings ----------

// loadConfig reads client.json; a missing file means default settings.
func (c *Controller) loadConfig() {
	data, err := os.ReadFile(filepath.Join(c.root, "client.json"))
	if err != nil {
		return
	}
	var cfg androidConfig
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.Host != "" {
		c.cfg.Host = cfg.Host
	}
	if cfg.Port > 0 {
		c.cfg.Port = cfg.Port
	}
	if cfg.Dir != "" {
		c.cfg.Dir = cfg.Dir
	}
	if cfg.Interval > 0 {
		c.cfg.Interval = cfg.Interval
	}
	if cfg.Token != "" {
		c.cfg.Token = cfg.Token
	}
	if cfg.DeviceName != "" {
		c.cfg.DeviceName = cfg.DeviceName
	}
	c.cfg.TLS = cfg.TLS
	c.cfg.TLSFingerprint = cfg.TLSFingerprint
	c.cfg.Rules = cfg.Rules
	c.cfg.RulesFree = cfg.RulesFree
	c.cfg.RuleDefaultsMigrated = cfg.RuleDefaultsMigrated
	if cfg.AutoRun != nil {
		c.cfg.AutoRun = cfg.AutoRun
	}
	if c.cfg.Interval <= 0 {
		c.cfg.Interval = 3
	}
	if c.cfg.Port <= 0 {
		c.cfg.Port = 8090
	}
}

// persist saves settings to client.json. Folder rules are taken from the engine:
// the engine itself may have changed them (migration, folder removal).
func (c *Controller) persist() {
	c.mu.Lock()
	cfg := c.cfg
	c.mu.Unlock()
	ec := c.syn.Config()
	cfg.Rules, cfg.RulesFree, cfg.RuleDefaultsMigrated = ec.Rules, ec.RulesFree, ec.RuleDefaultsMigrated
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	p := filepath.Join(c.root, "client.json")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, b, 0644)
}

// engineConfig assembles engine settings from the controller fields, transferring rules
// from the current engine state (so that Update does not overwrite them).
func (c *Controller) engineConfig() eng.Config {
	c.mu.Lock()
	cfg := c.cfg.Config
	c.mu.Unlock()
	cur := c.syn.Config()
	cfg.Rules, cfg.RulesFree, cfg.RuleDefaultsMigrated = cur.Rules, cur.RulesFree, cur.RuleDefaultsMigrated
	return cfg
}

// ---------- sync control ----------

// SaveSettings saves settings without starting synchronization.
func (c *Controller) SaveSettings(host string, port int, dir string, interval int, token string, tls bool, fingerprint, deviceName string, autoRun bool) {
	c.mu.Lock()
	if host = strings.TrimSpace(host); host != "" {
		c.cfg.Host = host
	}
	if port > 0 && port < 65536 {
		c.cfg.Port = port
	}
	if dir = strings.TrimSpace(dir); dir != "" {
		c.cfg.Dir = dir
	}
	if interval > 0 {
		c.cfg.Interval = interval
	}
	c.cfg.Token = strings.TrimSpace(token)
	c.cfg.TLS = tls
	c.cfg.TLSFingerprint = strings.TrimSpace(fingerprint)
	if deviceName = strings.TrimSpace(deviceName); deviceName != "" {
		c.cfg.DeviceName = deviceName
	}
	c.cfg.AutoRun = &autoRun
	c.mu.Unlock()
	if tls {
		_ = eng.ConfigureTLS(strings.TrimSpace(fingerprint))
	} else {
		_ = eng.ConfigureTLS("")
	}
	c.syn.ResetAuthBackoff()
	c.syn.Update(c.engineConfig())
	c.persist()
	c.Log("настройки сохранены")
}

// Start starts synchronization with the specified parameters.
func (c *Controller) Start(host string, port int, dir string, interval int, token string, tls bool, fingerprint string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("не указан токен доступа — получите его на сервере")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("не выбрана папка синхронизации")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("неверный порт сервера")
	}
	if interval <= 0 {
		interval = 3
	}
	if host = strings.TrimSpace(host); host == "" {
		host = "localhost"
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("синхронизация уже запущена")
	}
	c.cfg.Host, c.cfg.Port, c.cfg.Dir, c.cfg.Interval = host, port, dir, interval
	c.cfg.Token = strings.TrimSpace(token)
	c.cfg.TLS = tls
	c.cfg.TLSFingerprint = strings.TrimSpace(fingerprint)
	c.running = true
	c.mu.Unlock()

	if tls {
		if err := eng.ConfigureTLS(strings.TrimSpace(fingerprint)); err != nil {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			return err
		}
	} else {
		_ = eng.ConfigureTLS("")
	}
	c.syn.ResetAuthBackoff()
	c.syn.Update(c.engineConfig())
	c.persist()
	c.syn.Run()
	c.Log("синхронизация запущена: " + eng.BaseURL(host, port, tls) + ", папка " + dir)
	return nil
}

// StartFromSaved starts synchronization with the saved settings.
func (c *Controller) StartFromSaved() error {
	cfg := c.Config()
	return c.Start(cfg.Host, cfg.Port, cfg.Dir, cfg.Interval, cfg.Token, cfg.TLS, cfg.TLSFingerprint)
}

// Stop stops synchronization.
func (c *Controller) Stop() {
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	c.syn.Stop()
	c.Log("синхронизация остановлена")
}

// Running reports whether synchronization is running.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Status returns the synchronization state from the engine.
func (c *Controller) Status() eng.Status { return c.syn.Status() }

// SyncNow starts an out-of-band synchronization cycle.
func (c *Controller) SyncNow() {
	c.Log("синхронизация вручную")
	c.syn.ResetAuthBackoff()
	go c.syn.SyncOnce(true)
}

// RecheckToken removes the pause after a 401 and immediately verifies the token.
func (c *Controller) RecheckToken() {
	c.syn.ResetAuthBackoff()
	c.Log("проверка токена")
	go c.syn.SyncOnce(true)
}

// TestConnection checks communication with the server and returns the result text.
func (c *Controller) TestConnection(host string, port int, token string, tls bool, fingerprint string) string {
	if host = strings.TrimSpace(host); host == "" {
		host = c.Config().Host
	}
	if port <= 0 {
		port = c.Config().Port
	}
	if token = strings.TrimSpace(token); token == "" {
		token = c.Config().Token
	}
	if fingerprint = strings.TrimSpace(fingerprint); fingerprint == "" && tls {
		fingerprint = c.Config().TLSFingerprint
	}
	if tls {
		if err := eng.ConfigureTLS(fingerprint); err != nil {
			return "TLS error: " + err.Error()
		}
	} else {
		_ = eng.ConfigureTLS("")
	}
	return eng.TestConnection(host, port, token, tls)
}

// Pair connects the client using a code created on the server (poscloud://connect?...).
// Both forms of the code are supported: with a one-time secret (exchanged for
// a new token via POST /pair) and with an existing device token (QR "Device
// token" — saved as is, without contacting the server).
func (c *Controller) Pair(rawCode, deviceName string) (eng.PairResult, error) {
	// form with an existing device token — no exchange required
	if tc, err := ParseTokenConnection(rawCode); err == nil {
		return c.applyDeviceToken(tc)
	} else if !errors.Is(err, errNoDeviceToken) {
		return eng.PairResult{}, err // code with a token, but broken — show the parsing error
	}
	info, err := eng.ParseConnectionCode(rawCode)
	if err != nil {
		return eng.PairResult{}, err
	}
	if info.TLS {
		if err := eng.ConfigureTLS(info.Fingerprint); err != nil {
			return eng.PairResult{}, fmt.Errorf("ошибка TLS: %w", err)
		}
	} else {
		_ = eng.ConfigureTLS("")
	}
	name := strings.TrimSpace(deviceName)
	if name == "" {
		name = c.DeviceName()
	}
	res, err := eng.Pair(eng.BaseURL(info.Host, info.Port, info.TLS), info.Code, name)
	if err != nil {
		return eng.PairResult{}, err
	}
	c.mu.Lock()
	c.cfg.Host = info.Host
	c.cfg.Port = info.Port
	c.cfg.TLS = info.TLS
	c.cfg.TLSFingerprint = info.Fingerprint
	c.cfg.Token = res.Token
	if name != "" {
		c.cfg.DeviceName = name
	}
	c.mu.Unlock()
	c.syn.ResetAuthBackoff()
	c.syn.Update(c.engineConfig())
	c.persist()
	c.Log("подключение по коду: сервер " + eng.BaseURL(info.Host, info.Port, info.TLS) + ", пользователь " + res.UserName + ", устройство " + res.DeviceName)
	return res, nil
}

// applyDeviceToken saves the connection with an existing device token
// (QR "Device token" on the server): address, TLS and token are taken from the code,
// the server is not queried. UserName is not sent in this form — it is returned
// empty, the interface shows the device line without the user.
func (c *Controller) applyDeviceToken(tc TokenConnection) (eng.PairResult, error) {
	if tc.TLS {
		if err := eng.ConfigureTLS(tc.Fingerprint); err != nil {
			return eng.PairResult{}, fmt.Errorf("ошибка TLS: %w", err)
		}
	} else {
		_ = eng.ConfigureTLS("")
	}
	name := strings.TrimSpace(tc.DeviceName)
	if name == "" {
		name = c.DeviceName()
	}
	c.mu.Lock()
	c.cfg.Host = tc.Host
	c.cfg.Port = tc.Port
	c.cfg.TLS = tc.TLS
	c.cfg.TLSFingerprint = tc.Fingerprint
	c.cfg.Token = tc.Token
	c.cfg.DeviceName = name
	c.mu.Unlock()
	c.syn.ResetAuthBackoff()
	c.syn.Update(c.engineConfig())
	c.persist()
	c.Log("подключение по токену устройства: сервер " + eng.BaseURL(tc.Host, tc.Port, tc.TLS) + ", устройство " + name)
	return eng.PairResult{Token: tc.Token, DeviceName: name}, nil
}

// ---------- selective synchronization ----------

// ServerFolders returns the list of top-level folders with statuses.
func (c *Controller) ServerFolders() ([]eng.ServerFolderNode, error) { return c.syn.ServerFolders() }

// SetFolderRule sets the folder mode (both/up/down/off).
func (c *Controller) SetFolderRule(path, mode string) error {
	err := c.syn.AddRule(path, mode)
	if err == nil {
		c.persist()
	}
	return err
}

// SetRulesFree enables/disables the "no rules" mode.
func (c *Controller) SetRulesFree(on bool) error {
	err := c.syn.SetRulesFree(on)
	if err == nil {
		c.persist()
	}
	return err
}

// RulesFreeEnabled reports whether the "no rules" mode is enabled.
func (c *Controller) RulesFreeEnabled() bool { return c.syn.RulesFreeEnabled() }

// Resync re-reads the full list of server files and synchronizes.
func (c *Controller) Resync() { go c.syn.Resync() }

// ---------- снимки экрана ----------

// screenshotsState — какие снимки экрана уже скопированы (имя -> размер)
// и сколько раз просили разрешение на чтение фото.
type screenshotsState struct {
	Copied   map[string]int64 `json:"copied"`
	PermAsks int              `json:"perm_asks"`
}

// ScreenshotDirs — где система хранит снимки экрана.
func ScreenshotDirs() []string {
	return []string{
		"/storage/emulated/0/Pictures/Screenshots",
		"/storage/emulated/0/DCIM/Screenshots",
	}
}

// ImportScreenshots копирует новые снимки экрана в подпапку «Снимки экрана»
// папки синхронизации и возвращает число скопированных файлов.
func (c *Controller) ImportScreenshots() int {
	cfg := c.Config()
	if strings.TrimSpace(cfg.Dir) == "" {
		return 0
	}
	// Снимки экрана принадлежит системе: без разрешения на чтение фото
	// MediaStore их просто не показывает (и ошибки не даёт).
	if !camera.PhotoPermissionGranted() {
		c.setShotErr(errors.New("нет разрешения на чтение фото — нажмите «Разрешить доступ к фото» на вкладке «Статус»"))
		return 0
	}
	dstDir := filepath.Join(cfg.Dir, "Снимки экрана")
	st := c.loadScreenshotState()
	changed, copied := false, 0

	// Основной путь — MediaStore: только он даёт доступ к чужим файлам
	// (скриншот принадлежит системному приложению).
	shots, listErr := camera.Screenshots()
	c.setShotErr(listErr)
	if listErr == nil {
		for _, sh := range shots {
			if st.Copied[sh.Name] == sh.Size {
				continue
			}
			src, err := camera.Open(sh.URI)
			if err != nil {
				c.Log("снимок экрана не прочитан: " + sh.Name + ": " + err.Error())
				continue
			}
			err = saveReader(src, filepath.Join(dstDir, sh.Name))
			_ = src.Close()
			if err != nil {
				c.Log("снимок экрана не сохранён: " + sh.Name + ": " + err.Error())
				continue
			}
			st.Copied[sh.Name] = sh.Size
			changed = true
			copied++
			c.Log("снимок экрана добавлен: " + sh.Name)
		}
	}

	if changed {
		c.saveScreenshotState(st)
	}
	return copied
}

// setShotErr запоминает текущую ошибку списка снимков и пишет её в журнал
// только при изменении, чтобы не засорять его каждые 30 секунд.
func (c *Controller) setShotErr(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	c.mu.Lock()
	prev := c.shotErr
	c.shotErr = msg
	c.mu.Unlock()
	if msg != "" && msg != prev {
		c.Log("снимки экрана: " + msg)
	}
}

// ---------- разрешение на чтение фото ----------

// PhotoPermissionGranted сообщает, может ли приложение видеть снимки экрана.
func (c *Controller) PhotoPermissionGranted() bool { return camera.PhotoPermissionGranted() }

// PhotoPermissionAsks — сколько раз уже просили разрешение. После второго
// отказа система диалог больше не показывает.
func (c *Controller) PhotoPermissionAsks() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.permAsks
}

// RequestPhotoAccess запрашивает разрешение на чтение фото (системный
// диалог). Если диалог уже отклоняли дважды, открывает настройки приложения —
// включить разрешение можно только там. Когда разрешение уже есть, сразу
// забирает накопившиеся снимки экрана.
func (c *Controller) RequestPhotoAccess() {
	if c.PhotoPermissionGranted() {
		go c.ImportScreenshots()
		return
	}
	st := c.loadScreenshotState()
	asks := c.PhotoPermissionAsks()
	if asks >= 2 {
		if err := camera.OpenAppSettings(); err != nil {
			c.Log("настройки приложения не открылись: " + err.Error())
		} else {
			c.Log("разрешение на фото отклонено навсегда — включите «Фото и видео» в настройках приложения")
		}
		return
	}
	asks++
	st.PermAsks = asks
	c.saveScreenshotState(st)
	c.mu.Lock()
	c.permAsks = asks
	c.mu.Unlock()
	if err := camera.RequestPhotoPermission(); err != nil {
		c.Log("запрос разрешения на фото не удался: " + err.Error())
		return
	}
	c.Log("запрошено разрешение на чтение фото — оно нужно для переноса снимков экрана")
}

// loadScreenshotState читает список уже скопированных снимков экрана.
func (c *Controller) loadScreenshotState() screenshotsState {
	st := screenshotsState{}
	data, err := os.ReadFile(filepath.Join(c.root, "screenshots.json"))
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.Copied == nil {
		st.Copied = map[string]int64{}
	}
	return st
}

// saveScreenshotState сохраняет список; старые записи отбрасываются.
func (c *Controller) saveScreenshotState(st screenshotsState) {
	if len(st.Copied) > 1000 {
		n := 0
		for name := range st.Copied {
			delete(st.Copied, name)
			n++
			if n >= 200 {
				break
			}
		}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(c.root, "screenshots.json"), b, 0644)
}

// copyFile копирует файл, создавая каталог назначения.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// saveReader записывает поток в файл, создавая каталог назначения.
func saveReader(r io.Reader, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ---------- log ----------

// Log adds a line to the log and the client.log file.
func (c *Controller) Log(s string) {
	line := time.Now().Format("15:04:05") + "  " + s
	c.mu.Lock()
	c.logs = append(c.logs, line)
	if len(c.logs) > 500 {
		c.logs = c.logs[len(c.logs)-500:]
	}
	c.logDirty = true
	c.mu.Unlock()
	c.appendToFile(line)
}

// appendToFile дописывает строку в client.log.
// Файл открывается на каждую запись и сразу закрывается: дескриптор не остаётся
// открытым — это важно для Android и для тестов с временным каталогом.
func (c *Controller) appendToFile(line string) {
	p := filepath.Join(c.root, "client.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(time.Now().Format("2006-01-02 ") + line + "\n")
	_ = f.Close()
}

// Logs returns a copy of the log.
func (c *Controller) Logs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.logs))
	copy(out, c.logs)
	return out
}

// LogDirtyAndReset returns true if the log changed, and resets the flag.
func (c *Controller) LogDirtyAndReset() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.logDirty
	c.logDirty = false
	return d
}

// ClearLogs clears the in-memory log.
func (c *Controller) ClearLogs() {
	c.mu.Lock()
	c.logs = nil
	c.mu.Unlock()
}

// ---------- transfer progress ----------

// Transfer — an active file transfer (for the interface).
type Transfer struct {
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Pct   int    `json:"pct"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

type transferState struct {
	name  string
	dir   string
	done  int64
	total int64
}

// transferStore receives engine events about file transfer progress.
type TransferStore struct {
	mu   sync.Mutex
	m    map[string]*transferState
	up   int64 // отправлено байт с момента запуска
	down int64 // получено байт с момента запуска
}

// Transfers — the global progress receiver; assigned to engine.Progress.
var Transfers = &TransferStore{m: map[string]*transferState{}}

func (s *TransferStore) Begin(name, dir string, total int64) {
	if name == "" || total <= 0 {
		return
	}
	s.mu.Lock()
	s.m[name] = &transferState{name: name, dir: dir, total: total}
	s.mu.Unlock()
}

func (s *TransferStore) Add(name string, n int64) {
	s.mu.Lock()
	if t := s.m[name]; t != nil {
		t.done += n
		if t.done > t.total {
			t.done = t.total
		}
		if n > 0 {
			if t.dir == "down" {
				s.down += n
			} else {
				s.up += n
			}
		}
	}
	s.mu.Unlock()
}

// Totals returns bytes sent and received since the app launch.
func (s *TransferStore) Totals() (up, down int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.up, s.down
}

func (s *TransferStore) End(name string) {
	s.mu.Lock()
	delete(s.m, name)
	s.mu.Unlock()
}

// Snapshot returns active transfers, sorted by name.
func (s *TransferStore) Snapshot() []Transfer {
	s.mu.Lock()
	out := make([]Transfer, 0, len(s.m))
	for _, t := range s.m {
		pct := 0
		if t.total > 0 {
			pct = int(t.done * 100 / t.total)
		}
		if pct > 100 {
			pct = 100
		}
		out = append(out, Transfer{Name: t.name, Dir: t.dir, Pct: pct, Done: t.done, Total: t.total})
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
