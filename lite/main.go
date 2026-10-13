// POSCloud Lite — лёгкая версия клиента для старых Windows (7+): нативный
// интерфейс Win32, без WebView2. Синхронизацию выполняет общий движок
// poscloud-engine. Настройки читаются из client.json (совместим с обычным
// клиентом) или poscloud-lite.json рядом с программой.
package main

import (
	"encoding/json"
	"fmt"
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

	idSEdit   = 3001
	idSCheck  = 3002
	idSTest   = 3003
	idSSave   = 3004
	idSCancel = 3005

	idMenuSettings = 2001
	idMenuReload   = 2002
	idMenuLog      = 2003
	idMenuExit     = 2009

	timerStatus = 1
	wmFolders   = wmApp + 1
)

var (
	gCfg liteConfig
	gSyn *engine.Syncer

	hwndMain uintptr
	hStatus  uintptr
	hList    uintptr
	hMode    uintptr
	hFree    uintptr
	hDelete  uintptr
	hRule    uintptr
	hFreeChk uintptr
	gFont    uintptr

	hwndSettings uintptr
	hSetEdit     uintptr
	hSetCheck    uintptr

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
			return strings.ToLower(fm.title)
		}
	}
	return m
}

func statusText(n engine.ServerFolderNode) string {
	switch {
	case n.OnlyRule:
		return "правило без папки"
	case n.New:
		return "новая — выберите режим"
	case n.Mode == "off":
		if n.Local {
			return "есть на этом ПК (пауза)"
		}
		return "не синхронизируется"
	case n.Local:
		return "есть на этом ПК"
	case n.Mode == "both" || n.Mode == "down":
		return "будет скачана"
	}
	return "нет на этом ПК"
}

