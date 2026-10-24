package main

import (
	"path/filepath"
	"testing"
)

func TestListTokensReturnsIssuedAndReissuedTokens(t *testing.T) {
	a := &App{}
	a.auth = newAuthStore(filepath.Join(t.TempDir(), "users.json"))

	u, err := a.auth.createUser("andrey")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	tok, err := a.auth.issueToken(u.ID, "Рабочий ПК")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	list := a.ListTokens(u.ID)
	if len(list) != 1 {
		t.Fatalf("ожидался один токен, получено %d", len(list))
	}
	if !list[0].Available || list[0].Token != tok {
		t.Fatalf("новый токен должен быть доступен для копирования: %+v", list[0])
	}
	if _, _, ok := a.auth.findUserByToken(tok); !ok {
		t.Fatal("выданный токен не проходит аутентификацию")
	}

	newTok, err := a.auth.reissueToken(u.ID, list[0].ID)
	if err != nil {
		t.Fatalf("reissueToken: %v", err)
	}
	if _, _, ok := a.auth.findUserByToken(tok); ok {
		t.Fatal("старый токен должен перестать работать после перевыпуска")
	}
	if _, _, ok := a.auth.findUserByToken(newTok); !ok {
		t.Fatal("новый токен должен проходить аутентификацию")
	}
	list = a.ListTokens(u.ID)
	if len(list) != 1 || list[0].Token != newTok || !list[0].Available {
		t.Fatalf("после перевыпуска ожидался новый доступный токен: %+v", list)
	}
}

func TestListTokensMarksHashOnlyTokenUnavailable(t *testing.T) {
	a := &App{}
	a.auth = newAuthStore(filepath.Join(t.TempDir(), "users.json"))
	u, err := a.auth.createUser("old-user")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	a.auth.users[0].Tokens = append(a.auth.users[0].Tokens, DeviceToken{
		ID: "old", Name: "Старое устройство", Hash: hashToken("old-secret"), Enabled: true, CreatedAt: nowStamp(),
	})
	list := a.ListTokens(u.ID)
	if len(list) != 1 {
		t.Fatalf("ожидался один токен, получено %d", len(list))
	}
	if list[0].Available || list[0].Token != "" {
		t.Fatalf("старый hash-only токен не должен быть доступен: %+v", list[0])
	}
}
