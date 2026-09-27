//go:build windows

package main

import "testing"

// Текст состояния в шапке меню должен точно соответствовать реальному состоянию.
func TestSyncStateLabel(t *testing.T) {
	cases := []struct {
		name            string
		running, online bool
		errMsg, want    string
	}{
		{"остановлено", false, false, "", "остановлено"},
		{"работает и связан", true, true, "", "синхронизировано"},
		{"работает без признака связи", true, false, "", "синхронизация…"},
		{"нет связи", true, false, "нет связи с сервером", "нет связи с сервером"},
		{"ошибка важнее признака связи", true, true, "ошибка", "нет связи с сервером"},
	}
	for _, c := range cases {
		if got := syncStateLabel(c.running, c.online, c.errMsg); got != c.want {
			t.Errorf("%s: получено %q, ожидалось %q", c.name, got, c.want)
		}
	}
}
