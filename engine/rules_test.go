package engine

import "testing"

func TestFolderRulesDefaultOff(t *testing.T) {
	r := ParseRules(nil)
	if got := r.ModeFor("любая/подпапка/файл.txt"); got != FolderOff {
		t.Fatalf("без правил ожидался режим off (новая папка ждёт решения), получено %q", got)
	}
	if got := r.ModeForFile("заметка.txt"); got != FolderBoth {
		t.Fatalf("файл в корне должен синхронизироваться в обе стороны, получено %q", got)
	}
}

func TestRulesFreeSyncsEverything(t *testing.T) {
	r := ParseRules([]FolderRule{{Path: "Видео", Mode: FolderOff}})
	r.Free = true
	for _, p := range []string{"Видео/фильм.mp4", "Новая/файл.txt", "заметка.txt"} {
		if got := r.ModeForFile(p); got != FolderBoth {
			t.Fatalf("в режиме без правил для %q ожидался both, получено %q", p, got)
		}
	}
}

func TestFolderRulesDeepestWins(t *testing.T) {
	r := ParseRules([]FolderRule{
		{Path: "Видео", Mode: FolderOff},
		{Path: "Видео/Отпуск", Mode: FolderUp},
		{Path: "Сканы", Mode: FolderUp},
	})
	cases := []struct {
		path string
		want string
	}{
		{"Видео/фильм.mp4", FolderOff},
		{"Видео/Отпуск/фото.jpg", FolderUp},
		{"Видео/Отпуск/вложенное/фото.jpg", FolderUp},
		{"Сканы/док.pdf", FolderUp},
		{"Документы/док.pdf", FolderOff},
		{"Сканы-2/док.pdf", FolderOff},
		{"Видео и музыка/файл", FolderOff},
	}
	for _, c := range cases {
		if got := r.ModeFor(c.path); got != c.want {
			t.Errorf("ModeFor(%q) = %q, ожидалось %q", c.path, got, c.want)
		}
	}
}

func TestFolderRulesCaseInsensitive(t *testing.T) {
	r := ParseRules([]FolderRule{{Path: "Сканы", Mode: FolderDown}})
	for _, p := range []string{"сканы/док.pdf", "СКАНЫ/Док.PDF", "Сканы/вложенное/файл"} {
		if got := r.ModeFor(p); got != FolderDown {
			t.Errorf("ModeFor(%q) = %q, ожидалось down", p, got)
		}
	}
}

func TestFolderRulesModeHelpers(t *testing.T) {
	r := ParseRules([]FolderRule{
		{Path: "Отправка", Mode: FolderUp},
		{Path: "Приём", Mode: FolderDown},
		{Path: "Выкл", Mode: FolderOff},
	})
	if !r.CanUpload("Отправка/файл") || r.CanDownload("Отправка/файл") {
		t.Fatal("«только отправлять»: загрузка разрешена, скачивание запрещено")
	}
	if r.CanUpload("Приём/файл") || !r.CanDownload("Приём/файл") {
		t.Fatal("«только получать»: загрузка запрещена, скачивание разрешено")
	}
	if !r.IsOff("Выкл/файл") || r.CanUpload("Выкл/файл") || r.CanDownload("Выкл/файл") {
		t.Fatal("«не синхронизировать»: оба направления закрыты")
	}
	if r.CanUpload("Обычное/файл") || r.CanDownload("Обычное/файл") {
		t.Fatal("без правила папка не синхронизируется")
	}
	if !r.CanUpload("заметка.txt") || !r.CanDownload("заметка.txt") {
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
		got, ok := NormalizeRulePath(in)
		if !ok || got != want {
			t.Errorf("NormalizeRulePath(%q) = %q, %v; ожидалось %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "  ", "/", "..", "../x", "a/../b", "a/./b", "a//b"} {
		if got, ok := NormalizeRulePath(bad); ok {
			t.Errorf("NormalizeRulePath(%q) должен отклоняться, получено %q", bad, got)
		}
	}
}

func TestParseFolderRulesDropsInvalid(t *testing.T) {
	r := ParseRules([]FolderRule{
		{Path: "Ок", Mode: FolderUp},
		{Path: "Плохой режим", Mode: "странный"},
		{Path: "..", Mode: FolderUp},
		{Path: "", Mode: FolderDown},
		{Path: "ок", Mode: FolderDown}, // дубль по регистру: последний побеждает
	})
	if len(r.List) != 1 {
		t.Fatalf("ожидалось 1 правило, получено %d: %+v", len(r.List), r.List)
	}
	if r.List[0].Mode != FolderDown {
		t.Fatalf("последний дубль должен побеждать: %+v", r.List[0])
	}
}

func TestFolderRulesCovered(t *testing.T) {
	r := ParseRules([]FolderRule{
		{Path: "Фото", Mode: FolderBoth},
		{Path: "Музыка/Рок", Mode: FolderUp},
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
		if got := r.Covered(c.path); got != c.want {
			t.Errorf("Covered(%q) = %v, ожидалось %v", c.path, got, c.want)
		}
	}
}
