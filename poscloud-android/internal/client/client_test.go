package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigRoundTrip checks saving and loading client.json settings.
func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewController(dir)
	auto := true
	c.mu.Lock()
	c.cfg.Host = "192.168.1.50"
	c.cfg.Port = 9001
	c.cfg.Dir = filepath.Join(dir, "sync")
	c.cfg.Interval = 7
	c.cfg.Token = "secret-token"
	c.cfg.TLS = true
	c.cfg.TLSFingerprint = "AA:BB"
	c.cfg.DeviceName = "My phone"
	c.cfg.AutoRun = &auto
	c.mu.Unlock()
	c.persist()

	// A new controller reads the same settings
	c2 := NewController(dir)
	cfg := c2.Config()
	if cfg.Host != "192.168.1.50" || cfg.Port != 9001 || cfg.Interval != 7 || cfg.Token != "secret-token" {
		t.Fatalf("settings not restored: %+v", cfg)
	}
	if !cfg.TLS || cfg.TLSFingerprint != "AA:BB" {
		t.Fatalf("TLS settings not restored: %+v", cfg)
	}
	if cfg.DeviceName != "My phone" {
		t.Fatalf("device name not restored: %q", cfg.DeviceName)
	}
	if !c2.AutoRunEnabled() {
		t.Fatal("autoRun must be enabled")
	}
	if !c2.HasCredentials() {
		t.Fatal("token and folder must be present")
	}
	if c2.DeviceName() != "My phone" {
		t.Fatalf("DeviceName: %q", c2.DeviceName())
	}
}

// TestDeviceNameFallback checks the default device name.
func TestDeviceNameFallback(t *testing.T) {
	c := NewController(t.TempDir())
	if c.DeviceName() == "" {
		t.Fatal("device name must not be empty")
	}
}

// TestForgetMissing — снимок, удалённый из папки (вручную или синхронизацией
// с компьютера), больше не считается новым: иначе он возвращался бы с телефона.
func TestForgetMissing(t *testing.T) {
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "keep.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := screenshotsState{
		Copied: map[string]int64{"keep.jpg": 1, "gone2.jpg": 2, "gone1.jpg": 3},
		Gone:   map[string]int64{},
	}
	got := forgetMissing(dst, &st)
	if len(got) != 2 || got[0] != "gone1.jpg" || got[1] != "gone2.jpg" {
		t.Fatalf("forgetMissing = %v, ожидалось [gone1.jpg gone2.jpg]", got)
	}
	if _, ok := st.Copied["keep.jpg"]; !ok || len(st.Copied) != 1 {
		t.Fatalf("в copied должен остаться только keep.jpg: %v", st.Copied)
	}
	if st.Gone["gone1.jpg"] != 3 || st.Gone["gone2.jpg"] != 2 {
		t.Fatalf("удалённые снимки не запомнены: %v", st.Gone)
	}
	if again := forgetMissing(dst, &st); len(again) != 0 {
		t.Fatalf("повторный вызов ничего не должен находить: %v", again)
	}
}

// TestWritableSyncDir — недоступную для записи папку заменяем папкой
// приложения: иначе на Android 11+ с внешней папкой синхронизация «работает»,
// а файлы не идут.
func TestWritableSyncDir(t *testing.T) {
	root := t.TempDir()
	c := NewController(root)
	blocker := filepath.Join(root, "file.txt")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// родитель — файл: каталог не создать, запись невозможна
	got, err := c.writableSyncDir(filepath.Join(blocker, "sync"))
	if err != nil {
		t.Fatalf("writableSyncDir: %v", err)
	}
	if got != c.DefaultSyncDir() {
		t.Fatalf("папка не заменена: %q, ожидалась %q", got, c.DefaultSyncDir())
	}
	// доступная папка остаётся как есть
	if got, err := c.writableSyncDir(root); err != nil || got != root {
		t.Fatalf("доступная папка изменена: %q, %v", got, err)
	}
}

// TestInstallID — номер установки создаётся один раз и переживает перезапуск:
// по нему различаются журналы телефона и эмулятора с одинаковым именем.
func TestInstallID(t *testing.T) {
	root := t.TempDir()
	c := NewController(root)
	id := c.InstallID()
	if id == "" {
		t.Fatal("номер установки не создан")
	}
	c2 := NewController(root)
	if c2.InstallID() != id {
		t.Fatalf("номер не сохранился: %q и %q", id, c2.InstallID())
	}
	if len(NewController(t.TempDir()).InstallID()) != 8 {
		t.Fatal("ожидается 8 hex-символов")
	}
}

// TestLogMirrorPath — имя зеркала журнала безопасно для файловой системы и
// различается у устройств с одинаковым именем.
func TestLogMirrorPath(t *testing.T) {
	root := t.TempDir()
	c := NewController(root)
	c.mu.Lock()
	c.cfg.Dir = filepath.Join(root, "sync")
	c.cfg.DeviceName = "Мой телефон"
	c.mu.Unlock()
	p := c.LogMirrorPath()
	if filepath.Dir(p) != filepath.Join(root, "sync") {
		t.Fatalf("зеркало должно лежать в корне папки синхронизации: %q", p)
	}
	if !strings.HasPrefix(filepath.Base(p), "POSCloud-журнал-") {
		t.Fatalf("неожиданное имя зеркала: %q", filepath.Base(p))
	}
	if strings.ContainsAny(filepath.Base(p), " \\/:") {
		t.Fatalf("небезопасное имя файла: %q", filepath.Base(p))
	}
	c2 := NewController(t.TempDir())
	c2.mu.Lock()
	c2.cfg.Dir = c.Config().Dir
	c2.mu.Unlock()
	if filepath.Base(c.LogMirrorPath()) == filepath.Base(c2.LogMirrorPath()) {
		t.Fatal("одинаковые имена зеркал у разных установок")
	}
}

