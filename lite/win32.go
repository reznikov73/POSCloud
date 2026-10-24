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
	ole32    = syscall.NewLazyDLL("ole32.dll")

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
	procShellNotifyIconW     = shell32.NewProc("Shell_NotifyIconW")
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	procOpenClipboard        = user32.NewProc("OpenClipboard")
	procCloseClipboard       = user32.NewProc("CloseClipboard")
	procGetClipboardData     = user32.NewProc("GetClipboardData")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procGlobalSize           = kernel32.NewProc("GlobalSize")
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

	cfUnicodeText = 13

	wmNotify = 0x004E
	wmTray   = 0x8000 + 10

	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080

	bifReturnOnlyFSDirs = 0x0001
	bifNewDialogStyle   = 0x0040

	coinitApartmentThreaded = 0x2

	// ListView
	lvsReport        = 0x0001
	lvsSingleSel     = 0x0004
	lvsShowSelAlways = 0x0008
	lvsNoSortHeader  = 0x8000

	lvmFirst                    = 0x1000
	lvmDeleteAllItems           = lvmFirst + 9
	lvmGetNextItem              = lvmFirst + 12
	lvmEnsureVisible            = lvmFirst + 19
	lvmSetItemState             = lvmFirst + 43
	lvmSetExtendedListViewStyle = lvmFirst + 54
	lvmInsertItemW              = lvmFirst + 77
	lvmInsertColumnW            = lvmFirst + 97
	lvmSetItemTextW             = lvmFirst + 116

	lvniSelected = 0x0002
	lvisSelected = 0x0002
	lvisFocused  = 0x0001

	lvsExGridLines     = 0x00000001
	lvsExFullRowSelect = 0x00000020
	lvsExDoubleBuffer  = 0x00010000

	lvcfFmt     = 0x0001
	lvcfWidth   = 0x0002
	lvcfText    = 0x0004
	lvcfSubItem = 0x0008
	lvcfmtLeft  = 0x0000
	lvifText    = 0x0001

	lvnItemChanged = 0xFFFFFF9B
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

type point struct{ x, y int32 }

// notifyIconData — NOTIFYICONDATAW (версия 2: иконка, подсказка, сообщения).
type notifyIconData struct {
	cbSize            uint32
	hWnd              uintptr
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             uintptr
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
}

type lvColumn struct {
	mask       uint32
	fmt        int32
	cx         int32
	pszText    *uint16
	cchTextMax int32
	iSubItem   int32
	iImage     int32
	iOrder     int32
}

type lvItem struct {
	mask       uint32
	iItem      int32
	iSubItem   int32
	state      uint32
	stateMask  uint32
	pszText    *uint16
	cchTextMax int32
	iImage     int32
	lParam     uintptr
	iIndent    int32
}

type nmhdr struct {
	hwndFrom uintptr
	idFrom   uintptr
	code     uint32
}

type browseInfo struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
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

// clipboardText читает Unicode-текст из буфера обмена. Пустая строка означает,
// что буфер пуст или занят другим приложением.
func clipboardText() string {
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	size, _, _ := procGlobalSize.Call(h)
	if size < 2 {
		return ""
	}
	n := int(size / 2)
	if n > 1<<20 {
		n = 1 << 20
	}
	buf := unsafe.Slice((*uint16)(unsafe.Pointer(p)), n)
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
	return createFontWeight(px, 400)
}

// createFontBold — полужирный вариант (для заголовков).
func createFontBold(px int32) uintptr {
	return createFontWeight(px, 700)
}

func createFontWeight(px, weight int32) uintptr {
	h, _, _ := procCreateFontW.Call(
		uintptr(-px), 0, 0, 0, uintptr(weight), 0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(u16("Segoe UI"))))
	return h
}

func loadCursorArrow() uintptr {
	h, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	return h
}

// loadAppIcon берёт иконку, встроенную в программу (rsrc кладёт её под id 2);
// запасной вариант — системная иконка.
func loadAppIcon() uintptr {
	h, _, _ := procLoadIconW.Call(getModuleHandle(), 2)
	if h != 0 {
		return h
	}
	logf("иконка-ресурс не найдена, используется системная")
	h, _, _ = procLoadIconW.Call(0, uintptr(idiApplication))
	return h
}

func getCursorPos() (uintptr, uintptr) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return uintptr(p.x), uintptr(p.y)
}

// trackPopup показывает контекстное меню и возвращает выбранный id (0 — отмена).
func trackPopup(hmenu, owner uintptr) uintptr {
	x, y := getCursorPos()
	// по правилам Shell_NotifyIcon: окно должно быть на переднем плане, иначе меню не закроется
	procSetForegroundWindow.Call(owner)
	r, _, _ := procTrackPopupMenu.Call(hmenu,
		tpmReturnCmd|tpmRightButton|tpmNoNotify,
		x, y, 0, owner, 0)
	return r
}

// browseFolder показывает системный диалог выбора папки; возвращает путь или "".
func browseFolder(owner uintptr, title string) string {
	disp := make([]uint16, 260)
	bi := browseInfo{
		hwndOwner:      owner,
		pszDisplayName: &disp[0],
		lpszTitle:      u16(title),
		ulFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	path := make([]uint16, 260)
	ok, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}

// trayAdd добавляет значок программы в системный трей.
func trayAdd(hwnd, icon uintptr, tip string) {
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = 1
	nid.uFlags = nifIcon | nifMessage | nifTip
	nid.uCallbackMessage = wmTray
	nid.hIcon = icon
	copy(nid.szTip[:], syscall.StringToUTF16(tip))
	r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		logf("трей: не удалось добавить значок")
	} else {
		logf("трей: значок добавлен")
	}
}

// trayDelete убирает значок из трея.
func trayDelete(hwnd uintptr) {
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = 1
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
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
