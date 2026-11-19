package main

import (
	"flag"
	"testing"
)

// Автозапуск сервера при открытии программы включён по умолчанию;
// отключить его можно только явным аргументом -autostart=false.
func TestAutoStartIsOnByDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindServerFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("разбор без аргументов: %v", err)
	}
	if !f.autostart {
		t.Error("autostart по умолчанию выключен, ожидалось включённое значение")
	}
	if f.port != 0 {
		t.Errorf("-port по умолчанию = %d, ожидалось 0", f.port)
	}
	if f.data != "" {
		t.Errorf("-data по умолчанию = %q, ожидалось пустое значение", f.data)
	}
}

// Явный -autostart=false должен отключать автозапуск.
func TestAutoStartCanBeDisabled(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindServerFlags(fs)
	if err := fs.Parse([]string{"-autostart=false"}); err != nil {
		t.Fatalf("разбор аргумента: %v", err)
	}
	if f.autostart {
		t.Error("autostart остался включённым при явном -autostart=false")
	}
}

// -hidden используется записью автозапуска: окно при входе в систему не открывается.
func TestHiddenFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindServerFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("разбор без аргументов: %v", err)
	}
	if f.hidden {
		t.Error("-hidden должен быть выключен по умолчанию")
	}
	if err := fs.Parse([]string{"-hidden"}); err != nil {
		t.Fatalf("разбор -hidden: %v", err)
	}
	if !f.hidden {
		t.Error("-hidden должен включаться при явной передаче")
	}
}
