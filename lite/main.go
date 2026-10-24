// POSCloud Lite — лёгкая версия клиента для старых Windows (7+): нативный
// интерфейс Win32, без WebView2. Синхронизацию выполняет общий движок
// poscloud-engine. Настройки читаются из client.json (совместим с обычным
// клиентом) или poscloud-lite.json рядом с программой.
package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	engine "poscloud-engine"
)

const (
	mainClassName     = "PosCloudLiteWnd"
	settingsClassName = "PosCloudLiteSettingsWnd"
	appTitle          = "POSCloud Lite"
	appMutexName      = "POSCloudLiteMutex"
)

const (
	idList      = 1001
	idMode      = 1002
	idRefresh   = 1003
	idFree      = 1004
	idDelete    = 1005
	idRule      = 1006
	idRulesFree = 1007
	idLog       = 1008
	idSettings  = 1009

	idSHost   = 3006
	idSPort   = 3007
	idSDir    = 3008
	idSToken  = 3009
	idSInt    = 3010
	idSBrowse = 3011
	idSCheck  = 3002
	idSTest   = 3003
	idSSave   = 3004
	idSCancel = 3005

	idMenuSettings = 2001
	idMenuReload   = 2002
	idMenuLog      = 2003
	idMenuExit     = 2009

	idTrayOpen   = 4001
	idTrayToggle = 4002
	idTrayLog    = 4003
	idTrayExit   = 4004

	timerStatus = 1
	wmFolders   = wmApp + 1
)

var (
	gCfg liteConfig
	gSyn *engine.Syncer

	hwndMain  uintptr
	hTitle    uintptr
	hStatus   uintptr
	hList     uintptr
	hMode     uintptr
	hFree     uintptr
	hDelete   uintptr
	hRule     uintptr
	hFreeChk  uintptr
	gFont     uintptr
	gFontBold uintptr

	hwndSettings uintptr
	hEdHost      uintptr
	hEdPort      uintptr
	hEdDir       uintptr
	hEdToken     uintptr
	hEdInt       uintptr
	hChkAuto     uintptr

	foldersMu  sync.Mutex
	folders    []engine.ServerFolderNode
	refreshing bool
)

var folderModes = []struct{ id, title string }{
	{"both", "В обе стороны"},
	{"up", "Только отправлять"},
	{"down", "Только получать"},
	{"off", "Не синхронизировать"},
}

// ---------- вспомогательные ----------

func fmtSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f ГБ", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f МБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f КБ", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d Б", n)
}

func modeLabel(m string) string {
	for _, fm := range folderModes {
		if fm.id == m {
			return fm.title
		}
	}
	return m
}

func statusText(n engine.ServerFolderNode) string {
	switch {
	case n.OnlyRule:
		return "Правило без папки"
	case n.New:
		return "Новая — выберите режим"
	case n.Mode == "off":
		if n.Local {
			return "Есть на этом ПК (пауза)"
		}
		return "Не синхронизируется"
	case n.Local:
		return "Есть на этом ПК"
	case n.Mode == "both" || n.Mode == "down":
		return "Будет скачана"
	}
	return "Нет на этом ПК"
}

func setBtnEnabled(h uintptr, on bool) {
	var v uintptr
	if on {
		v = 1
	}
	procEnableWindow.Call(h, v)
}

func boolPtr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func ruleFreeEnabled() bool {
	if gSyn == nil {
		return false
	}
	return gSyn.RulesFreeEnabled()
}

func synConfigured() bool {
	return strings.TrimSpace(gCfg.Token) != "" && strings.TrimSpace(gCfg.Dir) != ""
}

// ---------- список папок ----------

func requestFolders() {
	foldersMu.Lock()
	if refreshing {
		foldersMu.Unlock()
		return
	}
	refreshing = true
	foldersMu.Unlock()
	go func() {
		list, err := gSyn.ServerFolders()
		foldersMu.Lock()
		if err == nil {
			folders = list
		}
		refreshing = false
		foldersMu.Unlock()
		if err != nil {
			logf("список папок: %v", err)
		}
		procPostMessageW.Call(hwndMain, wmFolders, 0, 0)
	}()
}

