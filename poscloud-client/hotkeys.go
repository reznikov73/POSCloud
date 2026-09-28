//go:build windows

package main

import (
	goruntime "runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- глобальные горячие клавиши ----------
//
// В API Wails глобальных горячих клавиш нет, поэтому регистрируем их обычным
// способом: скрытое окно-приёмник сообщений и цикл сообщений в отдельном потоке.
// Комбинации: Ctrl+Shift+1 — область, Ctrl+Shift+2 — окно, Ctrl+Shift+3 — весь экран.

const (
	hkRegion = 1
	hkWindow = 2
	hkFull   = 3

	wmHotkey    = 0x0312
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modNoRepeat = 0x4000

	vk1 = 0x31 // «1»
	vk2 = 0x32 // «2»
	vk3 = 0x33 // «3»

	hwndMessage = ^uintptr(2) // HWND_MESSAGE = (HWND)-3
)

var (
	user32K              = windows.NewLazySystemDLL("user32.dll")
	kernel32K            = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassEx  = user32K.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32K.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32K.NewProc("DefWindowProcW")
	procRegisterHotKey   = user32K.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32K.NewProc("UnregisterHotKey")
	procGetMessageW      = user32K.NewProc("GetMessageW")
	procTranslateMessage = user32K.NewProc("TranslateMessage")
	procDispatchMessageW = user32K.NewProc("DispatchMessageW")
	procGetModuleHandleW = kernel32K.NewProc("GetModuleHandleW")
	procDestroyWindow    = user32K.NewProc("DestroyWindow")
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

type winPoint struct{ X, Y int32 }

type winMsg struct {
	Wnd      uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}

// hotkeySpec — одна комбинация: идентификатор, виртуальный код клавиши,
// название действия и цифра для описания.
type hotkeySpec struct {
	id    uintptr
	vk    uintptr
	name  string
	digit string
	run   func(*App)
}

func hotkeySpecs() []hotkeySpec {
	return []hotkeySpec{
		{hkRegion, vk1, "область", "1", func(a *App) { a.trayShotRegion() }},
		{hkWindow, vk2, "окно", "2", func(a *App) { a.trayShotWindow() }},
		{hkFull, vk3, "весь экран", "3", func(a *App) { a.trayShotFull() }},
	}
}

// Назначенные комбинации и их описание для интерфейса.
var (
	hotkeyMu      sync.Mutex
	hotkeySummary = "сочетания не назначены: их заняли другие программы"
)

// HotkeySummary возвращает текстом то, что удалось зарегистрировать.
func HotkeySummary() string {
	hotkeyMu.Lock()
	defer hotkeyMu.Unlock()
	return hotkeySummary
}

func setHotkeySummary(text string) {
	hotkeyMu.Lock()
	hotkeySummary = text
	hotkeyMu.Unlock()
}

// startHotkeys запускает обработчик горячих клавиш в отдельном потоке.
func (a *App) startHotkeys() {
	specs := hotkeySpecs()
	go a.hotkeyLoop(specs)
}

func (a *App) hotkeyLoop(specs []hotkeySpec) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		a.log("горячие клавиши недоступны: не удалось получить дескриптор модуля")
		return
	}
	className, err := windows.UTF16PtrFromString("POSCloudHotkeyWindow")
	if err != nil {
		return
	}

	wndProc := windows.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
		return r
	})

	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   wndProc,
		Instance:  hInst,
		ClassName: className,
	}
	if r, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		a.log("горячие клавиши недоступны: не удалось зарегистрировать класс окна")
		return
	}

	// Окно только для сообщений: его не видно и в списке окон его нет.
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0, hwndMessage, 0, hInst, 0)
	if hwnd == 0 {
		a.log("горячие клавиши недоступны: не удалось создать окно-приёмник")
		return
	}
	defer procDestroyWindow.Call(hwnd)

	// Основные сочетания — Ctrl+Shift+цифра. Если они заняты другими программами,
	// пробуем Ctrl+Alt+та же цифра: на практике Ctrl+Shift+1..3 часто перехвачены.
	type combo struct {
		mods uintptr
		pre  string
	}
	combos := []combo{
		{modControl | modShift | modNoRepeat, "Ctrl+Shift+"},
		{modControl | modAlt | modNoRepeat, "Ctrl+Alt+"},
	}

	parts := make([]string, 0, len(specs))
	for _, s := range specs {
		assigned := ""
		for _, c := range combos {
			if r, _, _ := procRegisterHotKey.Call(hwnd, s.id, c.mods, s.vk); r != 0 {
				assigned = c.pre + s.digit
				break
			}
		}
		if assigned == "" {
			parts = append(parts, s.name+" — не назначено")
			continue
		}
		parts = append(parts, assigned+" — "+s.name)
	}

	summary := strings.Join(parts, ", ")
	setHotkeySummary(summary)
	a.log("горячие клавиши: " + summary)

	var msg winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return // WM_QUIT или ошибка
		}
		if msg.Message == wmHotkey {
			for _, s := range specs {
				if uintptr(msg.WParam) == s.id {
					go s.run(a)
					break
				}
			}
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
