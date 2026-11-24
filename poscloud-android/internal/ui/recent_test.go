package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"poscloud-client-android/internal/client"
)

// TestPluralChanges — склонение «изменение»: 1 изменение, 2 изменения, 5 изменений.
func TestPluralChanges(t *testing.T) {
	cases := map[int]string{
		0:  "0 изменений",
		1:  "1 изменение",
		2:  "2 изменения",
		5:  "5 изменений",
		11: "11 изменений",
		21: "21 изменение",
	}
	for n, want := range cases {
		if got := pluralChanges(n); got != want {
			t.Errorf("pluralChanges(%d) = %q, хочу %q", n, got, want)
		}
	}
}

// TestRecentPageEmpty — пустая лента показывает «пока ничего» и не падает.
func TestRecentPageEmpty(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	ctrl := client.NewController(t.TempDir())
	p := newRecentPage(ctrl, w)
	p.Refresh()
	if p.state.Text != "Пока ничего не менялось" {
		t.Fatalf("подпись: %q", p.state.Text)
	}
	if len(p.box.Objects) != 0 {
		t.Fatalf("строк в пустой ленте: %d", len(p.box.Objects))
	}
	w.Close()
}

// TestRecentPageFromHistory — лента читает историю из events.json
// (контроллер читает файл при создании) и строит строки, новые сверху.
func TestRecentPageFromHistory(t *testing.T) {
	root := t.TempDir()
	events := `{"events":[` +
		`{"name":"папка/первый.txt","op":"down","time":1700000000},` +
		`{"name":"второй.txt","op":"up","time":1700000100},` +
		`{"name":"третий.txt","op":"delete","time":1700000200}` +
		`]}`
	if err := os.WriteFile(filepath.Join(root, "events.json"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	ctrl := client.NewController(root)
	p := newRecentPage(ctrl, w)
	p.Refresh()

	if !strings.Contains(p.state.Text, "3 изменения") {
		t.Fatalf("итог: %q", p.state.Text)
	}
	if len(p.box.Objects) != 3 {
		t.Fatalf("строк: %d", len(p.box.Objects))
	}
	list := ctrl.RecentEvents()
	if len(list) != 3 || list[2].Name != "третий.txt" || list[2].Op != "delete" {
		t.Fatalf("история: %+v", list)
	}
	w.Close()
}

// TestFilesPageSearch — поиск по имени: находки из свёрнутых подпапок,
// плоский список, без учёта регистра.
func TestFilesPageSearch(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	root := t.TempDir()
	for _, dir := range []string{"beta", "alpha"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]int{
		"b.txt":        3,
		"a.txt":        2,
		"beta/ab.md":   5,
		"alpha/BA.doc": 7,
		"zzz.txt":      4,
	}
	for name, size := range files {
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := client.NewController(t.TempDir())
	ctrl.SaveSettings("localhost", 8090, root, 3, "t", false, "", "", true, "", "", 0)
	p := newFilesPage(ctrl, w)
	p.Refresh()

	// без поиска — обычное дерево (две папки и три файла)
	if got := len(p.childUIDs(filesRootUID)); got != 5 {
		t.Fatalf("без поиска детей корня: %d, ожидалось 5", got)
	}

	// текст ставим напрямую, без OnChanged: он планирует обход по таймеру,
	// а здесь проверяем логику поиска синхронно
	p.search.Text = "a"
	if !p.filtering() {
		t.Fatal("фильтр должен считаться активным")
	}
	// до готовности поиска дерево показывает «Ищем…», а не пустоту
	p.searchMu.Lock()
	p.searchLoading = true
	p.searchMu.Unlock()
	if len(p.childUIDs(filesRootUID)) != 0 {
		t.Fatal("пока идёт поиск, список должен быть пуст")
	}

	q := strings.ToLower(strings.TrimSpace(p.search.Text))
	if !p.doSearch(q, root, p.searchGen) {
		t.Fatal("актуальный запрос не должен отбрасываться")
	}
	kids := p.childUIDs(filesRootUID)
	want := []string{"root/a.txt", "root/alpha/BA.doc", "root/beta/ab.md"}
	if len(kids) != len(want) {
		t.Fatalf("находки: %v, ожидалось %v", kids, want)
	}
	for i := range want {
		if string(kids[i]) != want[i] {
			t.Fatalf("находки: %v, ожидалось %v", kids, want)
		}
	}
	if p.isBranch("root/a.txt") {
		t.Fatal("находка не должна быть веткой")
	}
	if !p.isBranch(filesRootUID) {
		t.Fatal("корень должен оставаться веткой")
	}
	if got := p.childUIDs("root/a.txt"); len(got) != 0 {
		t.Fatalf("у находки не должно быть детей: %v", got)
	}

	// устаревший запрос (человек допечатал) не перезаписывает свежий кеш
	if p.doSearch("zzz", root, p.searchGen-1) {
		t.Fatal("устаревший запрос должен отбрасываться")
	}
	if got := p.childUIDs(filesRootUID); len(got) != 3 {
		t.Fatalf("кеш перезаписан устаревшим запросом: %v", got)
	}

	// чистая функция: регистр не важен, скрытые пропускаются, лимит работает
	if got := walkMatches(root, "A", 300); len(got) != 3 {
		t.Fatalf("walkMatches: %v", got)
	}
	if got := walkMatches(root, "a", 2); len(got) != 2 {
		t.Fatalf("лимит: %v", got)
	}
	if got := walkMatches(root, "poscloud-state", 300); len(got) != 0 {
		t.Fatalf("скрытые файлы не должны находиться: %v", got)
	}

	// очистка поля возвращает обычное дерево
	p.search.Text = ""
	p.scheduleSearch()
	if p.filtering() || len(p.searchResults()) != 0 {
		t.Fatal("после очистки кеш должен быть пуст")
	}
	if got := len(p.childUIDs(filesRootUID)); got != 5 {
		t.Fatalf("после очистки детей корня: %d, ожидалось 5", got)
	}
	w.Close()
}
