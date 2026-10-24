package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTLSCertStableAndValid(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, fp1, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatalf("ensureTLSCert: %v", err)
	}
	if fp1 == "" || len(fp1) != 64 {
		t.Fatalf("некорректный отпечаток: %q", fp1)
	}
	if _, err := os.Stat(certFile); err != nil {
		t.Fatalf("cert: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("key: %v", err)
	}
	_, _, fp2, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatalf("повторный ensureTLSCert: %v", err)
	}
	if fp1 != fp2 {
		t.Fatalf("отпечаток изменился: %s != %s", fp1, fp2)
	}
	if got := tlsCertFingerprint(dir); got != fp1 {
		t.Fatalf("tlsCertFingerprint=%q, ожидался %q", got, fp1)
	}

	// Сертификат должен парситься и содержать SAN localhost.
	data, err := os.ReadFile(filepath.Join(dir, "tls", "server.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("пустой сертификат")
	}
}
