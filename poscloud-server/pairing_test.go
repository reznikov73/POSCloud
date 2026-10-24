package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testAuthStore(t *testing.T) *authStore {
	t.Helper()
	return newAuthStore(t.TempDir() + "/users.json")
}

func TestPairingIssueAndRedeem(t *testing.T) {
	s := testAuthStore(t)
	u, err := s.createUser("Дом")
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.issuePairing(u.ID, pairingTTL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Code, "pcp_") {
		t.Fatalf("unexpected code %q", info.Code)
	}
	if info.UserName != "Дом" || info.UserID != u.ID {
		t.Fatalf("unexpected info: %+v", info)
	}
	tok, gotUser, err := s.redeemPairing(info.Code, "WORK-PC")
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" || gotUser.ID != u.ID {
		t.Fatalf("unexpected redeem result: token=%q user=%+v", tok, gotUser)
	}
	uid, _, ok := s.findUserByToken(tok)
	if !ok || uid != u.ID {
		t.Fatalf("paired token not accepted: uid=%q ok=%v", uid, ok)
	}
	if _, _, err := s.redeemPairing(info.Code, "SECOND"); err == nil {
		t.Fatal("pairing code was reused")
	}
	got, ok := s.get(u.ID)
	if !ok {
		t.Fatal("user missing")
	}
	tokens := got.Tokens
	if len(tokens) != 1 || tokens[0].Name != "WORK-PC" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestPairingExpiry(t *testing.T) {
	s := testAuthStore(t)
	u, _ := s.createUser("Дом")
	info, err := s.issuePairing(u.ID, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.redeemPairing(info.Code, "PC"); err == nil {
		t.Fatal("expired code accepted")
	}
}

func TestPairingDisabledUser(t *testing.T) {
	s := testAuthStore(t)
	u, _ := s.createUser("Дом")
	if err := s.setUserEnabled(u.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.issuePairing(u.ID, pairingTTL); err == nil {
		t.Fatal("issued code for disabled user")
	}
}

func TestPairingUnknownCode(t *testing.T) {
	s := testAuthStore(t)
	if _, _, err := s.redeemPairing("pcp_missing", "PC"); err == nil {
		t.Fatal("unknown code accepted")
	}
}

func TestHandlePair(t *testing.T) {
	s := testAuthStore(t)
	u, _ := s.createUser("Дом")
	info, _ := s.issuePairing(u.ID, pairingTTL)
	a := &App{auth: s}
	pairAttempts = newPairLimiter(100, time.Minute)

	body := `{"code":"` + info.Code + `","deviceName":"WORK-PC"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(body))
	req.RemoteAddr = "192.0.2.10:1234"
	a.handlePair(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"token":"pcs_`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/pair", strings.NewReader(body))
	a.handlePair(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d", rec.Code)
	}
}
