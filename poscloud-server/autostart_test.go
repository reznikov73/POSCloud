package main

import (
	"strings"
	"testing"
)

// Команда автозапуска должна стартовать скрыто в трее: без -hidden
// после входа в систему открывалось бы окно.
func TestAutostartCommandIsHidden(t *testing.T) {
	exe := `/opt/poscloud/poscloud-server`
	cmd := autostartCommand(exe)
	if !strings.HasPrefix(cmd, `"`+exe+`"`) {
		t.Fatalf("команда автозапуска должна начинаться с пути в кавычках: %s", cmd)
	}
	for _, flagName := range []string{"-autostart", "-hidden"} {
		if !strings.Contains(cmd, flagName) {
			t.Fatalf("в команде автозапуска нет %s: %s", flagName, cmd)
		}
	}
}

// .desktop-файл должен содержать Exec с -hidden и TryExec без кавычек.
func TestDesktopContent(t *testing.T) {
	exe := `/opt/poscloud/poscloud-server`
	content := desktopContent(exe)
	if !strings.Contains(content, "[Desktop Entry]") {
		t.Fatalf("нет заголовка Desktop Entry: %s", content)
	}
	want := `Exec="` + exe + `" -autostart -hidden`
	if !strings.Contains(content, want) {
		t.Fatalf("нет строки %q в файле:\n%s", want, content)
	}
	if !strings.Contains(content, "TryExec="+exe+"\n") {
		t.Fatalf("TryExec должен быть путём без кавычек и флагов:\n%s", content)
	}
}

// Path с пробелами остаётся в кавычках: иначе Exec развалится на аргументы.
func TestDesktopContentQuotesPathWithSpaces(t *testing.T) {
	exe := `/home/user/Мои программы/poscloud-server`
	if !strings.Contains(desktopContent(exe), `Exec="`+exe+`" -autostart -hidden`) {
		t.Fatalf("путь с пробелами должен быть в кавычках:\n%s", desktopContent(exe))
	}
}

func TestDesktopExec(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"обычный файл", desktopContent("/opt/app"), `"/opt/app" -autostart -hidden`},
		{"нет Exec", "[Desktop Entry]\nType=Application\n", ""},
		{"пробелы вокруг", "[Desktop Entry]\nExec=  \"/opt/app\" -autostart\n", `"/opt/app" -autostart`},
	}
	for _, c := range cases {
		if got := desktopExec(c.data); got != c.want {
			t.Fatalf("%s: desktopExec() = %q, ожидалось %q", c.name, got, c.want)
		}
	}
}

// Запись прежней версии (без -hidden) должна распознаваться как устаревшая,
// а запись, указывающая на другой exe, трогать нельзя.
func TestAutostartNeedsUpdate(t *testing.T) {
	exe := `/opt/poscloud/poscloud-server`
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"старая запись без -hidden", `"/opt/poscloud/poscloud-server" -autostart`, true},
		{"актуальная запись", `"/opt/poscloud/poscloud-server" -autostart -hidden`, false},
		{"другой exe", `"/usr/local/bin/poscloud-server" -autostart`, false},
		{"пустая запись", "", false},
	}
	for _, c := range cases {
		if got := autostartNeedsUpdate(c.value, exe); got != c.want {
			t.Fatalf("%s: autostartNeedsUpdate(%q) = %v, ожидалось %v", c.name, c.value, got, c.want)
		}
	}
}

// Пустой exe означает «путь неизвестен» — такую запись не трогаем.
func TestAutostartNeedsUpdateEmptyExe(t *testing.T) {
	if autostartNeedsUpdate(`"/opt/app" -autostart`, "") {
		t.Fatal("при неизвестном exe запись автозапуска трогать нельзя")
	}
}
