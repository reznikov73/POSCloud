package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------- резервная копия из браузера ----------
//
// CreateBackup в окне открывает диалог сохранения файла (runtime.SaveFileDialog).
// В браузере такого диалога нет, и вызов зависал: Wails-метод ждал окно,
// которого не существует. Здесь копия пишется в каталог по умолчанию,
// а готовый файл отдаётся на скачивание.

// backupsDir возвращает каталог, куда складываются копии из веб-режима.
// Он вне каталога данных: в копии лежат токены и TLS-ключ, и класть их
// в синхронизируемую папку нельзя.
func (a *App) backupsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "poscloud-backups")
	}
	return filepath.Join(home, "poscloud-backups")
}

// handleBackupCreate создаёт копию и возвращает путь к файлу.
func (a *App) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	if !a.serveBackup(w, r) {
		return
	}
	dir := a.backupsDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "не удалось создать каталог для копий"})
		return
	}
	name := "POSCloud-backup-" + time.Now().Format("20060102-150405") + ".zip"
	path := filepath.Join(dir, name)
	if err := a.writeBackup(path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	a.log("создана резервная копия настроек и доступов")
	writeJSON(w, http.StatusOK, map[string]any{"result": BackupResult{Path: path}})
}

// handleBackupDownload отдаёт готовый файл копии.
// Имя проверяется: только файлы из каталога копий, без переходов по путям.
func (a *App) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if !a.serveBackup(w, r) {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "недопустимое имя файла"})
		return
	}
	path := filepath.Join(a.backupsDir(), name)
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "файл не найден"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}

// serveBackup пропускает запрос, если у клиента есть сессия админки.
// Возвращает false, если ответ уже отправлен.
func (a *App) serveBackup(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := a.adminSession(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "требуется вход в админку"})
		return false
	}
	return true
}
