package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"poscloud-client-android/internal/client"
)

// TestPluralConflicts — склонение «конфликт»: 1 конфликт, 2 конфликта, 5 конфликтов.
func TestPluralConflicts(t *testing.T) {
	cases := map[int]string{
		0:  "0 конфликтов",
		1:  "1 конфликт",
		2:  "2 конфликта",
		5:  "5 конфликтов",
		11: "11 конфликтов",
		14: "14 конфликтов",
		21: "21 конфликт",
	}
	for n, want := range cases {
		if got := pluralConflicts(n); got != want {
			t.Errorf("pluralConflicts(%d) = %q, хочу %q", n, got, want)
		}
	}
}

// TestConflictsCardRefresh — карточка на «Файлах»: без конфликтов пишет «нет»
// и прячет кнопку, с конфликтом — сколько копий и какого объёма.
func TestConflictsCardRefresh(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	root := t.TempDir()
	ctrl := client.NewController(t.TempDir())
	ctrl.SaveSettings("localhost", 8090, root, 3, "t", false, "", "", true, "", "", 0)

	p := newFilesPage(ctrl, w)
	c := p.conflicts
	c.Refresh()
	if c.label.Text != "Конфликтов нет" {
		t.Fatalf("пустая папка: подпись %q", c.label.Text)
	}
	if !c.button.Hidden {
		t.Fatal("без конфликтов кнопка должна быть спрятана")
	}

	cf := "документ (конфликт 2026-11-24_10-00-00).txt"
	if err := os.WriteFile(filepath.Join(root, cf), []byte("my edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "документ.txt"), []byte("server"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.Refresh()
	if !strings.Contains(c.label.Text, "1 конфликт (7 Б)") {
		t.Fatalf("подпись: %q", c.label.Text)
	}
	if c.button.Hidden {
		t.Fatal("с конфликтом кнопка должна быть видна")
	}
	w.Close()
}
