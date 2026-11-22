package client

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// TokenConnection — a parsed connection code with an existing device token.
//
// Format (QR "Device token" on the server):
//
//	poscloud://connect?v=1&host=...&port=...&name=...&token=...&tls=1&fp=...
//
// Unlike a regular connection code, instead of a one-time secret
// this contains a permanent token of an existing device: the server does not re-issue
// it, and the client saves it as is — without an exchange via POST /pair. Parsing
// is done in the Android client, not in the shared engine: desktop clients
// do not use this form (see commit e00ff96 on the server).
type TokenConnection struct {
	Host        string
	Port        int
	TLS         bool
	Fingerprint string
	Token       string
	DeviceName  string
}

// errNoDeviceToken — the code has no token field: it is a regular code with a one-time
// secret, parsed by engine.ParseConnectionCode.
var errNoDeviceToken = errors.New("the code has no device token")

// ParseTokenConnection parses and validates a connection code with an existing
// device token. The checks mirror engine.ParseConnectionCode,
// so that both code forms behave equally predictably.
func ParseTokenConnection(raw string) (TokenConnection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TokenConnection{}, fmt.Errorf("empty connection code")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return TokenConnection{}, fmt.Errorf("failed to parse the code: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "poscloud") {
		return TokenConnection{}, fmt.Errorf("invalid code scheme (expected poscloud://)")
	}
	if !strings.EqualFold(u.Host, "connect") {
		return TokenConnection{}, fmt.Errorf("invalid connection code type")
	}
	q := u.Query()
	if q.Get("v") != "1" {
		return TokenConnection{}, fmt.Errorf("unsupported code version")
	}
	token := strings.TrimSpace(q.Get("token"))
	if token == "" {
		return TokenConnection{}, errNoDeviceToken
	}
	host := strings.TrimSpace(q.Get("host"))
	if host == "" {
		return TokenConnection{}, fmt.Errorf("the code does not specify the server address")
	}
	if strings.ContainsAny(host, " /\\?<=") {
		return TokenConnection{}, fmt.Errorf("invalid server address")
	}
	port, err := strconv.Atoi(strings.TrimSpace(q.Get("port")))
	if err != nil || port < 1 || port > 65535 {
		return TokenConnection{}, fmt.Errorf("invalid server port")
	}
	tlsOn := q.Get("tls") == "1" || strings.EqualFold(q.Get("tls"), "true")
	fp := strings.TrimSpace(q.Get("fp"))
	if tlsOn && fp == "" {
		return TokenConnection{}, fmt.Errorf("TLS requires the certificate fingerprint in the code")
	}
	deviceName := strings.TrimSpace(q.Get("name"))
	if r := []rune(deviceName); len(r) > 80 {
		deviceName = string(r[:80])
	}
	return TokenConnection{Host: host, Port: port, TLS: tlsOn, Fingerprint: fp, Token: token, DeviceName: deviceName}, nil
}
