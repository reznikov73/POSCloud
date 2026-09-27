//go:build windows

package main

import (
	"fmt"
	"path/filepath"

	systray "github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Пункты шапки меню обновляются на ходу, поэтому держим ссылки на них.
var (
	trayMenuStatus  *systray.MenuItem
	trayMenuSummary *systray.MenuItem
	trayMenuToggle  *systray.MenuItem

	// счётчик файлов считается редко — обход каталога небыстрый
	trayMenuTicks     int
	trayMenuFileCount int
)

const trayAppTitle = "POSCloud · Сервер"

// buildTrayMenu собирает контекстное меню сервера (правый клик по иконке в трее).
func (a *App) buildTrayMenu() {
	header := systray.AddMenuItem(trayAppTitle, "Сервер обмена файлами")
	header.Disable()
	trayMenuStatus = systray.AddMenuItem("Статус: …", "Состояние сервера")
	trayMenuStatus.Disable()
	trayMenuSummary = systray.AddMenuItem("…", "Файлы и пользователи")
	trayMenuSummary.Disable()

	systray.AddSeparator()

	trayMenuToggle = systray.AddMenuItem("Остановить сервер", "Запустить или остановить сервер")
	trayMenuToggle.Click(func() { a.trayToggleServer() })

	mFolder := systray.AddMenuItem("Открыть папку хранения", "Каталог с файлами пользователей")
	mFolder.Click(func() { openInExplorer(a.storageRoot()) })

	mRelease := systray.AddMenuItem("Освободить порт", "Завершить чужой процесс, занявший порт")
	mRelease.Click(func() { a.trayReleasePort() })

	mAddr := systray.AddMenuItem("Скопировать адрес для клиентов", "http://<ваш-IP>:<порт>")
	mAddr.Click(func() { a.trayCopyAddress() })

	systray.AddSeparator()

	mWindow := systray.AddMenuItem("Открыть окно настроек", "Показать окно приложения")
	mWindow.Click(func() { a.trayShowWindow() })

	mLog := systray.AddMenuItem("Открыть журнал", "Показать файл журнала")
	mLog.Click(func() { a.OpenLogFile() })

	systray.AddSeparator()

	mMore := systray.AddMenuItem("Дополнительно", "Дополнительные действия")
	mCfg := mMore.AddSubMenuItem("Открыть файл настроек", "server.json рядом с программой")
	mCfg.Click(func() { openInExplorer(a.configPath()) })
	mLogFolder := mMore.AddSubMenuItem("Открыть папку с журналом", "Каталог с server.log")
	mLogFolder.Click(func() { openInExplorer(a.root()) })

	systray.AddSeparator()

	mAbout := systray.AddMenuItem("О программе", "Версия и назначение")
	mAbout.Click(func() {
		a.showAbout("О программе POSCloud",
			trayAppTitle+"\nВерсия "+appVersion+"\n\n"+
				"Сервер обмена файлами: хранит файлы и отдаёт их клиентам по сети.\n"+
				"Закрытие окна не завершает программу — она остаётся в трее.")
	})

	mQuit := systray.AddMenuItem("Выйти", "Закрыть программу")
	mQuit.Click(func() { a.trayQuit() })

	a.updateTrayMenu()
}

// serverStateLabel — текст состояния сервера для шапки меню.
func serverStateLabel(running, portFree bool, port int) string {
	switch {
	case running:
		return fmt.Sprintf("работает, порт %d", port)
	case !portFree:
		return "порт занят другим процессом"
	default:
		return "остановлен"
	}
}

// updateTrayMenu обновляет подписи шапки и кнопки пуска.
func (a *App) updateTrayMenu() {
	if trayMenuStatus == nil {
		return
	}
	a.mu.Lock()
	running, port, dir := a.srvRun, a.srvPort, a.srvDir
	users := 0
	if a.auth != nil {
		users = len(a.auth.list())
	}
	a.mu.Unlock()
	if port == 0 {
		port = 8090
	}

	trayMenuStatus.SetTitle("Статус: " + serverStateLabel(running, portFree(port), port))

	// Обход каталога делаем не каждую секунду, а раз в десять секунд.
	trayMenuTicks++
	if trayMenuTicks%10 == 1 {
		trayMenuFileCount = countFiles(dir)
	}
	trayMenuSummary.SetTitle(fmt.Sprintf("Файлов на сервере: %d · пользователей: %d", trayMenuFileCount, users))

	if running {
		trayMenuToggle.SetTitle("Остановить сервер")
	} else {
		trayMenuToggle.SetTitle("Запустить сервер")
	}
}

// trayToggleServer запускает или останавливает сервер из меню.
func (a *App) trayToggleServer() {
	a.mu.Lock()
	running := a.srvRun
	port, dir := a.srvPort, a.srvDir
	a.mu.Unlock()
	if running {
		a.StopServer()
	} else {
		a.StartServer(port, dir)
	}
	a.updateTrayMenu()
}

// trayReleasePort освобождает порт из меню: свой сервер — остановить,
// чужой процесс — спросить и завершить.
func (a *App) trayReleasePort() {
	a.mu.Lock()
	port, running := a.srvPort, a.srvRun
	a.mu.Unlock()
	if port == 0 {
		port = 8090
	}
	if running {
		a.StopServer()
		a.updateTrayMenu()
		return
	}

	holder := a.PortHolder(port)
	if holder.PID == 0 {
		a.showAbout("Освободить порт", fmt.Sprintf("Порт %d свободен.", port))
		return
	}
	if holder.IsSelf {
		a.showAbout("Освободить порт", "Порт слушает это же приложение — сначала остановите сервер.")
		return
	}
	if a.ctx == nil {
		return
	}

	msg := fmt.Sprintf("Порт %d занят процессом:\n%s (PID %d)", port, holder.Name, holder.PID)
	if holder.Path != "" {
		msg += "\n" + holder.Path
	}
	msg += "\n\nЗавершить этот процесс?"

	btn, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Освободить порт",
		Message:       msg,
		Buttons:       []string{"Завершить процесс", "Отмена"},
		DefaultButton: "Завершить процесс",
		CancelButton:  "Отмена",
	})
	if err != nil || btn != "Завершить процесс" {
		return
	}
	a.ReleasePort(port, holder.PID)
	a.updateTrayMenu()
}

// trayCopyAddress кладёт в буфер обмена адрес, который вводят в клиенте.
func (a *App) trayCopyAddress() {
	a.mu.Lock()
	port := a.srvPort
	a.mu.Unlock()
	if port == 0 {
		port = 8090
	}
	addr := fmt.Sprintf("http://%s:%d", firstIPOrLocalhost(), port)
	a.copyText(addr)
	a.showAbout("Адрес для клиентов", "Скопировано в буфер обмена:\n\n"+addr)
}

func (a *App) configPath() string { return filepath.Join(a.root(), "server.json") }
