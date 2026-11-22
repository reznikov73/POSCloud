package main

import (
	"encoding/base64"
	"fmt"
	"net/http"

	qrcode "github.com/skip2/go-qrcode"
)

// ---------- QR-код подключения ----------
//
// Код подключения — строка вида poscloud://connect?v=1&host=...&code=...
// Её удобно передать телефону QR-кодом: набирать вручную 200+ символов
// с отпечатком сертификата невозможно.
//
// QR рисуется на сервере и отдаётся как data-URL (PNG в base64): так он
// одинаково работает в окне Wails и в браузере, без сторонних JS-библиотек.

// qrDataURL возвращает PNG с QR-кодом в виде data-URL.
// size — сторона картинки в пикселях.
func qrDataURL(text string, size int) (string, error) {
	if text == "" {
		return "", fmt.Errorf("пустой текст для QR")
	}
	if size <= 0 || size > 1024 {
		size = 320
	}
	// Уровень коррекции Medium: код короткий, избыточность не нужна,
	// зато модули крупнее и телефону проще считывать с экрана.
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	code.DisableBorder = false
	png, err := code.PNG(size)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// ConnectionCodeQR — ответ с QR-кодом для готового кода подключения.
type ConnectionCodeQR struct {
	Code  string `json:"code"`
	QR    string `json:"qr,omitempty"`
	Error string `json:"error,omitempty"`
}

// QRForConnectionCode рисует QR по строке кода подключения.
// Метод доступен и окну (через биндинг), и браузеру (через /api).
func (a *App) QRForConnectionCode(code string) ConnectionCodeQR {
	if len(code) > 4096 {
		return ConnectionCodeQR{Error: "код слишком длинный"}
	}
	dataURL, err := qrDataURL(code, 360)
	if err != nil {
		return ConnectionCodeQR{Code: code, Error: err.Error()}
	}
	return ConnectionCodeQR{Code: code, QR: dataURL}
}

// handleCodeQR отдаёт PNG с QR-кодом по строке из параметра.
// Используется, если картинку нужно открыть отдельной ссылкой.
func (a *App) handleCodeQR(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminSession(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "требуется вход в админку"})
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "не указан код"})
		return
	}
	dataURL, err := qrDataURL(code, 360)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": ConnectionCodeQR{Code: code, QR: dataURL}})
}
