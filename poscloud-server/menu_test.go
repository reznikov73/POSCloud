//go:build windows

package main

import "testing"

// Текст состояния в шапке меню должен точно соответствовать реальному состоянию.
func TestServerStateLabel(t *testing.T) {
	cases := []struct {
		name          string
		running, free bool
		port          int
		want          string
	}{
		{"работает", true, true, 8099, "работает, порт 8099"},
		{"порт занят", false, false, 8090, "порт занят другим процессом"},
		{"остановлен", false, true, 8090, "остановлен"},
		{"работает даже если порт занят извне", true, false, 8090, "работает, порт 8090"},
	}
	for _, c := range cases {
		if got := serverStateLabel(c.running, c.free, c.port); got != c.want {
			t.Errorf("%s: получено %q, ожидалось %q", c.name, got, c.want)
		}
	}
}
