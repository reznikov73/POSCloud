package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Включение набора добавляет блок и делает его видимым для интерфейса.
func TestApplyIgnorePresetOn(t *testing.T) {
	a := newTestApp(t, "")
	text := a.ApplyIgnorePreset("", "logs", true)

	for _, want := range []string{"# preset: logs", "*.log", "# /preset: logs"} {
		if !strings.Contains(text, want) {
			t.Fatalf("в тексте нет %q:\n%s", want, text)
		}
	}
	got := a.AppliedIgnorePresets(text)
	if len(got) != 1 || got[0] != "logs" {
		t.Fatalf("состояние набора определено неверно: %v", got)
	}
	if again := a.ApplyIgnorePreset(text, "logs", true); again != text {
		t.Fatalf("повторное включение изменило текст:\n%q\n%q", text, again)
	}
}

// Выключение набора удаляет только его блок и не трогает чужие строки.
func TestApplyIgnorePresetOff(t *testing.T) {
	a := newTestApp(t, "")
	text := a.ApplyIgnorePreset("# мои шаблоны\nnotes", "logs", true)
	text = a.AddIgnorePatterns(text, []string{"keep-me"})

	off := a.ApplyIgnorePreset(text, "logs", false)
	if strings.Contains(off, "preset") || strings.Contains(off, "*.log") {
		t.Fatalf("блок набора не удалён:\n%s", off)
	}
	if !strings.Contains(off, "notes") || !strings.Contains(off, "keep-me") || !strings.Contains(off, "# мои шаблоны") {
		t.Fatalf("пострадали чужие строки:\n%s", off)
	}
	if len(a.AppliedIgnorePresets(off)) != 0 {
		t.Fatal("набор всё ещё считается включённым")
	}
}

// Повреждённый блок (нет закрывающего маркера) не должен приводить к удалению строк.
func TestApplyIgnorePresetBrokenBlockKeepsText(t *testing.T) {
	a := newTestApp(t, "")
	text := "# preset: logs\n*.log\nважное\nещё"
	if got := a.ApplyIgnorePreset(text, "logs", false); got != text {
		t.Fatalf("повреждённый блок удалил строки:\n%q\n%q", text, got)
	}
}

// Неизвестный идентификатор набора ничего не меняет.
func TestApplyIgnorePresetUnknownID(t *testing.T) {
	a := newTestApp(t, "")
	if got := a.ApplyIgnorePreset("a\nb", "нет-такого", true); got != "a\nb" {
		t.Fatalf("неизвестный набор изменил текст: %q", got)
	}
}

// Добавление шаблонов не создаёт дублей.
func TestAddIgnorePatternsNoDuplicates(t *testing.T) {
	a := newTestApp(t, "")
	text := a.AddIgnorePatterns("build\n*.log", []string{"*.log", "temp", "", "temp", "  "})
	counts := map[string]int{}
	for _, l := range strings.Split(text, "\n") {
		counts[l]++
	}
	for _, l := range []string{"build", "*.log", "temp"} {
		if counts[l] != 1 {
			t.Fatalf("строка %q встречается %d раз(а):\n%s", l, counts[l], text)
		}
	}
	if strings.Contains(text, "\n\n") {
		t.Fatalf("появились пустые строки:\n%s", text)
	}
}

// Каталог готовых наборов отдаётся интерфейсу целиком.
func TestIgnorePresetsCatalog(t *testing.T) {
	a := newTestApp(t, "")
	list := a.IgnorePresets()
	if len(list) != 7 {
		t.Fatalf("наборов %d, ожидалось 7", len(list))
	}
	for _, p := range list {
		if p.ID == "" || p.Title == "" || len(p.Patterns) == 0 {
			t.Fatalf("неполный набор: %+v", p)
		}
	}
}

// Предпросмотр считает по тем же правилам, что и синхронизация:
// служебные записи (с точкой) не учитываются, файл внутри исключённой
// папки тоже считается исключённым.
func TestIgnorePreview(t *testing.T) {
	a := newTestApp(t, "")
	dir := t.TempDir()
	a.localDir = dir

	write := func(rel string, size int) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("keep/a.txt", 10)
	write("build/out.bin", 100)
	write("build/sub/deep.bin", 200)
	write("logs/app.log", 5)
	write(".hidden/skip.txt", 50)

	st := a.IgnorePreview("build\n*.log")
	if st.Total != 4 {
		t.Fatalf("Total=%d, ожидалось 4 (служебный .hidden не считается)", st.Total)
	}
	if st.Matched != 3 {
		t.Fatalf("Matched=%d, ожидалось 3 (build/* и logs/app.log)", st.Matched)
	}
	if st.MatchedBytes != 305 || st.TotalBytes != 315 {
		t.Fatalf("байты посчитаны неверно: %+v", st)
	}

	empty := a.IgnorePreview("")
	if empty.Matched != 0 || empty.Total != 4 {
		t.Fatalf("без исключений ожидалось 0 из 4, получено %+v", empty)
	}
}

// Список содержимого папки синхронизации: каталоги первыми, служебные скрыты,
// выход за пределы папки запрещён.
func TestListSyncEntries(t *testing.T) {
	a := newTestApp(t, "")
	dir := t.TempDir()
	a.localDir = dir

	if err := os.MkdirAll(filepath.Join(dir, "Docs", "inner"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	top := a.ListSyncEntries("")
	if len(top) != 2 {
		t.Fatalf("записей %d, ожидалось 2: %+v", len(top), top)
	}
	if !top[0].IsDir || top[0].Name != "Docs" {
		t.Fatalf("каталоги должны идти первыми: %+v", top)
	}
	if top[1].Name != "a.txt" || top[1].Path != "a.txt" {
		t.Fatalf("файл разобран неверно: %+v", top[1])
	}

	inner := a.ListSyncEntries("Docs")
	if len(inner) != 1 || inner[0].Path != "Docs/inner" {
		t.Fatalf("вложенный каталог разобран неверно: %+v", inner)
	}

	for _, bad := range []string{"..", "../..", "Docs/../..", "C:/Windows", `..\..`, "/etc"} {
		if got := a.ListSyncEntries(bad); len(got) != 0 {
			t.Fatalf("путь %q не отклонён: %+v", bad, got)
		}
	}
}
