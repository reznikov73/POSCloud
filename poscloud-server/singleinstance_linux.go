//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------- защита от двойного запуска ----------
//
// Первый экземпляр программы берёт эксклюзивную блокировку flock на файле-замке.
// Второй экземпляр блокировку получить не может и завершается, не создавая
// ни трея, ни окна, ни HTTP-сервера.
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

// activateExistingWindow в Linux ничего не делает: оконный менеджер сам
// поднимает окно уже запущенного приложения при повторном запуске, а надёжного
// способа сделать это из чужого процесса без привязки к конкретному WM нет.
func activateExistingWindow(title string) {
	_ = title
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
