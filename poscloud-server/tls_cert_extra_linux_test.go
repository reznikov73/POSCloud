//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Сертификат переиспользуется: повторный вызов не создаёт новый,
// иначе отпечаток менялся бы при каждом запуске и клиенты отваливались.
func TestTLSCertIsStable(t *testing.T) {
	dir := t.TempDir()
	_, _, fp1, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, fp2, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("отпечаток изменился при повторном вызове: %s != %s", fp1, fp2)
	}
}

// Ключ закрыт от других пользователей: 0600.
func TestTLSKeyPermissions(t *testing.T) {
	dir := t.TempDir()
	_, keyFile, _, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Fatalf("права на ключ %o, ожидались 0600", perm)
	}
}

// Каталог tls закрыт: 0700.
func TestTLSDirPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, _, _, err := ensureTLSCert(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "tls"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0700 {
		t.Fatalf("права на каталог %o, ожидались 0700", perm)
	}
}

// Повреждённый сертификат не должен ломать запуск: функция пересоздаёт его.
func TestTLSRecreatesBrokenCert(t *testing.T) {
	dir := t.TempDir()
	if _, _, _, err := ensureTLSCert(dir); err != nil {
		t.Fatal(err)
	}
	cert := filepath.Join(dir, "tls", "server.crt")
	if err := os.WriteFile(cert, []byte("мусор, не PEM"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, fp, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatalf("после повреждения сертификат не пересоздан: %v", err)
	}
	if fp == "" {
		t.Fatal("отпечаток пуст после пересоздания")
	}
}

// Отпечаток стабилен между перезапусками сервера (читается с диска).
func TestTLSFingerprintReadFromDisk(t *testing.T) {
	dir := t.TempDir()
	_, _, fp, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := tlsCertFingerprint(dir); got != fp {
		t.Fatalf("tlsCertFingerprint=%q != %q", got, fp)
	}
}
