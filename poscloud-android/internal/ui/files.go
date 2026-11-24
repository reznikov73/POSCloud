package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	eng "poscloud-engine"

	"poscloud-client-android/internal/client"
)

// filesPage — вкладка «Файлы»: содержимое папки синхронизации деревом, как в
// проводнике. Только чтение: ни настройки, ни файлы не меняются, поэтому
// страница работает при любой папке — в том числе служебной, которую не видит
// файловый менеджер телефона.
type filesPage struct {
	ctrl  *client.Controller
	win   fyne.Window
	root  string // абсолютный путь папки синхронизации
	tree  *widget.Tree
	path  *widget.Label
	state *widget.Label
	rows  map[fyne.CanvasObject]*fileRow // строки дерева: объект → подписи
}

// filesRootUID — корневой узел дерева: сама папка синхронизации.
const filesRootUID = "root"

func newFilesPage(ctrl *client.Controller, win fyne.Window) *filesPage {
	p := &filesPage{ctrl: ctrl, win: win, rows: map[fyne.CanvasObject]*fileRow{}}
	p.path = widget.NewLabel("")
	p.path.Wrapping = fyne.TextWrapBreak
	p.state = caption("")
	p.tree = widget.NewTree(p.childUIDs, p.isBranch, p.createNode, p.updateNode)
	p.tree.Root = filesRootUID
	p.tree.HideSeparators = true
	return p
}

// object собирает страницу: путь, итог, кнопка обновления и само дерево.
func (p *filesPage) object() fyne.CanvasObject {
	top := container.NewVBox(
		heading("Файлы"),
		p.path,
		container.NewBorder(nil, nil, nil,
			widget.NewButtonWithIcon("Обновить", theme.ViewRefreshIcon(), func() { p.Refresh() }),
			p.state,
		),
		widget.NewSeparator(),
	)
	return container.NewBorder(top, nil, nil, nil, p.tree)
}

// Refresh перечитывает папку синхронизации (её путь мог измениться) и дерево.
func (p *filesPage) Refresh() {
	p.root = strings.TrimSpace(p.ctrl.Config().Dir)
	if p.root == "" {
		p.path.SetText("папка синхронизации не выбрана")
		p.state.SetText("")
		p.tree.Refresh()
		return
	}
	p.path.SetText(p.root)
	if fi, err := os.Stat(p.root); err != nil || !fi.IsDir() {
		p.state.SetText("Папки нет — выберите её на вкладке «Связь»")
		p.tree.Refresh()
		return
	}
	n, bytes := eng.DirStats(p.root)
	p.state.SetText(pluralFiles(n) + ", " + humanBytes(bytes) + " · нажмите на папку, чтобы раскрыть")
	p.tree.OpenAllBranches()
	p.tree.Refresh()
}

// absPath переводит узел дерева в путь на диске.
func (p *filesPage) absPath(uid widget.TreeNodeID) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(string(uid), filesRootUID), "/")
	if rel == "" {
		return p.root
	}
	return filepath.Join(p.root, filepath.FromSlash(rel))
}

// childUIDs отдаёт содержимое каталога: сначала папки, потом файлы, по алфавиту.
// Скрытые (служебные) файлы движка не показываем — они не синхронизируются.
func (p *filesPage) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	entries, err := os.ReadDir(p.absPath(uid))
	if err != nil {
		return nil
	}
	var dirs, files []widget.TreeNodeID
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		child := widget.TreeNodeID(string(uid) + "/" + name)
		if e.IsDir() {
			dirs = append(dirs, child)
		} else {
			files = append(files, child)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i] < dirs[j] })
	sort.Slice(files, func(i, j int) bool { return files[i] < files[j] })
	return append(dirs, files...)
}

// isBranch — папка ли это (у неё есть треугольник раскрытия).
func (p *filesPage) isBranch(uid widget.TreeNodeID) bool {
	fi, err := os.Stat(p.absPath(uid))
	return err == nil && fi.IsDir()
}

// fileRow — строка дерева: иконка, имя, справа размер и дата.
type fileRow struct {
	icon *widget.Icon
	name *widget.Label
	info *widget.Label
}

func (p *filesPage) createNode(branch bool) fyne.CanvasObject {
	row := &fileRow{
		icon: widget.NewIcon(theme.FileIcon()),
		name: widget.NewLabel(""),
		info: caption(""),
	}
	row.name.Truncation = fyne.TextTruncateEllipsis
	row.info.SizeName = theme.SizeNameCaptionText
	obj := container.NewBorder(nil, nil, row.icon, nil, container.NewVBox(row.name, row.info))
	p.rows[obj] = row
	return obj
}

func (p *filesPage) updateNode(uid widget.TreeNodeID, branch bool, o fyne.CanvasObject) {
	row, ok := p.rows[o]
	if !ok {
		return
	}
	if string(uid) == filesRootUID {
		row.icon.SetResource(theme.FolderIcon())
		row.name.SetText(filepath.Base(p.root))
		row.info.SetText("")
		return
	}
	row.name.SetText(shortenName(nameOf(uid)))
	if branch {
		row.icon.SetResource(theme.FolderIcon())
		row.info.SetText("")
		return
	}
	row.icon.SetResource(theme.FileIcon())
	info := ""
	if fi, err := os.Stat(p.absPath(uid)); err == nil {
		info = humanBytes(fi.Size())
		if t := fi.ModTime(); !t.IsZero() {
			info += " · " + t.Format("02.01 15:04")
		}
	}
	row.info.SetText(info)
}

// nameLimit — сколько знаков имени показываем: на телефоне строка узкая,
// а длинные имена снимков иначе вылезали бы за край.
const nameLimit = 40

// shortenName сжимает длинное имя, оставляя начало и конец (дата и расширение).
func shortenName(name string) string {
	r := []rune(name)
	if len(r) <= nameLimit {
		return name
	}
	head := nameLimit/2 + 4
	tail := nameLimit - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// nameOf — имя файла или папки по узлу дерева.
func nameOf(uid widget.TreeNodeID) string {
	s := strings.TrimSuffix(string(uid), "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return "/"
	}
	return s
}
