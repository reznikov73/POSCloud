package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigRoundTrip checks saving and loading client.json settings.
func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewController(dir)
	auto := true
	c.mu.Lock()
	c.cfg.Host = "192.168.1.50"
	c.cfg.Port = 9001
	c.cfg.Dir = filepath.Join(dir, "sync")
	c.cfg.Interval = 7
	c.cfg.Token = "secret-token"
	c.cfg.TLS = true
	c.cfg.TLSFingerprint = "AA:BB"
	c.cfg.DeviceName = "My phone"
	c.cfg.AutoRun = &auto
	c.mu.Unlock()
	c.persist()

	// A new controller reads the same settings
	c2 := NewController(dir)
	cfg := c2.Config()
	if cfg.Host != "192.168.1.50" || cfg.Port != 9001 || cfg.Interval != 7 || cfg.Token != "secret-token" {
		t.Fatalf("settings not restored: %+v", cfg)
	}
	if !cfg.TLS || cfg.TLSFingerprint != "AA:BB" {
		t.Fatalf("TLS settings not restored: %+v", cfg)
	}
	if cfg.DeviceName != "My phone" {
		t.Fatalf("device name not restored: %q", cfg.DeviceName)
	}
	if !c2.AutoRunEnabled() {
		t.Fatal("autoRun must be enabled")
	}
	if !c2.HasCredentials() {
		t.Fatal("token and folder must be present")
	}
	if c2.DeviceName() != "My phone" {
		t.Fatalf("DeviceName: %q", c2.DeviceName())
	}
}

// TestDeviceNameFallback checks the default device name.
func TestDeviceNameFallback(t *testing.T) {
	c := NewController(t.TempDir())
	if c.DeviceName() == "" {
		t.Fatal("device name must not be empty")
	}
}

// TestTransfers checks the transfer progress receiver.
func TestTransfers(t *testing.T) {
	s := &TransferStore{m: map[string]*transferState{}}
	s.Begin("a.txt", "up", 1000)
	s.Begin("b.txt", "down", 500)
	s.Add("a.txt", 250)
	s.Add("b.txt", 500) // more than the full size — clamped from above
	s.Add("missing.txt", 10)
	list := s.Snapshot()
	if len(list) != 2 {
		t.Fatalf("expected 2 transfers, got %d", len(list))
	}
	byName := map[string]Transfer{}
	for _, tr := range list {
		byName[tr.Name] = tr
	}
	if byName["a.txt"].Pct != 25 {
		t.Fatalf("a.txt: percent %d, expected 25", byName["a.txt"].Pct)
	}
	if byName["b.txt"].Pct != 100 {
		t.Fatalf("b.txt: percent %d, expected 100 (clamped from above)", byName["b.txt"].Pct)
	}
	s.End("a.txt")
	if len(s.Snapshot()) != 1 {
		t.Fatal("after End(a.txt) one transfer must remain")
	}
}

// TestLogFile checks that the log is written to a file and accumulated in memory.
func TestLogFile(t *testing.T) {
	dir := t.TempDir()
	c := NewController(dir)
	c.Log("test line")
	data, err := os.ReadFile(filepath.Join(dir, "client.log"))
	if err != nil {
		t.Fatalf("client.log not created: %v", err)
	}
	if !strings.Contains(string(data), "test line") {
		t.Fatalf("log not written: %q", string(data))
	}
	lines := c.Logs()
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "test line") {
		t.Fatalf("in-memory log is empty: %v", lines)
	}
	if !c.LogDirtyAndReset() {
		t.Fatal("the log must be flagged as changed")
	}
	if c.LogDirtyAndReset() {
		t.Fatal("after the reset the flag must not be set again")
	}
	c.ClearLogs()
	if len(c.Logs()) != 0 {
		t.Fatal("the log must be cleared")
	}
}
