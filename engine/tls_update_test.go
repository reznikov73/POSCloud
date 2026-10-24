package engine

import "testing"

func TestUpdateCopiesTLSSettings(t *testing.T) {
	s := NewSyncer(Config{}, func(string) {}, func(Config) {})
	s.Update(Config{
		Host: "example", Port: 8443, TLS: true, TLSFingerprint: "ABC",
	})
	cfg := s.Config()
	if !cfg.TLS || cfg.TLSFingerprint != "ABC" {
		t.Fatalf("Update потерял TLS-настройки: %+v", cfg)
	}
}
