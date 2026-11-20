//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// desktopFileName — файл автозапуска в ~/.config/autostart (стандарт freedesktop).
const desktopFileName = "poscloud-server.desktop"

// autostartLabel — подпись галочки автозапуска в интерфейсе.
const autostartLabel = "Запускать программу при входе в систему"

// autostartPath возвращает путь к .desktop-файлу автозапуска.
func autostartPath() (string, error) {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "autostart", desktopFileName), nil
}

// AutostartEnabled сообщает, включён ли автозапуск при входе в систему.
// Запись с Hidden=true считается выключенной — так пользователь может
// отключить автозапуск штатными средствами рабочего стола.
func (a *App) AutostartEnabled() bool {
	p, err := autostartPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Hidden=") {
			return !strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "Hidden=")), "true")
		}
	}
	return true
}

// SetAutostart включает/выключает автозапуск при входе в систему
// через ~/.config/autostart/*.desktop (freedesktop autostart spec).
func (a *App) SetAutostart(enabled bool) error {
	p, err := autostartPath()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return writeAutostartFile(p, exe)
}

// writeAutostartFile создаёт каталог автозапуска и записывает .desktop-файл.
func writeAutostartFile(path, exe string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(desktopContent(exe)), 0644)
}

// migrateAutostartEntry обновляет .desktop-файл, созданный прежней версией:
// в нём нет -hidden, поэтому после входа в систему открывалось окно.
// Файл, указывающий на другой exe, не трогаем.
func (a *App) migrateAutostartEntry() {
	p, err := autostartPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if !autostartNeedsUpdate(desktopExec(string(data)), exe) {
		return
	}
	if err := writeAutostartFile(p, exe); err == nil {
		a.log("автозапуск обновлён: программа будет стартовать в трее без окна")
	}
}
