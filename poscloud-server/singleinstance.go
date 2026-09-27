//go:build windows

package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- защита от двойного запуска ----------
//
// Первый экземпляр программы создаёт именованный мьютекс. Если такой мьютекс уже
// существует, значит программа уже запущена: второй экземпляр показывает окно
// первого и завершается, не создавая ни трея, ни окна, ни HTTP-сервера.
//
// Мьютекс — объект ядра: ОС освобождает его автоматически при завершении процесса,
// поэтому «зависший» после падения экземпляр не блокирует следующий запуск.

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// SW_RESTORE — восстановить окно (свёрнутое или скрытое) и активировать его.
const swRestore = 9

// singleInstanceMutex держит дескриптор мьютекса до конца работы процесса.
// Закрывать его вручную не нужно: ядро сделает это при выходе.
var singleInstanceMutex windows.Handle

// acquireSingleInstance возвращает true, если это первый (единственный) экземпляр.
func acquireSingleInstance(name string) bool {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return true // не смогли подготовить имя — не мешаем запуску
	}
	h, err := windows.CreateMutex(nil, false, n)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS && h != 0 {
			// Мьютекс уже создан другим экземпляром — свой дескриптор закрываем.
			_ = windows.CloseHandle(h)
			return false
		}
		return true // любая другая ошибка — не блокируем запуск программы
	}
	singleInstanceMutex = h
	return true
}

// activateExistingWindow находит окно уже запущенной программы по заголовку и
// выводит его на передний план. Окно может быть скрыто (закрытие не завершает
// программу, а прячет окно), поэтому FindWindowW ищет и невидимые окна.
func activateExistingWindow(title string) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	// Первый экземпляр мог ещё не успеть создать окно — пробуем несколько раз.
	for i := 0; i < 20; i++ {
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
		if hwnd != 0 {
			procShowWindow.Call(hwnd, swRestore)
			procSetForegroundWindow.Call(hwnd)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
