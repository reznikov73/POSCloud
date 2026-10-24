package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigureTLSPinning(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cert := srv.Certificate()
	sum := sha256.Sum256(cert.Raw)
	fp := hex.EncodeToString(sum[:])

	if err := ConfigureTLS(fp); err != nil {
		t.Fatalf("ConfigureTLS: %v", err)
	}
	resp, err := getHTTPClient().Get(srv.URL)
	if err != nil {
		t.Fatalf("запрос с верным отпечатком: %v", err)
	}
	_ = resp.Body.Close()

	if err := ConfigureTLS(strings.Repeat("00", 32)); err != nil {
		t.Fatalf("ConfigureTLS(wrong): %v", err)
	}
	if _, err := getHTTPClient().Get(srv.URL); err == nil {
		t.Fatal("запрос с неверным отпечатком должен завершиться ошибкой")
	}

	_ = ConfigureTLS("")
	if got := BaseURL("host", 8090, true); got != "https://host:8090" {
		t.Fatalf("BaseURL https = %q", got)
	}
	if got := BaseURL("host", 8090, false); got != "http://host:8090" {
		t.Fatalf("BaseURL http = %q", got)
	}
}