func selectedIndexLocked() int {
	r := send(hList, lvmGetNextItem, ^uintptr(0), lvniSelected)
	if int32(r) < 0 {
		return -1
	}
	i := int(r)
	if i < 0 || i >= len(folders) {
		return -1
	}
	return i
}

func selectedIndex() int {
	foldersMu.Lock()
	defer foldersMu.Unlock()
	return selectedIndexLocked()
}

func selectedNode() (engine.ServerFolderNode, bool) {
	foldersMu.Lock()
	defer foldersMu.Unlock()
	i := selectedIndexLocked()
	if i < 0 {
		return engine.ServerFolderNode{}, false
	}
	return folders[i], true
}

func selectRow(i int) {
	si := lvItem{state: lvisSelected | lvisFocused, stateMask: lvisSelected | lvisFocused}
	send(hList, lvmSetItemState, uintptr(i), uintptr(unsafe.Pointer(&si)))
	send(hList, lvmEnsureVisible, uintptr(i), 1)
}

func updateModeControls() {
	foldersMu.Lock()
	i := selectedIndexLocked()
	var n engine.ServerFolderNode
	if i >= 0 {
		n = folders[i]
	}
	foldersMu.Unlock()
	if i < 0 {
		send(hMode, cbSetCurSel, ^uintptr(0), 0) // CB_ERR — снять выбор
		setBtnEnabled(hFree, false)
		setBtnEnabled(hDelete, false)
		setBtnEnabled(hRule, false)
		return
	}
	idx := 0
	cur := n.Rule
	if cur == "" {
		cur = n.Mode
	}
	for j, fm := range folderModes {
		if fm.id == cur {
			idx = j
		}
	}
	send(hMode, cbSetCurSel, uintptr(idx), 0)
	setBtnEnabled(hFree, n.Local && n.Mode == "off" && !n.OnlyRule)
	setBtnEnabled(hDelete, !n.OnlyRule)
	setBtnEnabled(hRule, n.OnlyRule)
}

func fillFolderList() {
	foldersMu.Lock()
	list := append([]engine.ServerFolderNode(nil), folders...)
	foldersMu.Unlock()

	selPath := ""
	if i := selectedIndex(); i >= 0 {
		selPath = list[i].Path
	}
	send(hList, lvmDeleteAllItems, 0, 0)
	newSel := -1
	for i, n := range list {
		meta := "папка"
		if n.Files > 0 {
			meta = fmt.Sprintf("%d ф. · %s", n.Files, fmtSize(n.Bytes))
		}
		it := lvItem{mask: lvifText, iItem: int32(i), iSubItem: 0, pszText: u16(n.Path)}
		send(hList, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&it)))
		setSub := func(sub int32, text string) {
			si := lvItem{iSubItem: sub, pszText: u16(text)}
			send(hList, lvmSetItemTextW, uintptr(i), uintptr(unsafe.Pointer(&si)))
		}
		setSub(1, meta)
		setSub(2, statusText(n))
		setSub(3, modeLabel(n.Mode))
		if selPath != "" && n.Path == selPath {
			newSel = i
		}
	}
	if newSel >= 0 {
		selectRow(newSel)
	}
	updateModeControls()
}

// ---------- действия ----------

func confirmBox(text string) bool {
	return messageBox(hwndMain, text, appTitle, mbYesNo|mbIconExclamation) == idYes
}

func doRefresh() {
	gSyn.Resync()
	requestFolders()
}

func doFreeSpace() {
	n, ok := selectedNode()
	if !ok {
		return
	}
	if !confirmBox(fmt.Sprintf("Удалить локальные файлы папки «%s» с этого компьютера?\n\nДанные на сервере останутся.", n.Path)) {
		return
	}
	go func() {
		if _, err := gSyn.ClearLocalCopy(n.Path); err != nil {
			logf("освободить место: %v", err)
		}
		requestFolders()
	}()
}

