//go:build !linux

package main

import (
	wailslinux "github.com/wailsapp/wails/v2/pkg/options/linux"
)

// linuxOptions на не-Linux платформах возвращает nil: настройки окна там
// задаются иначе (в Windows иконка берётся из ресурсов exe).
func linuxOptions() *wailslinux.Options {
	return nil
}
