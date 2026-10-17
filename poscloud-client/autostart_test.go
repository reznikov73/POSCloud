package main

import (
	"flag"
	"strings"
	"testing"
)

func TestHiddenFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := bindClientFlags(fs)
	if f.hidden {
		t.Fatal("-hidden должен быть выключен по умолчанию")
	}
	if err := fs.Parse([]string{"-hidden"}); err != nil {
		t.Fatalf("не удалось разобрать -hidden: %v", err)
	}
	if !f.hidden {
		t.Fatal("-hidden должен включаться при явной передаче")
	}
}

func TestAutostartCommand(t *testing.T) {
	exe := `C:\Apps\poscloud-client.exe`
	cmd := autostartCommand(exe)
	if !strings.HasPrefix(cmd, `"C:\Apps\poscloud-client.exe"`) {
		t.Fatalf("команда автозапуска должна начинаться с пути в кавычках: %s", cmd)
	}
	for _, flagName := range []string{"-autostart", "-hidden"} {
		if !strings.Contains(cmd, flagName) {
			t.Fatalf("в команде автозапуска нет %s: %s", flagName, cmd)
		}
	}
}

func TestAutostartNeedsUpdate(t *testing.T) {
	exe := `C:\Apps\poscloud-client.exe`
	cases := []struct {
		value string
		want  bool
	}{
		{`"C:\Apps\poscloud-client.exe" -autostart`, true},
		{`"C:\Apps\poscloud-client.exe" -autostart -hidden`, false},
		{`"D:\Other\poscloud-client.exe" -autostart`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := autostartNeedsUpdate(c.value, exe); got != c.want {
			t.Fatalf("autostartNeedsUpdate(%q) = %v, ожидалось %v", c.value, got, c.want)
		}
	}
}
