package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// QR-код должен строиться и быть валидным PNG.
func TestQRDataURL(t *testing.T) {
	code := "poscloud://connect?v=1&host=193.7.203.222&port=8090&tls=1&fp=58005596BA86&code=pcp_abc123"
	got, err := qrDataURL(code, 320)
	if err != nil {
		t.Fatalf("qrDataURL: %v", err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("ожидался data-URL с PNG, получено: %.40s", got)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "data:image/png;base64,"))
	if err != nil {
		t.Fatalf("base64 не декодируется: %v", err)
	}
	// Сигнатура PNG: 89 50 4E 47 0D 0A 1A 0A
	if len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("данные не являются PNG")
	}
	if len(raw) < 100 {
		t.Fatalf("PNG подозрительно мал: %d байт", len(raw))
	}
}

// Пустой код — ошибка, а не пустая картинка.
func TestQRDataURLEmpty(t *testing.T) {
	if _, err := qrDataURL("", 320); err == nil {
		t.Fatal("для пустой строки ожидалась ошибка")
	}
}

// Длинный код подключения (с отпечатком и именем устройства) тоже кодируется:
// QR вмещает до ~2900 байт, реальный код — около 250.
func TestQRDataURLLongCode(t *testing.T) {
	code := "poscloud://connect?v=1&host=193.7.203.222&port=8090&tls=1" +
		"&fp=58005596BA86C6940F77E3C386981EE41D62ACFFAB30084E9843C109376BEA16" +
		"&name=" + strings.Repeat("Устройство-", 10) +
		"&code=pcp_abcdefghijklmnopqrstuvwxyz0123456789"
	if _, err := qrDataURL(code, 360); err != nil {
		t.Fatalf("длинный код не закодировался: %v", err)
	}
}

// Через метод App: возвращается и код, и картинка.
func TestQRForConnectionCode(t *testing.T) {
	a := NewApp()
	const code = "poscloud://connect?v=1&code=pcp_test"
	res := a.QRForConnectionCode(code)
	if res.Error != "" {
		t.Fatalf("ошибка: %s", res.Error)
	}
	if res.Code != code {
		t.Fatalf("код вернулся изменённым: %q", res.Code)
	}
	if !strings.HasPrefix(res.QR, "data:image/png;base64,") {
		t.Fatal("нет data-URL с картинкой")
	}
}

// Слишком длинная строка отклоняется: QR не бесконечный.
func TestQRForConnectionCodeTooLong(t *testing.T) {
	a := NewApp()
	res := a.QRForConnectionCode(strings.Repeat("x", 5000))
	if res.Error == "" {
		t.Fatal("для строки в 5000 символов ожидалась ошибка")
	}
}
