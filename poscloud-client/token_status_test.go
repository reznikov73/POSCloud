//go:build windows

package main

import "testing"

func TestSyncStateLabelTokenRevoked(t *testing.T) {
	if got := syncStateLabel(true, false, "токен отозван"); got != "токен отозван" {
		t.Fatalf("ожидалось «токен отозван», получено %q", got)
	}
	if got := syncStateLabel(true, false, "нет связи с сервером"); got != "нет связи с сервером" {
		t.Fatalf("ожидалось «нет связи с сервером», получено %q", got)
	}
}
