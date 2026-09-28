//go:build windows

package main

import (
	"fmt"
	"path/filepath"

	systray "github.com/energye/systray"
)

// Пункты шапки меню обновляются на ходу, поэтому держим ссылки на них.
var (
	trayMenuStatus  *systray.MenuItem
	trayMenuSummary *systray.MenuItem
	trayMenuToggle  *systray.MenuItem
)

const trayAppTitle = "POSCloud · Клиент"

// buildTrayMenu собирает контекстное меню клиента (правый клик по иконке в трее).
func (a *App) buildTrayMenu() {
	header := systray.AddMenuItem(trayAppTitle, "Клиент синхронизации папок")
	header.Disable()
	trayMenuStatus = systray.AddMenuItem("Статус: …", "Состояние синхронизации")
	trayMenuStatus.Disable()
	trayMenuSummary = systray.AddMenuItem("…", "Файлы и время последнего обмена")
	trayMenuSummary.Disable()

	systray.AddSeparator()

	trayMenuToggle = systray.AddMenuItem("Приостановить синхронизацию", "Запустить или остановить синхронизацию")
	trayMenuToggle.Click(func() { a.trayToggleSync() })

	mFolder := systray.AddMenuItem("Открыть папку", "Открыть папку синхронизации")
	mFolder.Click(func() { openInExplorer(a.currentDir()) })

	systray.AddSeparator()

	mSync := systray.AddMenuItem("Синхронизировать сейчас", "Обменяться файлами немедленно")
	mSync.Click(func() { go a.SyncNow() })

	mWindow := systray.AddMenuItem("Открыть окно настроек", "Показать окно приложения")
	mWindow.Click(func() { a.trayShowWindow() })

	mLog := systray.AddMenuItem("Открыть журнал", "Показать файл журнала")
	mLog.Click(func() { a.OpenLogFile() })

	systray.AddSeparator()

	mShotFull := systray.AddMenuItem("Снимок всего экрана", "Сохранить снимок всего экрана")
	mShotFull.Click(func() { go a.trayShotFull() })

	mShotWindow := systray.AddMenuItem("Снимок активного окна", "Снять окно, которое сейчас в фокусе")
	mShotWindow.Click(func() { go a.trayShotWindow() })

	mShotRegion := systray.AddMenuItem("Снимок области", "Выделить область мышью и сохранить")
	mShotRegion.Click(func() { go a.trayShotRegion() })

	mShotFolder := systray.AddMenuItem("Открыть папку со снимками", "Каталог со снимками экрана")
	mShotFolder.Click(func() { a.OpenShotsFolder() })

	systray.AddSeparator()

	mMore := systray.AddMenuItem("Дополнительно", "Дополнительные действия")
	mCfg := mMore.AddSubMenuItem("Открыть файл настроек", "client.json рядом с программой")
	mCfg.Click(func() { openInExplorer(a.configPath()) })
	mLogFolder := mMore.AddSubMenuItem("Открыть папку с журналом", "Каталог с client.log")
	mLogFolder.Click(func() { openInExplorer(a.root()) })

	systray.AddSeparator()

	mAbout := systray.AddMenuItem("О программе", "Версия и назначение")
	mAbout.Click(func() {
		a.showAbout("О программе POSCloud",
			trayAppTitle+"\nВерсия "+appVersion+"\n\n"+
				"Клиент синхронизации папок между устройствами.\n"+
				"Закрытие окна не завершает программу — она остаётся в трее.")
	})

	mQuit := systray.AddMenuItem("Выйти", "Закрыть программу")
	mQuit.Click(func() { a.trayQuit() })

	a.updateTrayMenu()
}

// syncStateLabel — текст состояния синхронизации для шапки меню.
func syncStateLabel(running, online bool, errMsg string) string {
	switch {
	case !running:
		return "остановлено"
	case errMsg != "":
		return "нет связи с сервером"
	case online:
		return "синхронизировано"
	default:
		return "синхронизация…"
	}
}

// updateTrayMenu обновляет подписи шапки и кнопки паузы.
func (a *App) updateTrayMenu() {
	if trayMenuStatus == nil {
		return
	}
	a.mu.Lock()
	running, online := a.syncRun, a.online
	count, last, errMsg := a.count, a.lastSync, a.errMsg
	a.mu.Unlock()

	trayMenuStatus.SetTitle("Статус: " + syncStateLabel(running, online, errMsg))

	summary := fmt.Sprintf("Локальных файлов: %d", count)
	if last != "" {
		summary += " · последний обмен " + last
	}
	trayMenuSummary.SetTitle(summary)

	if running {
		trayMenuToggle.SetTitle("Приостановить синхронизацию")
	} else {
		trayMenuToggle.SetTitle("Продолжить синхронизацию")
	}
}

// trayToggleSync запускает или останавливает синхронизацию из меню.
func (a *App) trayToggleSync() {
	a.mu.Lock()
	running := a.syncRun
	host, port, dir, itv, tok := a.host, a.port, a.localDir, a.interval, a.token
	a.mu.Unlock()

	switch {
	case running:
		a.StopSync()
	case tok == "" || dir == "":
		a.log("запуск из трея невозможен: не заданы токен доступа или папка синхронизации")
		a.trayShowWindow()
	default:
		a.StartSync(host, port, dir, itv, tok)
	}
	a.updateTrayMenu()
}

func (a *App) currentDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.localDir
}

// trayShotFull делает снимок всего экрана из трея.
func (a *App) trayShotFull() {
	if _, err := a.CaptureFullScreen(); err != nil {
		a.log("не удалось сделать снимок экрана: " + err.Error())
	}
}

// trayShotRegion начинает выбор области из трея: снимок и показ окна.
func (a *App) trayShotRegion() {
	if _, err := a.StartRegionCapture(); err != nil {
		a.log("не удалось начать снимок области: " + err.Error())
	}
}

// trayShotWindow делает снимок активного окна.
func (a *App) trayShotWindow() {
	if _, err := a.CaptureActiveWindow(); err != nil {
		a.log("не удалось сделать снимок активного окна: " + err.Error())
	}
}

func (a *App) configPath() string { return filepath.Join(a.root(), "client.json") }
