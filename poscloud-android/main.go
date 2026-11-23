// POSCloud Client для Android — точка входа: настройка приложения,
// автозапуск синхронизации и запуск интерфейса из internal/ui.
// Собирается в APK как чистое Go-приложение (gomobile), без Java-кода.
// Логика — в internal/client, синхронизацию выполняет общий движок
// poscloud-engine.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	eng "poscloud-engine"

	"poscloud-client-android/internal/client"
	"poscloud-client-android/internal/ui"
)

func main() {
	// приёмник событий о ходе передач (индикация в статусе)
	eng.Progress = client.Transfers

	a := app.NewWithID("io.github.reznikov73.poscloud.client")
	a.Settings().SetTheme(ui.Theme())
	w := a.NewWindow("POSCloud Client")

	ctrl := client.NewController(resolveRoot(a))
	st := ui.Build(ctrl, w)

	w.SetContent(st.Content)
	w.Resize(fyne.NewSize(430, 800))

	// автозапуск синхронизации при открытии приложения
	if ctrl.AutoRunEnabled() && ctrl.HasCredentials() {
		if err := ctrl.StartFromSaved(); err != nil {
			ctrl.Log("автозапуск пропущен: " + err.Error())
		}
	} else if ctrl.AutoRunEnabled() && !ctrl.HasCredentials() {
		ctrl.Log("автозапуск пропущен: не заданы токен и папка — заполните «Подключение» и нажмите «Запустить»")
	}

	stop := make(chan struct{})
	w.SetOnClosed(func() { close(stop) })
	go ui.Refresh(ctrl, st, stop)

	// При первом запуске просим доступ к фото: без него снимки экрана
	// (их создаёт система) не видны приложению и не попадают в папку.
	if ctrl.PhotoPermissionAsks() == 0 && !ctrl.PhotoPermissionGranted() {
		time.AfterFunc(1500*time.Millisecond, ctrl.RequestPhotoAccess)
	}

	w.ShowAndRun()
}

// resolveRoot подбирает каталог данных приложения: хранилище Fyne (на Android —
// приватный каталог приложения), иначе пользовательский конфиг-каталог.
func resolveRoot(a fyne.App) string {
	if a != nil && a.Storage() != nil {
		if uri := a.Storage().RootURI(); uri != nil && uri.Scheme() == "file" {
			p := strings.TrimPrefix(uri.String(), "file://")
			if p != "" && canWrite(p) {
				return p
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && canWrite(home) {
		return filepath.Join(home, ".config", "POSCloud")
	}
	if cd, err := os.UserConfigDir(); err == nil && canWrite(cd) {
		return filepath.Join(cd, "POSCloud")
	}
	return "."
}

// canWrite проверяет, что в каталог можно записать файл.
func canWrite(dir string) bool {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
