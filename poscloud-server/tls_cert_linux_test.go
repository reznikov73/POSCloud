//go:build linux

package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Полная проверка HTTPS: сертификат создаётся, сервер поднимается,
// отпечаток совпадает с реальным, клиент с проверкой отпечатка подключается.
func TestHTTPSFullCycle(t *testing.T) {
	dir := t.TempDir()

	certFile, keyFile, fp, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatalf("ensureTLSCert: %v", err)
	}
	if _, err := os.Stat(certFile); err != nil {
		t.Fatalf("файл сертификата: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("файл ключа: %v", err)
	}
	t.Logf("отпечаток: %s", fp)
	if len(fp) != 64 {
		t.Fatalf("отпечаток длиной %d, ожидалось 64 hex-символа SHA-256", len(fp))
	}

	// Разбираем сертификат и сверяем отпечаток независимо.
	data, _ := os.ReadFile(certFile)
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("сертификат не PEM")
	}
	sum := sha256.Sum256(block.Bytes)
	want := strings.ToUpper(hex.EncodeToString(sum[:]))
	if fp != want {
		t.Fatalf("отпечаток %s != пересчитанный %s", fp, want)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("разбор сертификата: %v", err)
	}
	t.Logf("CN=%s, до %s, SAN=%v %v", cert.Subject.CommonName, cert.NotAfter.Format("2006-01-02"), cert.DNSNames, cert.IPAddresses)
	if !cert.NotAfter.After(time.Now().AddDate(9, 0, 0)) {
		t.Errorf("срок действия меньше 9 лет: %s", cert.NotAfter)
	}

	// Поднимаем сервер с TLS на свободном порту.
	a := NewApp()
	a.auth = newAuthStore(filepath.Join(dir, "users.json"))
	a.auth.setLogger(a.log)
	a.srvTLS = true
	st := a.StartServer(0, dir)
	if st.Error != "" {
		t.Fatalf("сервер не стартовал: %s", st.Error)
	}
	defer a.StopServer()
	if st.Port == 0 {
		t.Fatal("порт не определён")
	}
	if st.Fingerprint == "" {
		t.Error("состояние не содержит отпечатка сертификата")
	}
	if st.Fingerprint != fp {
		t.Errorf("отпечаток в состоянии %q != %q", st.Fingerprint, fp)
	}

	base := fmt.Sprintf("https://127.0.0.1:%d", st.Port)

	// 1. Клиент БЕЗ доверия сертификату должен получить ошибку проверки.
	insecureNo := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{}}}
	_, err = insecureNo.Get(base + "/health")
	if err == nil {
		t.Fatal("самоподписанный сертификат был принят без проверки — это неверно")
	}
	t.Logf("ожидаемый отказ без доверия: %v", err)

	// 2. Клиент с проверкой отпечатка (как в POSCloud Client) подключается.
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	ok := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := ok.Get(base + "/health")
	if err != nil {
		t.Fatalf("с доверенным сертификатом не подключился: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("/health вернул %d: %s", resp.StatusCode, body)
	}
	t.Logf("/health по HTTPS: %s", body)

	// 3. Сертификат должен подходить для внешнего IP (клиенты ходят по нему).
	ips := localIPs()
	if len(ips) > 0 {
		found := false
		for _, ip := range cert.IPAddresses {
			for _, myIP := range ips {
				if ip.Equal(myIP) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("в сертификате нет ни одного локального IP: сертификат %v, локальные %v", cert.IPAddresses, ips)
		}
	}
}
