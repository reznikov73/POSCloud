//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------- защита от двойного запуска ----------
//
// Первый экземпляр программы берёт эксклюзивную блокировку flock на файле-замке.
// Второй экземпляр блокировку получить не может, поэтому просит первый показать
// окно и завершается, не создавая ни трея, ни окна, ни HTTP-сервера.
//
// flock снимается ядром автоматически при завершении процесса, поэтому «зависший»
// после падения экземпляр не блокирует следующий запуск.
//
// Файл-замок лежит рядом с настройками пользователя, а не в /tmp: предсказуемый
// путь в общем каталоге позволил бы другому пользователю подменить файл.

// singleInstanceFile держит дескриптор замка до конца работы процесса.
// Закрывать его вручную не нужно: ядро сделает это при выходе.
var singleInstanceFile *os.File

// singleInstancePath возвращает путь к файлу-замку.
func singleInstancePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "poscloud-server.lock"), nil
}

// showSocketPath возвращает путь к сокету, через который второй экземпляр
// просит первый показать окно.
func showSocketPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "poscloud-server.sock"), nil
}

// acquireSingleInstance возвращает true, если это первый (единственный) экземпляр.
// Любая ошибка подготовки замка не блокирует запуск: лучше два экземпляра,
// чем ни одного.
func acquireSingleInstance(name string) bool {
	// name остаётся в сигнатуре ради общей с Windows реализации.
	_ = name

	p, err := singleInstancePath()
	if err != nil {
		return true
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return true
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return true
	}
	// LOCK_EX|LOCK_NB — эксклюзивно и без ожидания: если замок занят, сразу отказ.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return false
	}
	singleInstanceFile = f
	return true
}

// startShowRequestListener принимает просьбы «покажи окно» от повторных запусков.
//
// Зачем это нужно: закрытие окна не завершает программу, а прячет его в трей.
// Без такого приёма повторный запуск из файлового менеджера выглядел бы как
// «программа не запускается» — второй экземпляр молча завершался, а спрятанное
// окно первого так и оставалось невидимым.
func startShowRequestListener(a *App) {
	p, err := showSocketPath()
	if err != nil {
		return
	}
	// Сокет от прошлого запуска мог остаться после падения — убираем.
	_ = os.Remove(p)
	ln, err := net.Listen("unix", p)
	if err != nil {
		return
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Запрос короткий: читаем с таймаутом, чтобы чужое соединение
			// не подвесило приёмник.
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 32)
			n, _ := conn.Read(buf)
			_ = conn.Close()
			if strings.TrimSpace(string(buf[:n])) == "show" {
				a.showWindowFromOtherInstance()
			}
		}
	}()
}

// showWindowFromOtherInstance показывает и поднимает окно уже запущенной программы.
func (a *App) showWindowFromOtherInstance() {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// stopShowRequestListener убирает сокет при выходе: иначе следующий запуск
// попытается подключиться к мёртвому сокету.
func stopShowRequestListener() {
	if p, err := showSocketPath(); err == nil {
		_ = os.Remove(p)
	}
}

// activateExistingWindow просит уже запущенный экземпляр показать окно.
// Если первый экземпляр не отвечает (например, ещё не поднял сокет),
// просто завершаемся молча — окно всё равно появится при следующем запуске.
func activateExistingWindow(title string) {
	_ = title
	p, err := showSocketPath()
	if err != nil {
		return
	}
	conn, err := net.DialTimeout("unix", p, time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = conn.Write([]byte("show"))
}

// WindowHidden сообщает интерфейсу, что окно скрыто (свёрнуто в трей):
// страница в этом состоянии не обновляется, чтобы не тратить процессор впустую.
// В Windows окно прячется в трей и становится невидимым; в Linux окно
// сворачивается, поэтому проверяем именно свёрнутое состояние.
func (a *App) WindowHidden() bool {
	if a.ctx == nil {
		return false
	}
	return runtime.WindowIsMinimised(a.ctx)
}
