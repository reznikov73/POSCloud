//go:build linux

package main

import (
	"os/exec"
	"path/filepath"
)

// openPath открывает каталог или файл в системном файловом менеджере.
// xdg-open — часть freedesktop, есть в любом десктопном окружении.
func openPath(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("xdg-open", path).Start()
}

// revealPath открывает каталог, содержащий файл.
// Выделить сам файл средствами xdg-open нельзя — открываем его каталог.
func revealPath(path string) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		dir = path
	}
	openPath(dir)
}
