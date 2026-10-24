package main

import (
	"net/url"
	"testing"
)

func TestCreateConnectionCodeRoundTrip(t *testing.T) {
	s := testAuthStore(t)
	u, err := s.createUser("Дом")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{auth: s, srvRun: true, srvPort: 8099}
	cc := a.CreateConnectionCode(u.ID, "")
	if cc.Error != "" {
		t.Fatalf("create error: %s", cc.Error)
	}
	u2, err := url.Parse(cc.Code)
	if err != nil {
		t.Fatal(err)
	}
	code := u2.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in %q", cc.Code)
	}
	tok, got, err := s.redeemPairing(code, "PC")
	if err != nil {
		t.Fatalf("redeem: %v (code=%q)", err, code)
	}
	if tok == "" || got.ID != u.ID {
		t.Fatalf("bad result")
	}
}
