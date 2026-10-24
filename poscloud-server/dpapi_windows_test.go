//go:build windows

package main

import "testing"

func TestTokenProtectionRoundTrip(t *testing.T) {
	const tok = "test-token-1234567890"
	enc, err := protectToken(tok)
	if err != nil {
		t.Fatalf("protectToken: %v", err)
	}
	if enc == "" || enc == tok {
		t.Fatal("токен не был зашифрован")
	}
	dec, err := unprotectToken(enc)
	if err != nil {
		t.Fatalf("unprotectToken: %v", err)
	}
	if dec != tok {
		t.Fatalf("после расшифровки получен %q, ожидался %q", dec, tok)
	}
}
