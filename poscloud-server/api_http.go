package main

import (
	"strings"
	"path/filepath"
	"os"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
)

// ---------- HTTP-доступ к тем же методам, что и окно ----------
//
// Окно Wails вызывает методы App напрямую через биндинг (window.go.main.App).
// Браузер такого моста не имеет, поэтому здесь те же методы выставляются
// по HTTP: POST /api/<метод> с JSON-массивом аргументов, ответ — JSON.
//
// Разбор аргументов идёт через рефлексию, а не 37 отдельными обработчиками:
// список методов один, и его достаточно поддерживать в одном месте.
//
// Методы, которым нужен GUI-контекст (диалог выбора папки, буфер обмена),
// в браузере возвращают пустой результат или понятную ошибку — см. guiMethods.

// apiMethod описывает один метод, доступный по HTTP.
type apiMethod struct {
	// name — имя метода App и путь /api/<name>.
	name string
	// needsGUI — метод требует окна Wails; в браузере работает заглушка.
	needsGUI bool
}

// apiMethods — белый список методов. Всё, чего здесь нет, по HTTP недоступно:
// это защита от случайного вызова внутренних методов через рефлексию.
var apiMethods = []apiMethod{
	{name: "GetServerState"},
	{name: "StartServer"},
	{name: "StopServer"},
	{name: "SaveSettings"},
	{name: "GetLogs"},
	{name: "ClearLogs"},
	{name: "GetTransfers"},

	{name: "ListUsers"},
	{name: "CreateUser"},
	{name: "DeleteUser"},
	{name: "SetUserEnabled"},
	{name: "ListTokens"},
	{name: "IssueToken"},
	{name: "ReissueToken"},
	{name: "RenameToken"},
	{name: "RevokeToken"},
	{name: "CreateConnectionCode"},
	{name: "QRForConnectionCode"},
	{name: "ExportConnectionCode", needsGUI: true},

	{name: "UserFiles"},
	{name: "DeleteUserFile"},
	{name: "History"},
	{name: "Trash"},
	{name: "Conflicts"},
	{name: "ResolveConflict"},

	{name: "CreateBackup"},
	{name: "RestoreBackup"},

	{name: "PortHolder"},
	{name: "ReleasePort"},

	{name: "AutoRunEnabled"},
	{name: "SetAutoRun"},
	{name: "AutostartEnabled"},
	{name: "SetAutostart"},
	{name: "AutostartLabel"},

	// Эти в браузере не имеют смысла, но интерфейс их вызывает:
	// отвечаем безопасной заглушкой, чтобы не ломать страницу.
	{name: "PickDirectory", needsGUI: true},
	{name: "CheckDirectory"},
	{name: "CopyToClipboard", needsGUI: true},
	{name: "OpenLogFile", needsGUI: true},
	{name: "WindowHidden", needsGUI: true},
}

// apiIndex — имя метода → описание.
var apiIndex = func() map[string]apiMethod {
	m := make(map[string]apiMethod, len(apiMethods))
	for _, meth := range apiMethods {
		m[meth.name] = meth
	}
	return m
}()

// apiApp — тот же *App, что создаёт main. Ставится из main.go до запуска сервера.
var (
	apiAppMu sync.RWMutex
	apiApp   *App
)

// setAPIApp запоминает экземпляр App для HTTP-вызовов.
func setAPIApp(a *App) {
	apiAppMu.Lock()
	apiApp = a
	apiAppMu.Unlock()
}

// getAPIApp возвращает экземпляр App.
func getAPIApp() *App {
	apiAppMu.RLock()
	defer apiAppMu.RUnlock()
	return apiApp
}

// registerAPIHandlers вешает обработчик /api/ на mux.
func registerAPIHandlers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/{method}", handleAPICall)
}

