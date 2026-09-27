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
const appWindowTitle = "POSCloud Client — клиент"

// oneInstanceKey — имя объекта ядра, по которому распознаётся уже запущенная копия.
// Префикс Local\ ограничивает область текущим сеансом пользователя.
const oneInstanceKey = `Local\POSCloud-Client-SingleInstance`

// clientFlags — параметры запуска клиента из командной строки.
type clientFlags struct {
	host      string
	port      int
	dir       string
	interval  int
	token     string
	autostart bool
}

// bindClientFlags объявляет флаги запуска. Пустая строка и 0 означают
// «взять из сохранённых настроек»: значения по умолчанию не должны затирать
// client.json. Вынесено в отдельную функцию, чтобы это можно было проверить тестом.
func bindClientFlags(fs *flag.FlagSet) *clientFlags {
	f := &clientFlags{}
	fs.StringVar(&f.host, "server", "", "адрес сервера (пусто = взять из сохранённых настроек)")
	fs.IntVar(&f.port, "port", 0, "порт сервера (0 = взять из сохранённых настроек)")
	fs.StringVar(&f.dir, "dir", "", "папка синхронизации")
	fs.IntVar(&f.interval, "interval", 0, "интервал опроса, сек (0 = из сохранённых настроек)")
	fs.StringVar(&f.token, "token", "", "токен доступа к серверу (получить на сервере)")
	fs.BoolVar(&f.autostart, "autostart", true, "запускать синхронизацию сразу при старте программы (-autostart=false — не запускать)")
	return f
}

func main() {
	f := bindClientFlags(flag.CommandLine)
	flag.Parse()

	// Защита от двойного запуска: если программа уже работает, показываем окно
	// уже запущенного экземпляра и завершаемся.
	if !acquireSingleInstance(oneInstanceKey) {
		activateExistingWindow(appWindowTitle)
		return
	}

	app := NewApp()
	app.flagHost = f.host
	app.flagPort = f.port
	app.flagDir = f.dir
	app.flagInterval = f.interval
	app.flagToken = f.token
	app.autoStart = f.autostart

	buildTrayIcons()
	trayStart, trayEnd := systray.RunWithExternalLoop(func() { app.trayInit("POSCloud Client") }, nil)

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
