package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ConnectionInfo — разобранный код подключения POSCloud.
//
// Формат кода:
//
//	poscloud://connect?v=1&host=...&port=...&tls=1&fp=...&code=...
//
// В коде нет постоянного токена: только одноразовый секрет, который клиент
// обменивает на обычный токен через POST /pair.
type ConnectionInfo struct {
	Host        string
	Port        int
	TLS         bool
	Fingerprint string
	Code        string
	DeviceName  string
}

// PairResult — ответ сервера на обмен одноразового кода.
type PairResult struct {
	Token      string `json:"token"`
	UserID     string `json:"userID"`
	UserName   string `json:"userName"`
	DeviceName string `json:"deviceName"`
}

// ParseConnectionCode разбирает и проверяет текстовый код подключения.
func ParseConnectionCode(raw string) (ConnectionInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ConnectionInfo{}, fmt.Errorf("пустой код подключения")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("не удалось разобрать код: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "poscloud") {
		return ConnectionInfo{}, fmt.Errorf("неверная схема кода (ожидается poscloud://)")
	}
	if !strings.EqualFold(u.Host, "connect") {
		return ConnectionInfo{}, fmt.Errorf("неверный тип кода подключения")
	}
	q := u.Query()
	if q.Get("v") != "1" {
		return ConnectionInfo{}, fmt.Errorf("неподдерживаемая версия кода")
	}
	host := strings.TrimSpace(q.Get("host"))
	if host == "" {
		return ConnectionInfo{}, fmt.Errorf("в коде не указан адрес сервера")
	}
	if strings.ContainsAny(host, " /\\?#") {
		return ConnectionInfo{}, fmt.Errorf("недопустимый адрес сервера")
	}
	port, err := strconv.Atoi(strings.TrimSpace(q.Get("port")))
	if err != nil || port < 1 || port > 65535 {
		return ConnectionInfo{}, fmt.Errorf("неверный порт сервера")
	}
	tlsOn := q.Get("tls") == "1" || strings.EqualFold(q.Get("tls"), "true")
	fp := strings.TrimSpace(q.Get("fp"))
	if tlsOn && fp == "" {
		return ConnectionInfo{}, fmt.Errorf("для TLS в коде нужен отпечаток сертификата")
	}
	code := strings.TrimSpace(q.Get("code"))
	if code == "" {
		return ConnectionInfo{}, fmt.Errorf("в коде нет одноразового секрета")
	}
	deviceName := strings.TrimSpace(q.Get("name"))
	if r := []rune(deviceName); len(r) > 80 {
		deviceName = string(r[:80])
	}
	return ConnectionInfo{Host: host, Port: port, TLS: tlsOn, Fingerprint: fp, Code: code, DeviceName: deviceName}, nil
}

// Pair обменивает одноразовый код подключения на обычный токен устройства.
// TLS-доверие должно быть настроено вызывающей стороной через ConfigureTLS.
func Pair(baseURL, code, deviceName string) (PairResult, error) {
	body, err := json.Marshal(map[string]string{
		"code":       strings.TrimSpace(code),
		"deviceName": strings.TrimSpace(deviceName),
	})
	if err != nil {
		return PairResult{}, err
	}
	req, err := http.NewRequest("POST", strings.TrimRight(baseURL, "/")+"/pair", bytes.NewReader(body))
	if err != nil {
		return PairResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return PairResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(readErrorBody(resp.Body))
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusGone:
			if msg == "" {
				msg = "код подключения недействителен или истёк"
			}
		case http.StatusTooManyRequests:
			if msg == "" {
				msg = "слишком много попыток подключения, повторите позже"
			}
		default:
			if msg == "" {
				msg = fmt.Sprintf("сервер вернул статус %d", resp.StatusCode)
			}
		}
		return PairResult{}, fmt.Errorf("%s", msg)
	}
	var out PairResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PairResult{}, fmt.Errorf("не удалось прочитать ответ сервера: %w", err)
	}
	if strings.TrimSpace(out.Token) == "" {
		return PairResult{}, fmt.Errorf("сервер не выдал токен устройства")
	}
	return out, nil
}

// readErrorBody достаёт поле error из JSON-ответа сервера, не раскрывая тело
// целиком (там не должно быть секретов, но лишний текст в UI не нужен).
func readErrorBody(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 2048))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &payload) == nil && strings.TrimSpace(payload.Error) != "" {
		return strings.TrimSpace(payload.Error)
	}
	return strings.TrimSpace(string(b))
}
