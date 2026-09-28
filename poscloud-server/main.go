package main

import (
	"context"
	"embed"
	"flag"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// Заголовок окна. Задан константой, потому что используется и при создании окна,
// и при поиске окна уже запущенного экземпляра (см. singleinstance.go).
const appWindowTitle = "POSCloud Server — сервер"

// oneInstanceKey — имя объекта ядра, по которому распознаётся уже запущенная копия.
// Префикс Local\ ограничивает область текущим сеансом пользователя.
const oneInstanceKey = `Local\POSCloud-Server-SingleInstance`

// Акцентный цвет стрелок иконки в трее: сервер — сочный красный.
const (
	trayAccentR = 0xef
	trayAccentG = 0x44
	trayAccentB = 0x44
)

// Версия приложения — показывается в пункте «О программе».
const appVersion = "1.0.0"

// serverFlags — параметры запуска сервера из командной строки.
type serverFlags struct {
	port      int
	data      string
	autostart bool
}

// bindServerFlags объявляет флаги запуска. Пустая строка и 0 означают
// «взять из сохранённых настроек»: значения по умолчанию не должны затирать
// server.json. Вынесено в отдельную функцию, чтобы это можно было проверить тестом.
func bindServerFlags(fs *flag.FlagSet) *serverFlags {
	f := &serverFlags{}
	fs.IntVar(&f.port, "port", 0, "порт сервера (0 = взять из сохранённых настроек, по умолчанию 8090)")
	fs.StringVar(&f.data, "data", "", "каталог хранения файлов")
	fs.BoolVar(&f.autostart, "autostart", true, "запускать сервер сразу при старте программы (-autostart=false — не запускать)")
	return f
}

func main() {
	f := bindServerFlags(flag.CommandLine)
	flag.Parse()

	// Отличаем «аргумент не передавали» от «передали явно»: явный важнее настройки.
	autoStartSet := false
	flag.Visit(func(fl *flag.Flag) {
		if fl.Name == "autostart" {
			autoStartSet = true
		}
	})

	// Защита от двойного запуска: если программа уже работает, показываем окно
	// уже запущенного экземпляра и завершаемся.
	if !acquireSingleInstance(oneInstanceKey) {
		activateExistingWindow(appWindowTitle)
		return
	}

	app := NewApp()
	app.flagPort = f.port
	app.flagData = f.data
	app.autoStart = f.autostart
	app.autoStartSet = autoStartSet

	buildTrayIcons()
	trayStart, trayEnd := systray.RunWithExternalLoop(func() { app.trayInit("POSCloud Server") }, nil)

	err := wails.Run(&options.App{
		Title:            appWindowTitle,
		Width:            940,
		Height:           720,
		MinWidth:         820,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 15, G: 18, B: 22, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			trayStart()
			app.startup(ctx)
		},
		OnShutdown: func(ctx context.Context) {
			app.shutdown(ctx)
			trayEnd()
		},
		OnBeforeClose: func(ctx context.Context) bool {
			if trayQuitRequested() {
				return false
			}
			runtime.WindowHide(ctx)
			return true
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
