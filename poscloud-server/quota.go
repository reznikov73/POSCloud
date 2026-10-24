package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// usageBytes считает занятое место пользователя: данные + история/корзина.
func usageBytes(userDir string) int64 {
	var total int64
	count := func(root string) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
			return nil
		})
	}
	count(userDir)
	count(historyRoot(userDir))
	return total
}

func (a *App) quotaBytes() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.quotaGB <= 0 {
		return 0
	}
	return a.quotaGB * 1024 * 1024 * 1024
}

// checkQuota проверяет, поместится ли ещё add байт (0 — без лимита).
func (a *App) checkQuota(userDir string, add int64) error {
	q := a.quotaBytes()
	if q <= 0 || add <= 0 {
		return nil
	}
	used := usageBytes(userDir)
	if used+add > q {
		return fmt.Errorf("превышена квота: занято %d из %d байт", used, q)
	}
	return nil
}

// checkQuotaWithTemp учитывает, что временный файл уже лежит в каталоге.
func (a *App) checkQuotaWithTemp(userDir, tmpPath string, newSize int64) error {
	q := a.quotaBytes()
	if q <= 0 || newSize <= 0 {
		return nil
	}
	used := usageBytes(userDir)
	if fi, err := os.Stat(tmpPath); err == nil {
		used -= fi.Size()
	}
	if used+newSize > q {
		return fmt.Errorf("превышена квота: занято %d из %d байт", used, q)
	}
	return nil
}
