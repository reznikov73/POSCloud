//go:build windows

package main

import (
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- глобальные горячие клавиши ----------
//
// В API Wails глобальных горячих клавиш нет: регистрируем их через скрытое
// окно-приёмник и цикл сообщений в отдельном потоке. Сочетания настраиваются
// пользователем (хранятся в client.json), изменения применяются на лету —
// поток получает сообщение и перерегистрирует клавиши.

const (
	hkRegion = 1
	hkWindow = 2
	hkFull   = 3

	wmHotkey     = 0x0312
	wmAppReload  = 0x8000 + 1 // перечитать настройки и перерегистрировать
	wmAppSuspend = 0x8000 + 2 // снять регистрацию (пользователь вводит сочетание)
	wmAppResume  = 0x8000 + 3 // вернуть регистрацию

	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modNoRepeat = 0x4000

	hwndMessage = ^uintptr(2) // HWND_MESSAGE = (HWND)-3
)

// HotkeyConfig — назначенные сочетания; пустая строка означает «выключено».
type HotkeyConfig struct {
	Region string `json:"region"`
	Window string `json:"window"`
	Full   string `json:"full"`
}

// DefaultHotkeys — сочетания по умолчанию.
func DefaultHotkeys() HotkeyConfig {
	return HotkeyConfig{Region: "Ctrl+Shift+1", Window: "Ctrl+Shift+2", Full: "Ctrl+Shift+3"}
}

// HotkeyState — состояние одного сочетания для интерфейса.
type HotkeyState struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Combo string `json:"combo"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type hotkeyAction struct {
	id    uintptr
	key   string
	title string
	run   func(*App)
}

func hotkeyActions() []hotkeyAction {
	return []hotkeyAction{
		{hkRegion, "region", "Снимок области", func(a *App) { a.trayShotRegion() }},
		{hkWindow, "window", "Снимок активного окна", func(a *App) { a.trayShotWindow() }},
		{hkFull, "full", "Снимок всего экрана", func(a *App) { a.trayShotFull() }},
	}
}

var (
	user32K              = windows.NewLazySystemDLL("user32.dll")
	kernel32K            = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassEx  = user32K.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32K.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32K.NewProc("DefWindowProcW")
	procRegisterHotKey   = user32K.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32K.NewProc("UnregisterHotKey")
	procPostMessageW     = user32K.NewProc("PostMessageW")
	procGetMessageW      = user32K.NewProc("GetMessageW")
	procTranslateMessage = user32K.NewProc("TranslateMessage")
	procDispatchMessageW = user32K.NewProc("DispatchMessageW")
	procDestroyWindow    = user32K.NewProc("DestroyWindow")
	procGetModuleHandleW = kernel32K.NewProc("GetModuleHandleW")

	hotkeyMu     sync.Mutex
	hotkeyStates []HotkeyState
	hotkeyHwnd   uintptr
	hotkeyActive = map[uintptr]func(*App){}
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

// parseHotkey разбирает «Ctrl+Shift+1» в модификаторы и виртуальный код клавиши.
// Без модификатора сочетание не принимаем: одиночная клавиша перехватывалась бы
// во всех программах сразу.
func parseHotkey(s string) (mods uintptr, vk uintptr, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	parts := strings.Split(s, "+")
	if len(parts) < 2 {
		return 0, 0, false
	}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			mods |= modControl
		case "alt":
			mods |= modAlt
		case "shift":
			mods |= modShift
		default:
			return 0, 0, false
		}
	}
	if mods == 0 {
		return 0, 0, false
	}

	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	switch {
	case len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z':
		vk = uintptr(key[0])
	case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
		vk = uintptr(key[0])
	case strings.HasPrefix(key, "F"):
		var n int
		if _, err := fmt.Sscanf(key, "F%d", &n); err != nil || n < 1 || n > 24 {
			return 0, 0, false
		}
		vk = uintptr(0x70 + n - 1)
	default:
		return 0, 0, false
	}
	return mods | modNoRepeat, vk, true
}

func comboFor(cfg HotkeyConfig, key string) string {
	switch key {
	case "region":
		return cfg.Region
	case "window":
		return cfg.Window
	case "full":
		return cfg.Full
	}
	return ""
}

func setCombo(cfg *HotkeyConfig, key, combo string) {
	switch key {
	case "region":
		cfg.Region = combo
	case "window":
		cfg.Window = combo
	case "full":
		cfg.Full = combo
	}
}

// hotkeySummaryText собирает строку вида «Ctrl+Shift+1 — Снимок области, …».
func hotkeySummaryText(states []HotkeyState) string {
	if len(states) == 0 {
		return "сочетания не назначены"
	}
	parts := make([]string, 0, len(states))
	for _, s := range states {
		if s.OK {
			parts = append(parts, s.Combo+" — "+s.Title)
			continue
		}
		reason := s.Error
		if reason == "" {
			reason = "не назначено"
		}
		parts = append(parts, s.Title+" — "+reason)
	}
	return strings.Join(parts, ", ")
}

// HotkeySummary возвращает краткое описание текущих сочетаний.
func HotkeySummary() string {
	hotkeyMu.Lock()
	defer hotkeyMu.Unlock()
	return hotkeySummaryText(hotkeyStates)
}

// startHotkeys запускает обработчик горячих клавиш в отдельном потоке.
func (a *App) startHotkeys() {
	go a.hotkeyLoop()
}

func (a *App) hotkeyLoop() {
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

	hotkeyMu.Lock()
	hotkeyHwnd = hwnd
	hotkeyMu.Unlock()

	registerHotkeys(a, hwnd)

	var msg winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return // WM_QUIT или ошибка
		}
		switch msg.Message {
		case wmHotkey:
			hotkeyMu.Lock()
			fn := hotkeyActive[uintptr(msg.WParam)]
			hotkeyMu.Unlock()
			if fn != nil {
				go fn(a)
			}
			continue
		case wmAppReload, wmAppResume:
			registerHotkeys(a, hwnd)
			continue
		case wmAppSuspend:
			unregisterHotkeys(hwnd)
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// registerHotkeys снимает прежнюю регистрацию и назначает сочетания из настроек.
func registerHotkeys(a *App, hwnd uintptr) {
	cfg := a.hotkeysSnapshot()
	actions := hotkeyActions()

	unregisterHotkeys(hwnd)

	states := make([]HotkeyState, 0, len(actions))
	active := map[uintptr]func(*App){}
	for _, act := range actions {
		combo := strings.TrimSpace(comboFor(cfg, act.key))
		st := HotkeyState{Key: act.key, Title: act.title, Combo: combo}
		if combo == "" {
			st.Error = "не задано"
			states = append(states, st)
			continue
		}
		mods, vk, ok := parseHotkey(combo)
		if !ok {
			st.Error = "сочетание не разобрано"
			states = append(states, st)
			continue
		}
		if r, _, _ := procRegisterHotKey.Call(hwnd, act.id, mods, vk); r == 0 {
			st.Error = "сочетание занято другой программой"
			states = append(states, st)
			continue
		}
		st.OK = true
		states = append(states, st)
		active[act.id] = act.run
	}

	hotkeyMu.Lock()
	hotkeyStates = states
	hotkeyActive = active
	hotkeyMu.Unlock()

	a.log("горячие клавиши: " + hotkeySummaryText(states))
}

func unregisterHotkeys(hwnd uintptr) {
	for _, act := range hotkeyActions() {
		procUnregisterHotKey.Call(hwnd, act.id)
	}
	hotkeyMu.Lock()
	hotkeyActive = map[uintptr]func(*App){}
	hotkeyMu.Unlock()
}

func postHotkey(msg uint32) {
	hotkeyMu.Lock()
	hwnd := hotkeyHwnd
	hotkeyMu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, uintptr(msg), 0, 0)
	}
}

// ---------- привязки для интерфейса ----------

// Hotkeys возвращает сохранённые сочетания.
func (a *App) Hotkeys() HotkeyConfig { return a.hotkeysSnapshot() }

// HotkeyStates возвращает состояние сочетаний: какие удалось назначить и почему нет.
func (a *App) HotkeyStates() []HotkeyState {
	hotkeyMu.Lock()
	defer hotkeyMu.Unlock()
	out := make([]HotkeyState, len(hotkeyStates))
	copy(out, hotkeyStates)
	return out
}

// SetHotkeys сохраняет и применяет новые сочетания.
func (a *App) SetHotkeys(cfg HotkeyConfig) ([]HotkeyState, error) {
	for _, c := range []string{cfg.Region, cfg.Window, cfg.Full} {
		if strings.TrimSpace(c) == "" {
			continue
		}
		if _, _, ok := parseHotkey(c); !ok {
			return a.HotkeyStates(), fmt.Errorf("не удалось разобрать сочетание: %s", c)
		}
	}
	a.mu.Lock()
	a.hotkeys = cfg
	a.mu.Unlock()
	a.persistConfig()
	postHotkey(wmAppReload)
	time.Sleep(150 * time.Millisecond) // даём потоку применить настройки
	return a.HotkeyStates(), nil
}

// SuspendHotkeys временно снимает регистрацию: пока пользователь вводит
// сочетание в поле, старые клавиши не должны срабатывать.
func (a *App) SuspendHotkeys() { postHotkey(wmAppSuspend) }

// ResumeHotkeys возвращает регистрацию обратно.
func (a *App) ResumeHotkeys() { postHotkey(wmAppResume) }

func (a *App) hotkeysSnapshot() HotkeyConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hotkeys
}
