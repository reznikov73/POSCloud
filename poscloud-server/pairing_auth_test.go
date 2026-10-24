package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPairEndpointBypassesBearerAuth(t *testing.T) {
	s := testAuthStore(t)
	u, err := s.createUser("Дом")
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.issuePairing(u.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pairAttempts = newPairLimiter(100, time.Minute)
	a := &App{auth: s, srvRun: true, srvPort: 8099}
	ts := httptest.NewServer(a.withLog(a.withAuth(a.serverMux())))
	defer ts.Close()

	body := `{"code":"` + info.Code + `","deviceName":"WORK-PC"}`
	resp, err := http.Post(ts.URL+"/pair", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair status = %d", resp.StatusCode)
	}

	// Обычные защищённые маршруты без токена по-прежнему должны отвергаться.
	resp2, err := http.Get(ts.URL + "/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("list without token status = %d", resp2.StatusCode)
	}
}
