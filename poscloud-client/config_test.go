package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestApp создаёт App с подменённым каталогом настроек.
// cfgJSON == "" — файла client.json нет.
func newTestApp(t *testing.T, cfgJSON string) *App {
	t.Helper()
	dir := t.TempDir()
	if cfgJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "client.json"), []byte(cfgJSON), 0644); err != nil {
			t.Fatalf("не удалось записать client.json: %v", err)
		}
	}
	a := NewApp()
	a.dataRoot = dir
	a.rootOnce.Do(func() {}) // фиксируем каталог настроек, чтобы root() не искал его рядом с exe
	return a
}

// Значения по умолчанию не должны выглядеть как «явно переданный аргумент»,
// иначе сохранённые настройки будут затираться при каждом запуске.
func TestFlagDefaultsDoNotOverrideSavedConfig(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindClientFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("разбор без аргументов: %v", err)
	}
	if f.host != "" {
		t.Errorf("-server по умолчанию = %q, ожидалось пустое значение", f.host)
	}
	if f.port != 0 {
		t.Errorf("-port по умолчанию = %d, ожидалось 0", f.port)
	}
	if f.dir != "" {
		t.Errorf("-dir по умолчанию = %q, ожидалось пустое значение", f.dir)
	}
	if f.interval != 0 {
		t.Errorf("-interval по умолчанию = %d, ожидалось 0", f.interval)
	}
	if f.token != "" {
		t.Errorf("-token по умолчанию = %q, ожидалось пустое значение", f.token)
	}
}

// Синхронизация должна запускаться сама при открытии программы:
// без аргументов автостарт включён.
func TestAutoStartIsOnByDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindClientFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("разбор без аргументов: %v", err)
	}
	if !f.autostart {
		t.Error("autostart по умолчанию выключен, ожидалось включённое значение")
	}
}

// Явный -autostart=false должен отключать автостарт (аварийный тормоз).
func TestAutoStartCanBeDisabled(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindClientFlags(fs)
	if err := fs.Parse([]string{"-autostart=false"}); err != nil {
		t.Fatalf("разбор аргумента: %v", err)
	}
	if f.autostart {
		t.Error("autostart остался включённым при явном -autostart=false")
	}
}

// Адрес сервера из client.json должен пережить перезапуск без аргументов.
func TestSavedHostSurvivesRestart(t *testing.T) {
	a := newTestApp(t, `{"host":"192.168.1.50","port":8099,"dir":"C:\\Sync","interval":7,"token":"pcs_abc"}`)
	a.loadConfig()
	a.applyFlagOverrides() // как при запуске без аргументов командной строки

	if a.host != "192.168.1.50" {
		t.Errorf("host = %q, ожидался 192.168.1.50", a.host)
	}
	if a.port != 8099 {
		t.Errorf("port = %d, ожидался 8099", a.port)
	}
	if a.localDir != `C:\Sync` {
		t.Errorf("localDir = %q, ожидался C:\\Sync", a.localDir)
	}
	if a.interval != 7 {
		t.Errorf("interval = %d, ожидался 7", a.interval)
	}
	if a.token != "pcs_abc" {
		t.Errorf("token = %q, ожидался pcs_abc", a.token)
	}
}

// Явно переданный аргумент должен побеждать сохранённую настройку.
func TestFlagOverridesSavedHost(t *testing.T) {
	a := newTestApp(t, `{"host":"192.168.1.50","port":8099}`)
	a.loadConfig()
	a.flagHost = "10.0.0.5"
	a.flagPort = 9000
	a.applyFlagOverrides()

	if a.host != "10.0.0.5" {
		t.Errorf("host = %q, ожидался 10.0.0.5", a.host)
	}
	if a.port != 9000 {
		t.Errorf("port = %d, ожидался 9000", a.port)
	}
}

// Без файла настроек и без аргументов остаются встроенные значения по умолчанию.
func TestBuiltinDefaultsWithoutConfig(t *testing.T) {
	a := newTestApp(t, "")
	a.loadConfig()
	a.applyFlagOverrides()

	if a.host != "localhost" {
		t.Errorf("host = %q, ожидался localhost", a.host)
	}
	if a.port != 8090 {
		t.Errorf("port = %d, ожидался 8090", a.port)
	}
	if a.interval != 3 {
		t.Errorf("interval = %d, ожидался 3", a.interval)
	}
}

// Без токена доступа автостарт не должен запускать синхронизацию: сервер
// отвечает 401, и вместо работы получился бы цикл ошибок. Причина — в журнале.
func TestStartOnLaunchSkipsWithoutToken(t *testing.T) {
	a := newTestApp(t, "")
	a.localDir = t.TempDir()
	a.token = ""
	a.startOnLaunch()
	if a.syncRun {
		t.Fatal("синхронизация запустилась без токена доступа")
	}
	if !logsContain(a, "автозапуск пропущен") {
		t.Errorf("в журнале нет причины пропуска: %v", a.GetLogs())
	}
}

// Без папки синхронизации автостарт тоже не стартует и объясняет причину.
func TestStartOnLaunchSkipsWithoutDir(t *testing.T) {
	a := newTestApp(t, "")
	a.localDir = ""
	a.token = "tok"
	a.startOnLaunch()
	if a.syncRun {
		t.Fatal("синхронизация запустилась без папки синхронизации")
	}
	if !logsContain(a, "автозапуск пропущен") {
		t.Errorf("в журнале нет причины пропуска: %v", a.GetLogs())
	}
}

func logsContain(a *App, sub string) bool {
	for _, l := range a.GetLogs() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