// TestSaveLogCopy — копия журнала кладётся в папку синхронизации только по
// вызову (кнопка на вкладке «Журнал») и содержит строки журнала.
func TestSaveLogCopy(t *testing.T) {
	root := t.TempDir()
	c := NewController(root)
	dir := filepath.Join(root, "sync")
	c.mu.Lock()
	c.cfg.Dir = dir
	c.mu.Unlock()
	c.Log("первая строка")
	p, err := c.SaveLogCopy()
	if err != nil {
		t.Fatalf("SaveLogCopy: %v", err)
	}
	if p != c.LogMirrorPath() {
		t.Fatalf("путь копии: %q, ожидался %q", p, c.LogMirrorPath())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("копия не создана: %v", err)
	}
	if !strings.Contains(string(b), "первая строка") {
		t.Fatalf("в копии нет строки журнала: %q", b)
	}
	// без вызова копия не появляется
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	c.Log("вторая строка")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("копия появилась без вызова SaveLogCopy")
	}
	if _, err := c.SaveLogCopy(); err != nil {
		t.Fatalf("SaveLogCopy: %v", err)
	}
	b, err = os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "вторая строка") {
		t.Fatalf("копия не обновилась: %q, %v", b, err)
	}
}

// TestVersionHelpers — разбор версии из имени файла и сравнение версий.
func TestVersionHelpers(t *testing.T) {
	cases := map[string]string{
		"POSCloud-Client-1.0.34-android.apk":             "1.0.34",
		"POSCloud-Client-1.0.34-android-arm64-debug.apk": "1.0.34",
		"проверка-обмена-2.apk":                          "",
		"file.apk":                                       "",
		"POSCloud-Client-10.20.30-android.apk":           "10.20.30",
	}
	for name, want := range cases {
		if got := versionFromName(name); got != want {
			t.Errorf("versionFromName(%q) = %q, ожидалось %q", name, got, want)
		}
	}
	if compareVersions("1.0.34", "1.0.34") != 0 || compareVersions("1.0.35", "1.0.34") != 1 || compareVersions("1.0.9", "1.0.10") != -1 {
		t.Fatal("compareVersions работает неверно")
	}
}

// TestFindApkUpdate — из папки берётся самый новый APK, версия сравнивается с текущей.
func TestFindApkUpdate(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "POSCloud")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		"POSCloud-Client-1.0.34-android.apk",
		"POSCloud-Client-1.0.36-android.apk",
		"POSCloud-Client-1.0.35-android-arm64-debug.apk",
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// в подпапке — только старый APK
	if err := os.WriteFile(filepath.Join(sub, "POSCloud-Client-1.0.30-android.apk"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, p := findApkUpdate(dir, "1.0.34")
	if v != "1.0.36" || !strings.HasSuffix(p, "POSCloud-Client-1.0.36-android.apk") {
		t.Fatalf("найдено %q (%q), ожидалась 1.0.36", v, p)
	}
	if v, _ := findApkUpdate(dir, "1.0.36"); v != "" {
		t.Fatalf("обновление найдено при равной версии: %q", v)
	}
	if v, _ := findApkUpdate("", "1.0.34"); v != "" {
		t.Fatalf("пустая папка дала обновление: %q", v)
	}
}

// TestTransfers checks the transfer progress receiver.
func TestTransfers(t *testing.T) {
	s := &TransferStore{m: map[string]*transferState{}}
	s.Begin("a.txt", "up", 1000)
	s.Begin("b.txt", "down", 500)
	s.Add("a.txt", 250)
	s.Add("b.txt", 500) // more than the full size — clamped from above
	s.Add("missing.txt", 10)
	list := s.Snapshot()
	if len(list) != 2 {
		t.Fatalf("expected 2 transfers, got %d", len(list))
	}
	byName := map[string]Transfer{}
	for _, tr := range list {
		byName[tr.Name] = tr
	}
	if byName["a.txt"].Pct != 25 {
		t.Fatalf("a.txt: percent %d, expected 25", byName["a.txt"].Pct)
	}
	if byName["b.txt"].Pct != 100 {
		t.Fatalf("b.txt: percent %d, expected 100 (clamped from above)", byName["b.txt"].Pct)
	}
	s.End("a.txt")
	if len(s.Snapshot()) != 1 {
		t.Fatal("after End(a.txt) one transfer must remain")
	}
}

// TestLogFile checks that the log is written to a file and accumulated in memory.
func TestLogFile(t *testing.T) {
	dir := t.TempDir()
	c := NewController(dir)
	c.Log("test line")
	data, err := os.ReadFile(filepath.Join(dir, "client.log"))
	if err != nil {
		t.Fatalf("client.log not created: %v", err)
	}
	if !strings.Contains(string(data), "test line") {
		t.Fatalf("log not written: %q", string(data))
	}
	lines := c.Logs()
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "test line") {
		t.Fatalf("in-memory log is empty: %v", lines)
	}
	if !c.LogDirtyAndReset() {
		t.Fatal("the log must be flagged as changed")
	}
	if c.LogDirtyAndReset() {
		t.Fatal("after the reset the flag must not be set again")
	}
	c.ClearLogs()
	if len(c.Logs()) != 0 {
		t.Fatal("the log must be cleared")
	}
}
