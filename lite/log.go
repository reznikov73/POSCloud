package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu   sync.Mutex
	logFile string
)

// initLog определяет файл журнала рядом с программой.
func initLog() {
	logFile = filepath.Join(exeDir(), "lite.log")
}

// logf пишет строку в журнал (время + текст). Журнал ведётся всегда — это
// единственный способ заглянуть внутрь фоновой программы на старой машине.
func logf(format string, args ...any) {
	line := time.Now().Format("2006-01-02 15:04:05") + "  " + fmt.Sprintf(format, args...)
	logMu.Lock()
	defer logMu.Unlock()
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}
