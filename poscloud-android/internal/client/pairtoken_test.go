package client

import (
	"errors"
	"testing"
)

// TestParseTokenConnectionOK parses a code from the QR "Device token"
// (format from the server's web admin: URLSearchParams, space is a plus).
func TestParseTokenConnectionOK(t *testing.T) {
	tc, err := ParseTokenConnection("poscloud://connect?v=1&host=192.168.1.10&port=8090&name=%D0%9C%D0%BE%D0%B9+%D1%82%D0%B5%D0%BB%D0%B5%D1%84%D0%BE%D0%BD&token=tok-123&tls=0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tc.Host != "192.168.1.10" || tc.Port != 8090 || tc.Token != "tok-123" {
		t.Fatalf("fields: %+v", tc)
	}
	if tc.TLS || tc.Fingerprint != "" {
		t.Fatalf("TLS was not expected: %+v", tc)
	}
	if tc.DeviceName != "Мой телефон" {
		t.Fatalf("device name: %q", tc.DeviceName)
	}
}

// TestParseTokenConnectionTLS checks the fingerprint requirement for TLS.
func TestParseTokenConnectionTLS(t *testing.T) {
	tc, err := ParseTokenConnection("poscloud://connect?v=1&host=example.org&port=443&token=sekret&tls=1&fp=AA:BB:CC")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !tc.TLS || tc.Fingerprint != "AA:BB:CC" {
		t.Fatalf("TLS fields: %+v", tc)
	}
	if _, err := ParseTokenConnection("poscloud://connect?v=1&host=example.org&port=443&token=sekret&tls=1"); err == nil {
		t.Fatal("an error without a fingerprint was expected")
	}
}

// TestParseTokenConnectionNoTokenFallbackSentinel: a code with a one-time secret
// must return errNoDeviceToken so that Pair switches to the engine.
func TestParseTokenConnectionNoTokenFallbackSentinel(t *testing.T) {
	_, err := ParseTokenConnection("poscloud://connect?v=1&host=h&port=8090&code=xyz")
	if !errors.Is(err, errNoDeviceToken) {
		t.Fatalf("errNoDeviceToken was expected, got: %v", err)
	}
}

// TestParseTokenConnectionBad — codes that must be rejected.
func TestParseTokenConnectionBad(t *testing.T) {
	cases := []string{
		"",
		"https://example.org/?token=x",
		"poscloud://other?v=1&host=h&port=1&token=x",
		"poscloud://connect?v=2&host=h&port=1&token=x",
		"poscloud://connect?v=1&token=x",               // no address
		"poscloud://connect?v=1&host=h&token=x",        // no port
		"poscloud://connect?v=1&host=h&port=0&token=x", // port out of range
		"poscloud://connect?v=1&host=h%2Fh&port=1&token=x",
	}
	for _, c := range cases {
		if _, err := ParseTokenConnection(c); err == nil {
			t.Errorf("code %q: a parse error was expected", c)
		}
	}
}

// TestPairWithDeviceToken: Pair with the token form does not go over the network —
// settings are saved immediately, the server is not queried.
func TestPairWithDeviceToken(t *testing.T) {
	ctrl := NewController(t.TempDir())
	res, err := ctrl.Pair("poscloud://connect?v=1&host=192.0.2.5&port=9090&name=%D0%9F%D0%BB%D0%B0%D0%BD%D1%88%D0%B5%D1%82&token=tok-42", "")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if res.Token != "tok-42" || res.UserName != "" || res.DeviceName != "Планшет" {
		t.Fatalf("result: %+v", res)
	}
	cfg := ctrl.Config()
	if cfg.Host != "192.0.2.5" || cfg.Port != 9090 || cfg.Token != "tok-42" {
		t.Fatalf("settings not applied: %+v", cfg)
	}
	if cfg.DeviceName != "Планшет" {
		t.Fatalf("device name: %q", cfg.DeviceName)
	}
	if !ctrl.HasCredentials() {
		t.Fatal("credentials not saved")
	}
}
