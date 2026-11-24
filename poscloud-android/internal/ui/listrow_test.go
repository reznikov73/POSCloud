package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"poscloud-client-android/internal/client"
)

// TestListRowEllipsizes — длинное название не вылезает за строку: в Fyne
// Label не обрезает уже разложенный текст, поэтому строку режем сами.
func TestListRowEllipsizes(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(Theme())

	row := newListRow(nil)
	row.set(nil, strings.Repeat("Очень длинное имя ", 8)+".apk", "получен · 24.11.2026 19:20")
	r := row.CreateRenderer()
	r.Layout(fyne.NewSize(300, row.MinSize().Height))

	if row.title.Text == row.fullTitle {
		t.Fatalf("название не обрезано: %q", row.title.Text)
	}
	if !strings.Contains(row.title.Text, "…") {
		t.Fatalf("нет многоточия: %q", row.title.Text)
	}
	if w := fyne.MeasureText(row.title.Text, 15, fyne.TextStyle{}).Width; w > 300 {
		t.Fatalf("текст шире строки: %.0f > 300", w)
	}
}

// TestSearchGroupsByFolder — результаты поиска группируются по папкам:
// в строке — только имя файла и размер, без относительного пути. Раньше
// путь стоял в строке, она становилась шире экрана и обрезалась краем окна.
func TestSearchGroupsByFolder(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(Theme())
	w := a.NewWindow("x")

	root := t.TempDir()
	dir := filepath.Join(root, "Снимки экрана")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "IMG_0001.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctrl := client.NewController(t.TempDir())
	ctrl.SaveSettings("localhost", 8090, root, 3, "t", false, "", "", true, "", "", 0)
	p := newFilesPage(ctrl, w)
	p.root = root

	uid := filesRootUID + "/Снимки экрана/IMG_0001.png"
	p.search.SetText("IMG")
	p.searchMu.Lock()
	p.searchLoading = false
	p.results = []widget.TreeNodeID{uid}
	p.searchMu.Unlock()

	obj := p.createNode(false)
	node, ok := obj.(*fileNode)
	if !ok {
		t.Fatalf("строка дерева не fileNode: %T", obj)
	}
	p.updateNode(uid, false, obj)
	if node.name.Text != "IMG_0001.png" {
		t.Fatalf("в строке %q, ожидалось имя файла", node.name.Text)
	}
	if node.info.Text == "Снимки экрана" {
		t.Fatalf("в подписи путь, ожидался размер: %q", node.info.Text)
	}

	// в корне поиска — папка-группа, внутри неё — файл
	kids := p.searchChildUIDs(filesRootUID)
	if len(kids) != 1 || string(kids[0]) != filesRootUID+"/Снимки экрана" {
		t.Fatalf("группы поиска: %v", kids)
	}
	if !p.isBranch(kids[0]) {
		t.Fatalf("папка-группа не считается веткой: %v", kids[0])
	}
	if got := p.searchChildUIDs(kids[0]); len(got) != 1 || string(got[0]) != uid {
		t.Fatalf("содержимое группы: %v", got)
	}

	folderObj := p.createNode(true)
	folderNode, _ := folderObj.(*fileNode)
	p.updateNode(kids[0], true, folderObj)
	if folderNode.name.Text != "Снимки экрана" || folderNode.info.Text != "1" {
		t.Fatalf("строка группы: %q, %q", folderNode.name.Text, folderNode.info.Text)
	}

	p.updateState()
	if got := p.state.Text; !strings.Contains(got, "Найдено: 1") {
		t.Fatalf("строка поиска: %q", got)
	}
}