func rowText(n engine.ServerFolderNode) string {
	meta := "папка"
	if n.Files > 0 {
		meta = fmt.Sprintf("%d ф. · %s", n.Files, fmtSize(n.Bytes))
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s  ·  %s", n.Path, meta, statusText(n), modeLabel(n.Mode))
}

func setBtnEnabled(h uintptr, on bool) {
	var v uintptr
	if on {
		v = 1
	}
	procEnableWindow.Call(h, v)
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

func selectedIndex() int {
	r := send(hList, lbGetCurSel, 0, 0)
	if r == ^uintptr(0) { // LB_ERR
		return -1
	}
	foldersMu.Lock()
	defer foldersMu.Unlock()
	i := int(r)
	if i < 0 || i >= len(folders) {
		return -1
	}
	return i
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

func selectedIndexLocked() int {
	r := send(hList, lbGetCurSel, 0, 0)
	if r == ^uintptr(0) {
		return -1
	}
	i := int(r)
	if i < 0 || i >= len(folders) {
		return -1
	}
	return i
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
	// выбранный режим строки
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

	// сохранить выбранный путь
	selPath := ""
	if i := selectedIndex(); i >= 0 {
		selPath = list[i].Path
	}
	send(hList, lbResetContent, 0, 0)
	newSel := -1
	for i, n := range list {
		send(hList, lbAddString, 0, uintptr(unsafe.Pointer(u16(rowText(n)))))
		if selPath != "" && n.Path == selPath {
			newSel = i
		}
	}
	if newSel >= 0 {
		send(hList, lbSetCurSel, uintptr(newSel), 0)
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

func mainWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		hwndMain = hwnd
		gFont = createFont(16)
		hStatus = createWindow("STATIC", "запуск…", wsChild|wsVisible|wsClipSiblings, 12, 10, 756, 20, hwnd, 0)
		hList = createWindow("LISTBOX", "", wsChild|wsVisible|wsTabStop|wsBorder|wsVScroll|wsClipSiblings|lbsNotify|lbsNoIntegralHeight, 12, 34, 756, 286, hwnd, idList)
		stLabel := createWindow("STATIC", "Режим:", wsChild|wsVisible|wsClipSiblings, 12, 331, 46, 20, hwnd, 0)
		hMode = createWindow("COMBOBOX", "", wsChild|wsVisible|wsTabStop|cbsDropDownList|wsVScroll, 62, 328, 180, 200, hwnd, idMode)
		for _, fm := range folderModes {
			send(hMode, cbAddString, 0, uintptr(unsafe.Pointer(u16(fm.title))))
		}
		btnRefresh := createWindow("BUTTON", "Обновить", wsChild|wsVisible|wsTabStop|bsPushButton, 248, 328, 86, 27, hwnd, idRefresh)
		hFree = createWindow("BUTTON", "Освободить место", wsChild|wsVisible|wsTabStop|bsPushButton, 340, 328, 138, 27, hwnd, idFree)
		hDelete = createWindow("BUTTON", "Удалить с сервера", wsChild|wsVisible|wsTabStop|bsPushButton, 484, 328, 148, 27, hwnd, idDelete)
		hRule = createWindow("BUTTON", "Убрать правило", wsChild|wsVisible|wsTabStop|bsPushButton, 638, 328, 116, 27, hwnd, idRule)
		hFreeChk = createWindow("BUTTON", "Без правил — синхронизировать всё", wsChild|wsVisible|wsTabStop|bsAutoCheckbox|bsLeft, 12, 366, 420, 24, hwnd, idRulesFree)
		btnLog := createWindow("BUTTON", "Журнал", wsChild|wsVisible|wsTabStop|bsPushButton, 560, 364, 96, 27, hwnd, idLog)
		btnSet := createWindow("BUTTON", "Настройки…", wsChild|wsVisible|wsTabStop|bsPushButton, 664, 364, 104, 27, hwnd, idSettings)
		hint := createWindow("STATIC", "Список показывает папки первого уровня: у каждой — состав, состояние и режим. Вложенные папки и файлы следуют режиму своей папки.\r\nНовая папка ничего не передаёт, пока ей не выбран режим. Закрытие окна не останавливает синхронизацию — запустите программу снова, чтобы вернуть окно.",
			wsChild|wsVisible|wsClipSiblings, 12, 402, 756, 64, hwnd, 0)
		for _, c := range []uintptr{hStatus, hList, stLabel, hMode, btnRefresh, hFree, hDelete, hRule, hFreeChk, btnLog, btnSet, hint} {
			send(c, wmSetFont, gFont, 1)
		}
		send(hFreeChk, 0x00F1 /*BM_SETCHECK*/, boolPtr(ruleFreeEnabled()), 0)
		updateModeControls()
		procSetTimer.Call(hwnd, timerStatus, 1000, 0)
		updateStatusLine()
		if synConfigured() {
			requestFolders()
		}
		return 0
	case wmTimer:
		updateStatusLine()
		if wParam == timerStatus {
			if int(time.Now().Unix())%10 == 0 && synConfigured() {
				requestFolders()
			}
		}
		return 0
	case wmFolders:
		fillFolderList()
		return 0
	case wmCommand:
		id := uintptr(wParam & 0xFFFF)
		notify := uintptr(wParam >> 16)
		switch id {
		case idList:
			if notify == lbnSelChange {
				updateModeControls()
			}
		case idMode:
			if notify == cbnSelChange {
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
		case idLog:
			shellOpen(logFile)
		case idSettings:
			openSettingsWindow()
		case idMenuSettings:
			openSettingsWindow()
		case idMenuReload:
			reloadConfig()
		case idMenuLog:
			shellOpen(logFile)
		case idMenuExit:
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmClose:
		procShowWindow.Call(hwnd, swHide)
		logf("окно скрыто — синхронизация продолжается в фоне (запустите программу снова, чтобы вернуть окно)")
		return 0
	case wmDestroy:
		procKillTimer.Call(hwnd, timerStatus)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
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
		200, 150, 640, 460,
		0, 0, getModuleHandle(), 0)
}

func settingsWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		hwndSettings = hwnd
		if gFont == 0 {
			gFont = createFont(16)
		}
		lbl := createWindow("STATIC", "Файл: "+configPath(), wsChild|wsVisible|wsClipSiblings, 12, 10, 610, 20, hwnd, 0)
		lbl2 := createWindow("STATIC", "Поля: host, port, dir, token, interval. После правки нажмите «Сохранить».", wsChild|wsVisible|wsClipSiblings, 12, 30, 610, 20, hwnd, 0)
		hSetEdit = createWindow("EDIT", configText(gCfg), wsChild|wsVisible|wsTabStop|wsBorder|wsVScroll|esMultiline|esAutoVScroll|esWantReturn, 12, 54, 610, 300, hwnd, idSEdit)
		hSetCheck = createWindow("BUTTON", "Запускать при входе в Windows", wsChild|wsVisible|wsTabStop|bsAutoCheckbox|bsLeft, 12, 368, 300, 24, hwnd, idSCheck)
		send(hSetCheck, 0x00F1, boolPtr(autostartEnabled()), 0)
		btnTest := createWindow("BUTTON", "Проверить связь", wsChild|wsVisible|wsTabStop|bsPushButton, 316, 366, 120, 27, hwnd, idSTest)
		btnSave := createWindow("BUTTON", "Сохранить", wsChild|wsVisible|wsTabStop|bsPushButton, 480, 366, 100, 27, hwnd, idSSave)
		btnCancel := createWindow("BUTTON", "Отмена", wsChild|wsVisible|wsTabStop|bsPushButton, 586, 366, 90, 27, hwnd, idSCancel)
		for _, c := range []uintptr{lbl, lbl2, hSetEdit, hSetCheck, btnTest, btnSave, btnCancel} {
			send(c, wmSetFont, gFont, 1)
		}
		return 0
	case wmCommand:
		id := uintptr(wParam & 0xFFFF)
		switch id {
		case idSCheck:
			on := send(hSetCheck, 0x00F0, 0, 0) == 1
			if err := setAutostart(on); err != nil {
				logf("автозапуск: %v", err)
				messageBox(hwnd, "Не удалось изменить автозапуск:\n"+err.Error(), appTitle, mbOk|mbIconExclamation)
				send(hSetCheck, 0x00F1, boolPtr(autostartEnabled()), 0)
			} else {
				gCfg.Autostart = on
				_ = saveConfig(gCfg)
				logf("автозапуск при входе: %v", on)
			}
		case idSTest:
			var probe liteConfig
			if err := json.Unmarshal([]byte(getText(hSetEdit)), &probe); err != nil {
				messageBox(hwnd, "JSON не разобран:\n"+err.Error(), appTitle, mbOk|mbIconExclamation)
				return 0
			}
			res := engine.TestConnection(probe.Host, probe.Port, probe.Token)
			messageBox(hwnd, res, appTitle, mbOk|mbIconInfo)
		case idSSave:
			var next liteConfig
			if err := json.Unmarshal([]byte(getText(hSetEdit)), &next); err != nil {
				messageBox(hwnd, "JSON не разобран:\n"+err.Error(), appTitle, mbOk|mbIconExclamation)
				return 0
			}
			if next.Interval <= 0 {
				next.Interval = 3
			}
			next.Autostart = send(hSetCheck, 0x00F0, 0, 0) == 1
			wasRunning := gSyn.Running()
			if wasRunning {
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
		hSetEdit = 0
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// ---------- запуск ----------

func main() {
	initLog()
	logf("POSCloud Lite запускается (каталог: %s)", exeDir())

	if !acquireSingleInstance(appMutexName) {
		if !showExistingInstance(mainClassName) {
			logf("программа уже запущена, но окно не найдено")
		}
		return
	}

	gCfg = loadConfig()
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
