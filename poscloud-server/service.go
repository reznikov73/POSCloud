package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// ---------- режим службы (без окна и трея) ----------
//
// Обычный запуск создаёт окно Wails и иконку в трее — для этого нужна
// графическая сессия. Служба работает иначе: только HTTP-сервер и веб-админка.
//
// Зачем: systemd поднимает программу при загрузке ОС, до чьего-либо входа,
// и перезапускает её при падении. Окно и трей при этом не нужны — управление
// идёт через браузер (см. api_http.go, api_admin.go).
//
// Методы, которым нужен Wails-контекст (диалог выбора папки, буфер обмена),
// в этом режиме не вызываются: у a.ctx остаётся nil, а HTTP-обёртка
// подставляет заглушки (callGUIMethod).

// runService запускает сервер без графической части и ждёт сигнала остановки.
func runService(f *serverFlags) {
	app := NewApp()
	app.flagPort = f.port
	app.flagData = f.data
	app.autoStart = f.autostart
	// Флаг передан явно — значит он важнее сохранённой настройки.
	app.autoStartSet = true
	setAPIApp(app)

	// Та же инициализация, что в окне: startup только запоминает ctx,
	// а он нужен исключительно GUI-методам, которые в службе не вызываются.
	// Контекст берём фоновый, чтобы не было nil-разыменований в общем коде.
	app.startup(context.Background())

	// Останавливаемся по SIGTERM (systemctl stop) и SIGINT (Ctrl+C).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	sig := <-stop
	app.log(fmt.Sprintf("служба остановлена (%s)", sig.String()))

	// Закрываем соединения и сохраняем состояние.
	app.shutdown(context.Background())
}