func doDeleteFolder() {
	n, ok := selectedNode()
	if !ok {
		return
	}
	if !confirmBox(fmt.Sprintf("Удалить папку «%s» с сервера?\n\nНа сервере: %d ф. (%s).\nФайлы исчезнут и с других устройств, локальная копия на этом компьютере тоже будет удалена.", n.Path, n.Files, fmtSize(n.Bytes))) {
		return
	}
	go func() {
		if _, err := gSyn.DeleteServerFolder(n.Path); err != nil {
			logf("удалить с сервера: %v", err)
		}
		requestFolders()
	}()
}

func doRemoveRule() {
	n, ok := selectedNode()
	if !ok {
		return
	}
	go func() {
		if err := gSyn.RemoveRule(n.Path); err != nil {
			logf("убрать правило: %v", err)
		}
		requestFolders()
	}()
}

func doModeChange() {
	n, ok := selectedNode()
	if !ok {
		return
	}
	sel := int(send(hMode, cbGetCurSel, 0, 0))
	if sel < 0 || sel >= len(folderModes) {
		return
	}
	val := folderModes[sel].id
	wasOff := n.Rule == "" || n.Mode == "off"
	const big = 50 * 1024 * 1024
	if wasOff && (val == "both" || val == "down") && !n.Local && n.Bytes >= big {
		if !confirmBox(fmt.Sprintf("Скачать папку «%s»?\n\nФайлов: %d (%s).", n.Path, n.Files, fmtSize(n.Bytes))) {
			updateModeControls()
			return
		}
	}
	if wasOff && (val == "both" || val == "up") && n.Local && n.LocBytes >= big {
		if !confirmBox(fmt.Sprintf("Отправить папку «%s»?\n\nФайлов: %d (%s).", n.Path, n.LocFiles, fmtSize(n.LocBytes))) {
			updateModeControls()
			return
		}
	}
	go func() {
		if err := gSyn.AddRule(n.Path, val); err != nil {
			logf("режим папки: %v", err)
		}
		requestFolders()
	}()
}

// ---------- главное окно ----------

var mainWndProcCb = syscall.NewCallback(mainWndProc)

func showMainWindow() {
	procShowWindow.Call(hwndMain, swRestore)
	procShowWindow.Call(hwndMain, swShow)
	procSetForegroundWindow.Call(hwndMain)
}

func showTrayMenu() {
	m := menuCreatePopup()
	toggle := "Приостановить синхронизацию"
	if !gSyn.Running() {
		toggle = "Продолжить синхронизацию"
	}
	menuAppend(m, idTrayOpen, "Открыть окно")
	menuAppend(m, idTrayToggle, toggle)
	menuAppend(m, idTrayLog, "Открыть журнал")
	menuAppend(m, idTrayExit, "Выход")
	switch trackPopup(m, hwndMain) {
	case idTrayOpen:
		showMainWindow()
	case idTrayToggle:
		if gSyn.Running() {
			gSyn.Stop()
			logf("синхронизация приостановлена (из трея)")
		} else if synConfigured() {
			gSyn.Run()
			logf("синхронизация возобновлена (из трея)")
		}
		updateStatusLine()
	case idTrayLog:
		shellOpen(logFile)
	case idTrayExit:
		procDestroyWindow.Call(hwndMain)
	}
}

func mainWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		hwndMain = hwnd
		gFont = createFont(16)
		gFontBold = createFontBold(18)
		hTitle = createWindow("STATIC", "Синхронизация папок", wsChild|wsVisible|wsClipSiblings, 12, 8, 500, 26, hwnd, 0)
		hStatus = createWindow("STATIC", "запуск…", wsChild|wsVisible|wsClipSiblings, 12, 38, 756, 20, hwnd, 0)
		hList = createWindow("SysListView32", "", wsChild|wsVisible|wsTabStop|wsBorder|wsClipSiblings|uint32(lvsReport|lvsSingleSel|lvsShowSelAlways|lvsNoSortHeader), 12, 62, 756, 258, hwnd, idList)
		ext := uintptr(lvsExGridLines | lvsExFullRowSelect | lvsExDoubleBuffer)
		send(hList, lvmSetExtendedListViewStyle, ext, ext)
		col := func(i int32, title string, w int32) {
			c := lvColumn{mask: lvcfFmt | lvcfWidth | lvcfText | lvcfSubItem, fmt: lvcfmtLeft, cx: w, pszText: u16(title), iSubItem: i}
			send(hList, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&c)))
		}
		col(0, "Папка", 270)
		col(1, "Состав", 136)
		col(2, "Статус", 192)
		col(3, "Режим", 152)

		lblMode := createWindow("STATIC", "Режим:", wsChild|wsVisible|wsClipSiblings, 12, 333, 46, 20, hwnd, 0)
		hMode = createWindow("COMBOBOX", "", wsChild|wsVisible|wsTabStop|cbsDropDownList|wsVScroll, 62, 330, 180, 200, hwnd, idMode)
		for _, fm := range folderModes {
			send(hMode, cbAddString, 0, uintptr(unsafe.Pointer(u16(fm.title))))
		}
		btnRefresh := createWindow("BUTTON", "Обновить", wsChild|wsVisible|wsTabStop|bsPushButton, 248, 330, 86, 27, hwnd, idRefresh)
		hFree = createWindow("BUTTON", "Освободить место", wsChild|wsVisible|wsTabStop|bsPushButton, 340, 330, 138, 27, hwnd, idFree)
		hDelete = createWindow("BUTTON", "Удалить с сервера", wsChild|wsVisible|wsTabStop|bsPushButton, 484, 330, 148, 27, hwnd, idDelete)
		hRule = createWindow("BUTTON", "Убрать правило", wsChild|wsVisible|wsTabStop|bsPushButton, 638, 330, 116, 27, hwnd, idRule)
		hFreeChk = createWindow("BUTTON", "Без правил — синхронизировать всё", wsChild|wsVisible|wsTabStop|bsAutoCheckbox|bsLeft, 12, 368, 420, 24, hwnd, idRulesFree)
		btnLog := createWindow("BUTTON", "Журнал", wsChild|wsVisible|wsTabStop|bsPushButton, 560, 366, 96, 27, hwnd, idLog)
		btnSet := createWindow("BUTTON", "Настройки…", wsChild|wsVisible|wsTabStop|bsPushButton, 664, 366, 104, 27, hwnd, idSettings)
		hint := createWindow("STATIC", "Список показывает папки первого уровня; всё вложенное следует её режиму.\r\nНовая папка ничего не передаёт, пока не выбран режим.\r\nКнопка «Х» сворачивает программу в трей — синхронизация продолжается.",
			wsChild|wsVisible|wsClipSiblings, 12, 400, 756, 70, hwnd, 0)
		for _, c := range []uintptr{hStatus, hList, lblMode, hMode, btnRefresh, hFree, hDelete, hRule, hFreeChk, btnLog, btnSet, hint} {
			send(c, wmSetFont, gFont, 1)
		}
		send(hTitle, wmSetFont, gFontBold, 1)
		send(hFreeChk, 0x00F1 /*BM_SETCHECK*/, boolPtr(ruleFreeEnabled()), 0)
		updateModeControls()
		procSetTimer.Call(hwnd, timerStatus, 1000, 0)
		trayAdd(hwnd, loadAppIcon(), "POSCloud Lite — синхронизация")
		updateStatusLine()
		if synConfigured() {
			requestFolders()
		}
		return 0
	case wmTimer:
		updateStatusLine()
		if synConfigured() {
			if int(time.Now().Unix())%10 == 0 {
				requestFolders()
			}
		}
		return 0
	case wmFolders:
		fillFolderList()
		return 0
	case wmNotify:
		hdr := (*nmhdr)(unsafe.Pointer(lParam))
		if hdr.idFrom == idList && hdr.code == lvnItemChanged {
			updateModeControls()
		}
		return 0
	case wmTray:
		switch lParam {
		case wmLButtonDblClk:
			showMainWindow()
		case wmRButtonUp:
			showTrayMenu()
		}
		return 0
	case wmCommand:
		switch wParam & 0xFFFF {
		case idMode:
			if wParam>>16 == cbnSelChange {
				doModeChange()
			}
		case idRefresh:
			doRefresh()
		case idFree:
			doFreeSpace()
		case idDelete:
			doDeleteFolder()
		case idRule:
			doRemoveRule()
		case idRulesFree:
			on := send(hFreeChk, 0x00F0 /*BM_GETCHECK*/, 0, 0) == 1
			go func() {
				if err := gSyn.SetRulesFree(on); err != nil {
					logf("режим без правил: %v", err)
				}
				requestFolders()
			}()
		case idLog, idMenuLog:
			shellOpen(logFile)
		case idSettings, idMenuSettings:
			openSettingsWindow()
		case idMenuReload:
			reloadConfig()
		case idMenuExit:
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmClose:
		procShowWindow.Call(hwnd, swHide)
		logf("окно свёрнуто в трей — синхронизация продолжается (двойной клик по значку или запуск программы возвращает окно)")
		return 0
	case wmDestroy:
		procKillTimer.Call(hwnd, timerStatus)
		trayDelete(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func updateStatusLine() {
	st := gSyn.Status()
	run := gSyn.Running()
	txt := "Синхронизация: "
	switch {
	case !synConfigured():
		txt += "не настроена — откройте «Настройки» и укажите сервер, токен и папку"
	case run:
		txt += "работает"
		if st.ErrMsg != "" {
			txt += " · " + st.ErrMsg
		} else {
			txt += " · связь есть"
		}
	default:
		txt += "остановлена"
	}
	if st.LastSync != "" {
		txt += " · последний обмен " + st.LastSync
	}
	if st.Count > 0 {
		txt += fmt.Sprintf(" · файлов в состоянии: %d", st.Count)
	}
	setText(hStatus, txt)
}

// reloadConfig перечитывает файл настроек и применяет их.
func reloadConfig() {
	wasRunning := gSyn.Running()
	if wasRunning {
		gSyn.Stop()
	}
	gCfg = loadConfig()
	if err := engine.ConfigureTLS(gCfg.TLSFingerprint); err != nil {
		logf("TLS: %v", err)
	}
	gSyn.Update(gCfg.Config)
	if wasRunning && synConfigured() {
		gSyn.Run()
	}
	send(hFreeChk, 0x00F1, boolPtr(ruleFreeEnabled()), 0)
	logf("настройки перечитаны из %s", configPath())
	updateStatusLine()
	requestFolders()
}

// ---------- окно настроек ----------

var settingsWndProcCb = syscall.NewCallback(settingsWndProc)

func openSettingsWindow() {
	if hwndSettings != 0 {
		procShowWindow.Call(hwndSettings, swShow)
		procSetForegroundWindow.Call(hwndSettings)
		return
	}
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   settingsWndProcCb,
		hInstance:     getModuleHandle(),
		hIcon:         loadAppIcon(),
		hCursor:       loadCursorArrow(),
		hbrBackground: uintptr(colorWindow + 1),
		lpszClassName: u16(settingsClassName),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(settingsClassName))),
		uintptr(unsafe.Pointer(u16("Настройки — "+appTitle))),
		uintptr(wsCaption|wsSysMenu|wsVisible|wsClipChildren),
		200, 150, 720, 330,
		0, 0, getModuleHandle(), 0)
}

func settingsWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		hwndSettings = hwnd
		lab := func(text string, x, y, w, h int32) {
			c := createWindow("STATIC", text, wsChild|wsVisible|wsClipSiblings, x, y, w, h, hwnd, 0)
			send(c, wmSetFont, gFont, 1)
		}
		edit := func(text string, x, y, w, h int32, id uintptr) uintptr {
			c := createWindow("EDIT", text, wsChild|wsVisible|wsTabStop|wsBorder|wsClipSiblings, x, y, w, h, hwnd, id)
			send(c, wmSetFont, gFont, 1)
			return c
		}
		lab("Сервер:", 12, 16, 80, 20)
		hEdHost = edit(gCfg.Host, 96, 14, 300, 24, idSHost)
		lab(":", 402, 16, 10, 20)
		hEdPort = edit(fmt.Sprintf("%d", gCfg.Port), 414, 14, 70, 24, idSPort)
		lab("Папка синхронизации:", 12, 50, 168, 20)
		hEdDir = edit(gCfg.Dir, 186, 48, 386, 24, idSDir)
		btnBrowse := createWindow("BUTTON", "Обзор…", wsChild|wsVisible|wsTabStop|bsPushButton, 580, 48, 110, 24, hwnd, idSBrowse)
		lab("Токен доступа:", 12, 84, 120, 20)
		hEdToken = edit(gCfg.Token, 138, 82, 552, 24, idSToken)
		lab("Интервал, сек:", 12, 118, 110, 20)
		hEdInt = edit(fmt.Sprintf("%d", gCfg.Interval), 128, 116, 70, 24, idSInt)
		lab("как часто проверять изменения", 206, 118, 340, 20)
		hChkAuto = createWindow("BUTTON", "Запускать при входе в Windows", wsChild|wsVisible|wsTabStop|bsAutoCheckbox|bsLeft, 12, 152, 320, 24, hwnd, idSCheck)
		btnTest := createWindow("BUTTON", "Проверить связь", wsChild|wsVisible|wsTabStop|bsPushButton, 370, 200, 130, 28, hwnd, idSTest)
		btnSave := createWindow("BUTTON", "Сохранить", wsChild|wsVisible|wsTabStop|bsPushButton, 508, 200, 92, 28, hwnd, idSSave)
		btnCancel := createWindow("BUTTON", "Отмена", wsChild|wsVisible|wsTabStop|bsPushButton, 608, 200, 84, 28, hwnd, idSCancel)
		for _, c := range []uintptr{btnBrowse, hChkAuto, btnTest, btnSave, btnCancel} {
			send(c, wmSetFont, gFont, 1)
		}
		send(hChkAuto, 0x00F1, boolPtr(autostartEnabled()), 0)
		return 0
	case wmCommand:
		switch wParam & 0xFFFF {
		case idSBrowse:
			if p := browseFolder(hwnd, "Папка синхронизации"); p != "" {
				setText(hEdDir, p)
			}
		case idSCheck:
			on := send(hChkAuto, 0x00F0, 0, 0) == 1
			if err := setAutostart(on); err != nil {
				logf("автозапуск: %v", err)
				messageBox(hwnd, "Не удалось изменить автозапуск:\n"+err.Error(), appTitle, mbOk|mbIconExclamation)
				send(hChkAuto, 0x00F1, boolPtr(autostartEnabled()), 0)
			} else {
				gCfg.Autostart = on
				_ = saveConfig(gCfg)
				logf("автозапуск при входе: %v", on)
			}
		case idSTest:
			port, _ := strconv.Atoi(strings.TrimSpace(getText(hEdPort)))
			_ = engine.ConfigureTLS(gCfg.TLSFingerprint)
			res := engine.TestConnection(strings.TrimSpace(getText(hEdHost)), port, strings.TrimSpace(getText(hEdToken)), gCfg.TLS)
			messageBox(hwnd, res, appTitle, mbOk|mbIconInfo)
		case idSSave:
			port, err1 := strconv.Atoi(strings.TrimSpace(getText(hEdPort)))
			if err1 != nil || port <= 0 || port > 65535 {
				messageBox(hwnd, "Проверьте порт сервера — нужно число от 1 до 65535.", appTitle, mbOk|mbIconExclamation)
				return 0
			}
			itv, err2 := strconv.Atoi(strings.TrimSpace(getText(hEdInt)))
			if err2 != nil || itv <= 0 {
				itv = 3
			}
			next := gCfg // правила и «без правил» сохраняются как есть
			next.Host = strings.TrimSpace(getText(hEdHost))
			next.Port = port
			next.Dir = strings.TrimSpace(getText(hEdDir))
			next.Token = strings.TrimSpace(getText(hEdToken))
			next.Interval = itv
			next.Autostart = send(hChkAuto, 0x00F0, 0, 0) == 1
			if gSyn.Running() {
				gSyn.Stop()
			}
			gCfg = next
			gSyn.Update(gCfg.Config)
			if err := saveConfig(gCfg); err != nil {
				messageBox(hwnd, "Не удалось сохранить файл настроек:\n"+err.Error(), appTitle, mbOk|mbIconExclamation)
			}
			if synConfigured() {
				gSyn.Run()
			}
			logf("настройки сохранены: %s", configPath())
			send(hFreeChk, 0x00F1, boolPtr(ruleFreeEnabled()), 0)
			updateStatusLine()
			requestFolders()
			procDestroyWindow.Call(hwnd)
		case idSCancel:
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		hwndSettings = 0
		hEdHost, hEdPort, hEdDir, hEdToken, hEdInt, hChkAuto = 0, 0, 0, 0, 0, 0
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// ---------- запуск ----------

func main() {
	initLog()
	logf("POSCloud Lite запускается (каталог: %s)", exeDir())
	procCoInitializeEx.Call(0, coinitApartmentThreaded)

	if !acquireSingleInstance(appMutexName) {
		if !showExistingInstance(mainClassName) {
			logf("программа уже запущена, но окно не найдено")
		}
		return
	}

	gCfg = loadConfig()
	if err := engine.ConfigureTLS(gCfg.TLSFingerprint); err != nil {
		logf("TLS: %v", err)
	}
	gSyn = engine.NewSyncer(gCfg.Config, func(s string) { logf("%s", s) }, func(c engine.Config) {
		gCfg.Config = c
		_ = saveConfig(gCfg)
	})

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   mainWndProcCb,
		hInstance:     getModuleHandle(),
		hIcon:         loadAppIcon(),
		hCursor:       loadCursorArrow(),
		hbrBackground: uintptr(colorWindow + 1),
		lpszClassName: u16(mainClassName),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// меню
	hmenu := menuCreate()
	hfile := menuCreatePopup()
	menuAppend(hfile, idMenuSettings, "Настройки…")
	menuAppend(hfile, idMenuReload, "Перечитать настройки")
	menuAppend(hfile, idMenuLog, "Открыть журнал")
	menuAppend(hfile, idMenuExit, "Выход")
	menuAppendPopup(hmenu, hfile, "Программа")

	hwndMain, _, _ = procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(mainClassName))),
		uintptr(unsafe.Pointer(u16(appTitle))),
		uintptr(wsCaption|wsSysMenu|wsMinimizeBox|wsClipChildren),
		120, 120, 800, 560,
		0, hmenu, getModuleHandle(), 0)
	if hwndMain == 0 {
		logf("не удалось создать окно")
		return
	}

	if synConfigured() {
		gSyn.Run()
		logf("синхронизация запущена: сервер %s:%d, папка %s", gCfg.Host, gCfg.Port, gCfg.Dir)
	} else {
		logf("синхронизация не настроена: укажите host/port/token/dir в «Настройках»")
	}

	procShowWindow.Call(hwndMain, swShow)
	procUpdateWindow.Call(hwndMain)
	runMessageLoop()
	gSyn.Stop()
	logf("выход")
}
