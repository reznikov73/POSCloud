//go:build windows

package main

import "os/exec"

// openPath открывает каталог или файл в системном файловом менеджере.
func openPath(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("explorer", path).Start()
}

// revealPath открывает каталог с выделением указанного файла.
func revealPath(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("explorer", "/select,"+path).Start()
}
