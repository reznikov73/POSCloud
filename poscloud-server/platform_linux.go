//go:build linux

package main

import (
	wailslinux "github.com/wailsapp/wails/v2/pkg/options/linux"
)

// linuxOptions возвращает настройки окна для Linux.
// Иконку задаём здесь: в Windows её берут из ресурсов exe
// (build/windows/icon.ico), а в Linux нужен PNG — своего ресурсного
// механизма у Wails на этой платформе нет.
func linuxOptions() *wailslinux.Options {
	return &wailslinux.Options{
		Icon: appIconPNG,
	}
}
