package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// FileActionResult — результат сохранения файла для интерфейса.
type FileActionResult struct {
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// ExportConnectionCode сохраняет код подключения в файл .poscloud-connect.
// В файле только сам код (одноразовый секрет); постоянный токен туда не попадает.
func (a *App) ExportConnectionCode(code string) FileActionResult {
	code = strings.TrimSpace(code)
	if code == "" {
		return FileActionResult{Error: "код подключения пуст"}
	}
	defaultDir := ""
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		defaultDir = filepath.Join(home, "Documents")
		if _, err := os.Stat(defaultDir); err != nil {
			defaultDir = home
		}
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Сохранить код подключения",
		DefaultFilename:  "poscloud-connect.poscloud-connect",
		DefaultDirectory: defaultDir,
		Filters: []runtime.FileFilter{
			{DisplayName: "Код подключения POSCloud (*.poscloud-connect)", Pattern: "*.poscloud-connect"},
			{DisplayName: "Текстовые файлы (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return FileActionResult{Error: "не удалось открыть диалог сохранения: " + err.Error()}
	}
	if strings.TrimSpace(path) == "" {
		return FileActionResult{Error: "сохранение отменено"}
	}
	if !strings.HasSuffix(strings.ToLower(path), ".poscloud-connect") {
		path += ".poscloud-connect"
	}
	if err := os.WriteFile(filepath.Clean(path), []byte(code+"\n"), 0600); err != nil {
		return FileActionResult{Error: "не удалось записать файл: " + err.Error()}
	}
	a.log("код подключения сохранён в файл")
	return FileActionResult{Path: path}
}