// handleAPICall вызывает метод App по имени.
func handleAPICall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("method")
	meta, ok := apiIndex[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "неизвестный метод"})
		return
	}

	app := getAPIApp()
	if app == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "приложение не инициализировано"})
		return
	}

	// Тело — JSON-массив аргументов. Пустое тело означает вызов без аргументов.
	var raw []json.RawMessage
	if r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "неверный JSON аргументов"})
			return
		}
	}

	if meta.needsGUI {
		out, err := callGUIMethod(app, name, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": out})
		return
	}

	out, err := callMethod(app, name, raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
}

// callMethod вызывает метод App через рефлексию и возвращает результат.
// Ошибка метода (последнее возвращаемое значение error) превращается в error.
func callMethod(app *App, name string, raw []json.RawMessage) (any, error) {
	mv := reflect.ValueOf(app).MethodByName(name)
	if !mv.IsValid() {
		return nil, fmt.Errorf("метод %s не найден", name)
	}
	mt := mv.Type()
	if len(raw) != mt.NumIn() {
		return nil, fmt.Errorf("метод %s ждёт %d аргументов, получено %d", name, mt.NumIn(), len(raw))
	}

	args := make([]reflect.Value, mt.NumIn())
	for i := 0; i < mt.NumIn(); i++ {
		ptr := reflect.New(mt.In(i))
		if err := json.Unmarshal(raw[i], ptr.Interface()); err != nil {
			return nil, fmt.Errorf("аргумент %d: %w", i+1, err)
		}
		args[i] = ptr.Elem()
	}

	out := mv.Call(args)

	// Последнее значение типа error — это ошибка метода, а не результат.
	if n := len(out); n > 0 {
		last := out[n-1]
		if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !last.IsNil() {
				return nil, last.Interface().(error)
			}
			out = out[:n-1]
		}
	}

	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		return out[0].Interface(), nil
	default:
		// Несколько результатов — отдаём массивом.
		vals := make([]any, len(out))
		for i, v := range out {
			vals[i] = v.Interface()
		}
		return vals, nil
	}
}

// callGUIMethod отвечает на вызовы методов, которым нужен Wails-контекст.
// В браузере окна нет, поэтому:
//   - PickDirectory возвращает пустую строку (интерфейс покажет «не выбрано»);
//   - CopyToClipboard отвечает ошибкой — в браузере копирование делает сам JS;
//   - OpenLogFile и WindowHidden просто подтверждают вызов.
func callGUIMethod(app *App, name string, raw []json.RawMessage) (any, error) {
	switch name {
	case "PickDirectory":
		return "", nil
	case "CopyToClipboard":
		return nil, fmt.Errorf("копирование выполняет браузер")
	case "OpenLogFile":
		return nil, nil
	case "WindowHidden":
		return false, nil
	}
	return nil, fmt.Errorf("метод %s недоступен по HTTP", name)
}

// dirCheck — ответ проверки каталога.
type dirCheck struct {
	Exists  bool   `json:"exists"`
	Created bool   `json:"created"`
	Error   string `json:"error,omitempty"`
	Path    string `json:"path"`
}

// CheckDirectory проверяет каталог хранения, выбранный в браузере.
// Системного диалога там нет, поэтому путь вводят текстом — его надо
// проверить до сохранения настроек: опечатка иначе всплывёт при запуске.
func (a *App) CheckDirectory(path string) dirCheck {
	p := strings.TrimSpace(path)
	if p == "" {
		return dirCheck{Error: "путь не указан"}
	}
	if !filepath.IsAbs(p) {
		return dirCheck{Error: "нужен полный путь (начинается с «/»)", Path: p}
	}
	fi, err := os.Stat(p)
	if err == nil {
		if !fi.IsDir() {
			return dirCheck{Error: "это файл, а не каталог", Path: p}
		}
		// Проверяем запись: каталог может быть только для чтения.
		f, werr := os.CreateTemp(p, ".poscloud-write-*")
		if werr != nil {
			return dirCheck{Exists: true, Error: "нет прав на запись в каталог", Path: p}
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return dirCheck{Exists: true, Path: p}
	}
	if !os.IsNotExist(err) {
		return dirCheck{Error: "не удалось проверить каталог: " + err.Error(), Path: p}
	}
	// Каталога нет: проверим, что родитель существует и доступен на запись.
	parent := filepath.Dir(p)
	if pfi, perr := os.Stat(parent); perr != nil || !pfi.IsDir() {
		return dirCheck{Error: "родительский каталог не существует: " + parent, Path: p}
	}
	return dirCheck{Created: true, Path: p}
}
