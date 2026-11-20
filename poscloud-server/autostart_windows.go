//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

// runKey — ветка реестра, откуда Windows читает программы автозапуска.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// autostartLabel — подпись галочки автозапуска в интерфейсе.
const autostartLabel = "Запускать программу при входе в Windows"

// AutostartEnabled сообщает, включён ли автозапуск при входе в Windows.
func (a *App) AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("POSCloud Server")
	return err == nil
}

// SetAutostart включает/выключает автозапуск при входе в Windows (реестр HKCU\...\Run).
func (a *App) SetAutostart(enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return k.SetStringValue("POSCloud Server", autostartCommand(exe))
	}
	_ = k.DeleteValue("POSCloud Server")
	return nil
}

// migrateAutostartEntry обновляет запись автозапуска, созданную прежней версией:
// в ней нет -hidden, поэтому после входа в Windows открывалось окно.
// Запись, указывающую на другой exe, не трогаем.
func (a *App) migrateAutostartEntry() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	value, _, err := k.GetStringValue("POSCloud Server")
	if err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if !autostartNeedsUpdate(value, exe) {
		return
	}
	if err := k.SetStringValue("POSCloud Server", autostartCommand(exe)); err == nil {
		a.log("автозапуск обновлён: программа будет стартовать в трее без окна")
	}
}
