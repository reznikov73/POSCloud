package main

import "testing"

func TestFolderRulesDefaultOff(t *testing.T) {
	r := parseFolderRules(nil)
	if got := r.modeFor("любая/подпапка/файл.txt"); got != folderOff {
		t.Fatalf("без правил ожидался режим off (новая папка ждёт решения), получено %q", got)
	}
	if got := r.modeForFile("заметка.txt"); got != folderBoth {
		t.Fatalf("файл в корне должен синхронизироваться в обе стороны, получено %q", got)
	}
}

func TestRulesFreeSyncsEverything(t *testing.T) {
	r := parseFolderRules([]FolderRule{{Path: "Видео", Mode: folderOff}})
	r.free = true
	for _, p := range []string{"Видео/фильм.mp4", "Новая/файл.txt", "заметка.txt"} {
		if got := r.modeForFile(p); got != folderBoth {
			t.Fatalf("в режиме без правил для %q ожидался both, получено %q", p, got)
		}
	}
}

func TestFolderRulesDeepestWins(t *testing.T) {
	r := parseFolderRules([]FolderRule{
		{Path: "Видео", Mode: folderOff},
		{Path: "Видео/Отпуск", Mode: folderUp},
		{Path: "Сканы", Mode: folderUp},
	})
	cases := []struct {
		path string
		want string
	}{
		{"Видео/фильм.mp4", folderOff},
		{"Видео/Отпуск/фото.jpg", folderUp},
		{"Видео/Отпуск/вложенное/фото.jpg", folderUp},
		{"Сканы/док.pdf", folderUp},
		{"Документы/док.pdf", folderOff},
		{"Сканы-2/док.pdf", folderOff},
		{"Видео и музыка/файл", folderOff},
	}
	for _, c := range cases {
		if got := r.modeFor(c.path); got != c.want {
			t.Errorf("modeFor(%q) = %q, ожидалось %q", c.path, got, c.want)
		}
	}
}

func TestFolderRulesCaseInsensitive(t *testing.T) {
	r := parseFolderRules([]FolderRule{{Path: "Сканы", Mode: folderDown}})
	for _, p := range []string{"сканы/док.pdf", "СКАНЫ/Док.PDF", "Сканы/вложенное/файл"} {
		if got := r.modeFor(p); got != folderDown {
			t.Errorf("modeFor(%q) = %q, ожидалось down", p, got)
		}
	}
}

func TestFolderRulesModeHelpers(t *testing.T) {
	r := parseFolderRules([]FolderRule{
		{Path: "Отправка", Mode: folderUp},
		{Path: "Приём", Mode: folderDown},
		{Path: "Выкл", Mode: folderOff},
	})
	if !r.canUpload("Отправка/файл") || r.canDownload("Отправка/файл") {
		t.Fatal("«только отправлять»: загрузка разрешена, скачивание запрещено")
	}
	if r.canUpload("Приём/файл") || !r.canDownload("Приём/файл") {
		t.Fatal("«только получать»: загрузка запрещена, скачивание разрешено")
	}
	if !r.isOff("Выкл/файл") || r.canUpload("Выкл/файл") || r.canDownload("Выкл/файл") {
		t.Fatal("«не синхронизировать»: оба направления закрыты")
	}
	if r.canUpload("Обычное/файл") || r.canDownload("Обычное/файл") {
		t.Fatal("без правила папка не синхронизируется")
	}
	if !r.canUpload("заметка.txt") || !r.canDownload("заметка.txt") {
		t.Fatal("файл в корне синхронизируется в обе стороны")
	}
}

func TestNormalizeRulePath(t *testing.T) {
	good := map[string]string{
		"Сканы":         "Сканы",
		"\\Сканы\\":     "Сканы",
		"a/b":           "a/b",
		"a\\b\\c":       "a/b/c",
		"  папка/под  ": "папка/под",
	}
	for in, want := range good {
		got, ok := normalizeRulePath(in)
		if !ok || got != want {
			t.Errorf("normalizeRulePath(%q) = %q, %v; ожидалось %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "  ", "/", "..", "../x", "a/../b", "a/./b", "a//b"} {
		if got, ok := normalizeRulePath(bad); ok {
			t.Errorf("normalizeRulePath(%q) должен отклоняться, получено %q", bad, got)
		}
	}
}

func TestParseFolderRulesDropsInvalid(t *testing.T) {
	r := parseFolderRules([]FolderRule{
		{Path: "Ок", Mode: folderUp},
		{Path: "Плохой режим", Mode: "странный"},
		{Path: "..", Mode: folderUp},
		{Path: "", Mode: folderDown},
		{Path: "ок", Mode: folderDown}, // дубль по регистру: последний побеждает
	})
	if len(r.rules) != 1 {
		t.Fatalf("ожидалось 1 правило, получено %d: %+v", len(r.rules), r.rules)
	}
	if r.rules[0].Mode != folderDown {
		t.Fatalf("последний дубль должен побеждать: %+v", r.rules[0])
	}
}

func TestFolderRulesCovered(t *testing.T) {
	r := parseFolderRules([]FolderRule{
		{Path: "Фото", Mode: folderBoth},
		{Path: "Музыка/Рок", Mode: folderUp},
	})
	cases := []struct {
		path string
		want bool
	}{
		{"Фото", true},
		{"Фото/снимок.jpg", true},
		{"Фото/2024/снимок.jpg", true},
		{"Музыка", false},
		{"Музыка/Рок", true},
		{"Музыка/Рок/песня.mp3", true},
		{"Музыка/Джаз/песня.mp3", false},
		{"Заметки/текст.txt", false},
	}
	for _, c := range cases {
		if got := r.covered(c.path); got != c.want {
			t.Errorf("covered(%q) = %v, ожидалось %v", c.path, got, c.want)
		}
	}
}
