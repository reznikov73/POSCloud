// Платформенный слой POSCloud Lite: минимальный набор вызовов Win32 API
// (окна, элементы управления, меню, таймеры) без сторонних библиотек.
// Собран под windows/386 и windows/amd64, работает начиная с Windows 7.
package main

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procSetWindowTextW       = user32.NewProc("SetWindowTextW")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procMoveWindow           = user32.NewProc("MoveWindow")
	procSetTimer             = user32.NewProc("SetTimer")
	procKillTimer            = user32.NewProc("KillTimer")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procFindWindowW          = user32.NewProc("FindWindowW")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procEnableWindow         = user32.NewProc("EnableWindow")
	procLoadCursorW          = user32.NewProc("LoadCursorW")
	procLoadIconW            = user32.NewProc("LoadIconW")
	procCreateMenu           = user32.NewProc("CreateMenu")
	procCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	procAppendMenuW          = user32.NewProc("AppendMenuW")
	procSetMenu              = user32.NewProc("SetMenu")
	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW         = kernel32.NewProc("CreateMutexW")
	procCreateFontW          = gdi32.NewProc("CreateFontW")
	procShellExecuteW        = shell32.NewProc("ShellExecuteW")
)

const (
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsVisible      = 0x10000000
	wsChild        = 0x40000000
	wsTabStop      = 0x00010000
	wsBorder       = 0x00800000
	wsVScroll      = 0x00200000
	wsClipSiblings = 0x04000000
	wsClipChildren = 0x02000000
	wsGroup        = 0x00020000
	wsExClientEdge = 0x00000200

	bsPushButton   = 0x00000000
	bsAutoCheckbox = 0x00000003
	bsLeft         = 0x00000100

	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esWantReturn  = 0x1000

	lbsNotify           = 0x0001
	lbsNoIntegralHeight = 0x0100

	cbsDropDownList = 0x0003
	cbsVScroll      = 0x00200000

	swHide       = 0
	swShow       = 5
	swRestore    = 9
	swShowNormal = 1

	wmCreate  = 0x0001
	wmDestroy = 0x0002
	wmSize    = 0x0005
	wmSetText = 0x000C
	wmClose   = 0x0010
	wmSetFont = 0x0030
	wmCommand = 0x0111
	wmTimer   = 0x0113
	wmApp     = 0x8000

	lbAddString    = 0x0180
	lbResetContent = 0x0184
	lbSetCurSel    = 0x0186
	lbGetCurSel    = 0x0188

	cbAddString    = 0x0143
	cbResetContent = 0x014B
	cbSetCurSel    = 0x014E
	cbGetCurSel    = 0x0147

	lbnSelChange = 1
	cbnSelChange = 1

	idcArrow       = 32512
	idiApplication = 32512

	mbOk              = 0x00000000
	mbOkCancel        = 0x00000001
	mbYesNo           = 0x00000004
	mbIconExclamation = 0x00000030
	mbIconInfo        = 0x00000040
	idYes             = 6

	errorAlreadyExists = 183

	colorWindow = 5
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type rect struct {
	left, top, right, bottom int32
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func getModuleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

// createWindow создаёт окно или элемент управления.
func createWindow(class, text string, style uint32, x, y, w, h int32, parent uintptr, id uintptr) uintptr {
	hc, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, id, getModuleHandle(), 0)
	return hc
}

func send(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func setText(hwnd uintptr, s string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(u16(s))))
}

func getText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, int(n)+2)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func messageBox(owner uintptr, text, caption string, flags uint32) int {
	r, _, _ := procMessageBoxW.Call(owner,
		uintptr(unsafe.Pointer(u16(text))),
		uintptr(unsafe.Pointer(u16(caption))), uintptr(flags))
	return int(r)
}

// createFont создаёт шрифт Segoe UI указанного размера (в пикселях по высоте).
func createFont(px int32) uintptr {
	h, _, _ := procCreateFontW.Call(
		uintptr(-px), 0, 0, 0, 400, 0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(u16("Segoe UI"))))
	return h
}

func loadCursorArrow() uintptr {
	h, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	return h
}

func loadAppIcon() uintptr {
	h, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return h
}

// shellOpen открывает файл программой по умолчанию (например, журнал в блокноте).
func shellOpen(path string) {
	procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(u16("open"))),
		uintptr(unsafe.Pointer(u16(path))), 0, 0, swShow)
}

// acquireSingleInstance создаёт именованный мьютекс; false — программа уже запущена.
func acquireSingleInstance(name string) bool {
	_, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16(name))))
	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		return false
	}
	return true
}

// showExistingInstance ищет окно другого экземпляра и показывает его.
func showExistingInstance(className string) bool {
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(u16(className))), 0)
	if h == 0 {
		return false
	}
	procShowWindow.Call(h, swRestore)
	procShowWindow.Call(h, swShow)
	procSetForegroundWindow.Call(h)
	return true
}

// ---------- меню ----------

func menuCreate() uintptr {
	h, _, _ := procCreateMenu.Call()
	return h
}

func menuCreatePopup() uintptr {
	h, _, _ := procCreatePopupMenu.Call()
	return h
}

// menuAppend добавляет пункт (id=0 — подменю-попап).
func menuAppend(hmenu uintptr, id uintptr, text string) {
	procAppendMenuW.Call(hmenu, 0 /*MF_STRING*/, id, uintptr(unsafe.Pointer(u16(text))))
}

func menuAppendPopup(hmenu, hsub uintptr, text string) {
	procAppendMenuW.Call(hmenu, 0x00000010 /*MF_POPUP*/, hsub, uintptr(unsafe.Pointer(u16(text))))
}

func menuSet(hwnd, hmenu uintptr) {
	procSetMenu.Call(hwnd, hmenu)
}

// ---------- цикл сообщений ----------

func runMessageLoop() int {
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return int(m.wParam)
}
