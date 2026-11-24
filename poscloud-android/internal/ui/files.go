package ui

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	eng "poscloud-engine"

	"poscloud-client-android/internal/camera"
	"poscloud-client-android/internal/client"
)

// filesPage — вкладка «Файлы»: папка синхронизации деревом, как в проводнике.
// Можно раскрывать папки, открывать файлы системными приложениями, копировать,
// переименовывать и удалять — сами настройки синхронизации при этом не меняются.
type filesPage struct {
	ctrl      *client.Controller
	win       fyne.Window
	root      string // абсолютный путь папки синхронизации
	tree      *widget.Tree
	path      *widget.Label
	state     *widget.Label
	search    *widget.Entry // поиск по имени: дерево становится плоским списком находок
	conflicts *conflictsCard
	nodes     map[fyne.CanvasObject]*fileNode // строки дерева: объект → подписи

	searchMu      sync.Mutex // кеш поиска: читает горутина, читает UI-поток
	results       []widget.TreeNodeID
	searchGen     int         // поколение запроса: устаревший результат отбрасывается
	searchTimer   *time.Timer // debounce ввода
	searchLoading bool        // идёт обход папки в фоне
}

// filesRootUID — корневой узел дерева: сама папка синхронизации.
const filesRootUID = "root"

func newFilesPage(ctrl *client.Controller, win fyne.Window) *filesPage {
	p := &filesPage{ctrl: ctrl, win: win, nodes: map[fyne.CanvasObject]*fileNode{}}
	p.path = widget.NewLabel("")
	p.path.Wrapping = fyne.TextWrapBreak
	p.path.SizeName = theme.SizeNameCaptionText
	p.path.TextStyle = fyne.TextStyle{Monospace: true}
	p.state = caption("")
	p.search = widget.NewEntry()
	p.search.SetPlaceHolder("Поиск по имени…")
	// Обход папки — в горутине и с debounce: колбэки дерева Fyne вызываются
	// на UI-потоке, и WalkDir по тысячам файлов прямо в childUIDs замораживал
	// бы экран на каждое нажатие клавиши.
	p.search.OnChanged = func(string) { p.scheduleSearch() }
	p.conflicts = newConflictsCard(ctrl, p, win)
	p.tree = widget.NewTree(p.childUIDs, p.isBranch, p.createNode, p.updateNode)
	p.tree.Root = filesRootUID
	p.tree.HideSeparators = true
	return p
}

// object собирает страницу: путь, поиск, итог, карточка конфликтов, кнопка
// обновления и само дерево.
func (p *filesPage) object() fyne.CanvasObject {
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { p.Refresh() })
	refresh.Importance = widget.LowImportance
	top := container.NewVBox(
		pageHeader("Файлы", refresh),
		p.path,
		fieldFill(p.search),
		p.state,
		p.conflicts.object(),
		widget.NewSeparator(),
	)
	return container.NewBorder(top, nil, nil, nil, p.tree)
}

// Refresh перечитывает папку синхронизации (её путь мог измениться), дерево
// и список конфликтных копий.
func (p *filesPage) Refresh() {
	p.root = strings.TrimSpace(p.ctrl.Config().Dir)
	p.conflicts.Refresh()
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
	p.updateState()
	// при активном поиске папка могла измениться — перезапускаем обход,
	// иначе дерево покажет устаревший кеш
	if p.filtering() {
		p.scheduleSearch()
	}
	p.tree.Refresh()
}

// updateState — строка под поиском: без поиска размер папки, с поиском —
// ход обхода или число находок (раньше там всегда висел размер всей папки).
func (p *filesPage) updateState() {
	if p.root == "" {
		p.state.SetText("")
		return
	}
	if p.filtering() {
		p.searchMu.Lock()
		loading, n := p.searchLoading, len(p.results)
		p.searchMu.Unlock()
		switch {
		case loading:
			p.state.SetText("Ищем…")
		case n == 0:
			p.state.SetText("Ничего не найдено — измените запрос")
		default:
			p.state.SetText(fmt.Sprintf("Найдено: %d · нажмите на файл для действий", n))
		}
		return
	}
	n, bytes := eng.DirStats(p.root)
	p.state.SetText(pluralFiles(n) + ", " + humanBytes(bytes) + " · нажмите на файл для действий")
}

// absPath переводит узел дерева в путь на диске.
func (p *filesPage) absPath(uid widget.TreeNodeID) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(string(uid), filesRootUID), "/")
	if rel == "" {
		return p.root
	}
	return filepath.Join(p.root, filepath.FromSlash(rel))
}

// filtering — идёт ли поиск по имени: дерево показывает плоский список находок.
func (p *filesPage) filtering() bool {
	return p.root != "" && strings.TrimSpace(p.search.Text) != ""
}

// searchDebounce — пауза после нажатия клавиши, прежде чем идти по диску.
const searchDebounce = 300 * time.Millisecond

// scheduleSearch перезапускает поиск с debounce: пока человек печатает,
// по диску не ходим. Вызывается на UI-потоке: текст поля и путь папки
// снимаем здесь и передаём в горутину значениями.
func (p *filesPage) scheduleSearch() {
	if p.searchTimer != nil {
		p.searchTimer.Stop()
	}
	if !p.filtering() {
		// фильтр очищен — дерево сразу возвращается к обычному виду
		p.searchMu.Lock()
		p.results = nil
		p.searchGen++
		p.searchLoading = false
		p.searchMu.Unlock()
		p.tree.Refresh()
		p.updateState()
		return
	}
	q := strings.ToLower(strings.TrimSpace(p.search.Text))
	root := p.root
	p.searchTimer = time.AfterFunc(searchDebounce, func() { p.runSearch(q, root) })
}

// runSearch обходит папку в горутине и кладёт результат в кеш; tree.Refresh
// уже не читает диск, а берёт готовый список. Результат устаревшего запроса
// (человек успел допечатать) отбрасывается по номеру поколения.
func (p *filesPage) runSearch(q, root string) {
	if q == "" || root == "" {
		return
	}
	p.searchMu.Lock()
	p.searchGen++
	gen := p.searchGen
	p.searchLoading = true
	p.searchMu.Unlock()
	go func() {
		p.doSearch(q, root, gen)
		fyne.Do(func() {
			p.tree.OpenAllBranches()
			p.tree.Refresh()
			p.updateState()
		})
	}()
}

// doSearch — само выполнение поиска (синхронно): вызывается из горутины,
// а в тестах — напрямую. Возвращает false, если запрос устарел.
func (p *filesPage) doSearch(q, root string, gen int) bool {
	found := walkMatches(root, q, 300)
	p.searchMu.Lock()
	defer p.searchMu.Unlock()
	if gen != p.searchGen {
		return false
	}
	p.results = found
	p.searchLoading = false
	return true
}

// walkMatches ищет файлы по подстроке имени (без учёта регистра) во всей
// папке, включая свёрнутые, и возвращает узлы дерева для плоского списка.
// Чистая функция без состояния — вызывается вне UI-потока.
func walkMatches(root, q string, limit int) []widget.TreeNodeID {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" || root == "" {
		return nil
	}
	var out []widget.TreeNodeID
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !strings.Contains(strings.ToLower(d.Name()), q) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		out = append(out, widget.TreeNodeID(filesRootUID+"/"+filepath.ToSlash(rel)))
		if len(out) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// searchResults возвращает кеш последнего поиска (мгновенно, без диска).
func (p *filesPage) searchResults() []widget.TreeNodeID {
	p.searchMu.Lock()
	defer p.searchMu.Unlock()
	return append([]widget.TreeNodeID(nil), p.results...)
}

// searchChildUIDs отдаёт находки, сгруппированные по папкам: в корне —
// папки с находками (и файлы из самого корня), внутри папки — её находки.
// Плоский список с относительным путём в строке не помещался по ширине:
// строка становилась шире экрана и обрезалась краем окна.
func (p *filesPage) searchChildUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	res := p.searchResults()
	var out []widget.TreeNodeID
	if string(uid) == filesRootUID {
		seen := map[string]bool{}
		for _, f := range res {
			d := path.Dir(string(f))
			if d == string(uid) {
				out = append(out, f)
				continue
			}
			if !seen[d] {
				seen[d] = true
				out = append(out, widget.TreeNodeID(d))
			}
		}
		return out
	}
	for _, f := range res {
		if path.Dir(string(f)) == string(uid) {
			out = append(out, f)
		}
	}
	return out
}

// childUIDs отдаёт содержимое каталога: сначала папки, потом файлы, по алфавиту.
// Во время поиска — находки, сгруппированные по папкам.
func (p *filesPage) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	if p.filtering() {
		return p.searchChildUIDs(uid)
	}
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
// Во время поиска ветка — корень и папки с находками.
func (p *filesPage) isBranch(uid widget.TreeNodeID) bool {
	if p.filtering() {
		if string(uid) == filesRootUID {
			return true
		}
		// папка-группа в результатах поиска
		for _, f := range p.searchResults() {
			if path.Dir(string(f)) == string(uid) {
				return true
			}
		}
		return false
	}
	fi, err := os.Stat(p.absPath(uid))
	return err == nil && fi.IsDir()
}

func (p *filesPage) createNode(branch bool) fyne.CanvasObject {
	n := newFileNode(nil)
	n.onTap = func() { p.actions(n.uid) }
	p.nodes[n] = n
	return n
}

func (p *filesPage) updateNode(uid widget.TreeNodeID, branch bool, o fyne.CanvasObject) {
	n, ok := p.nodes[o]
	if !ok {
		return
	}
	n.uid = uid
	abs := p.absPath(uid)
	if string(uid) == filesRootUID {
		if p.filtering() {
			p.searchMu.Lock()
			loading := p.searchLoading
			p.searchMu.Unlock()
			if loading {
				n.set("Ищем…", true, "")
				return
			}
			n.set("Результаты поиска", true, fmt.Sprintf("%d", len(p.searchResults())))
		} else {
			n.set(filepath.Base(p.root), true, "")
		}
		return
	}
	if p.filtering() {
		base := filepath.Base(abs)
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			n.set(base, true, fmt.Sprintf("%d", len(p.searchChildUIDs(uid))))
			return
		}
		// Размер в результатах не показываем: дерево Fyne отдаёт строке
		// ширину больше видимой, и правый столбец обрезался краем экрана.
		n.set(clipMiddle(base, 34), false, "")
		return
	}
	fi, err := os.Stat(abs)
	if err != nil {
		n.set(nameOf(uid), branch, "")
		return
	}
	info := ""
	if !branch {
		info = humanBytes(fi.Size())
	}
	n.set(nameOf(uid), branch, info)
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

// ---------- строка дерева ----------

// fileNode — компактная строка дерева: иконка, имя (обрезается по ширине) и
// размер. Имя режем сами по измеренной ширине: Label в Fyne не обрезает текст,
// если размер уже разложен, и длинные имена налезали на соседние подписи.
type fileNode struct {
	widget.BaseWidget
	uid      widget.TreeNodeID
	icon     *widget.Icon
	name     *widget.Label
	info     *widget.Label
	full     string
	fullInfo string
	onTap    func()
}

const (
	nodePad  = 8
	nodeIcon = 20
)

func newFileNode(onTap func()) *fileNode {
	n := &fileNode{
		icon:  widget.NewIcon(theme.FileIcon()),
		name:  widget.NewLabel(""),
		info:  caption(""),
		onTap: onTap,
	}
	n.name.TextStyle = fyne.TextStyle{}
	n.info.SizeName = theme.SizeNameCaptionText
	n.info.Alignment = fyne.TextAlignTrailing
	// Подпись справа не переносится: при узком месте перенос превращал
	// путь папки в три строки поверх имени файла (видно при поиске).
	n.info.Wrapping = fyne.TextWrapOff
	n.ExtendBaseWidget(n)
	return n
}

// set обновляет строку (full — полное имя, его обрезаем уже при раскладке).
func (n *fileNode) set(name string, folder bool, info string) {
	if folder {
		n.icon.SetResource(theme.FolderIcon())
	} else {
		n.icon.SetResource(theme.FileIcon())
	}
	n.full = name
	n.fullInfo = info
	n.name.SetText(name)
	n.info.SetText(info)
}

// Tapped — касание по строке: открываем меню действий.
func (n *fileNode) Tapped(*fyne.PointEvent) {
	if n.onTap != nil {
		n.onTap()
	}
}

func (n *fileNode) MinSize() fyne.Size {
	th := theme.CurrentForWidget(n)
	h := fyne.Max(nodeIcon, th.Size(theme.SizeNameText)) + 2*nodePad
	if h < rowMin {
		h = rowMin
	}
	return fyne.NewSize(0, h)
}

func (n *fileNode) CreateRenderer() fyne.WidgetRenderer {
	r := &fileNodeRenderer{obj: n, objs: []fyne.CanvasObject{n.icon, n.name, n.info}}
	r.Layout(n.MinSize())
	return r
}

type fileNodeRenderer struct {
	obj  *fileNode
	objs []fyne.CanvasObject
}

func (r *fileNodeRenderer) Destroy()                     {}
func (r *fileNodeRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *fileNodeRenderer) MinSize() fyne.Size           { return r.obj.MinSize() }

// Refresh пересчитывает раскладку: дерево Fyne переиспользует строки и после
// updateNode может не звать Layout заново, а без него имя оставалось полным
// и налезало на размер справа (видно в результатах поиска).
func (r *fileNodeRenderer) Refresh() {
	r.Layout(r.obj.Size())
	canvas.Refresh(r.obj)
}

// Layout: иконка слева, размер справа, имя — в остатке ширины с обрезкой.
func (r *fileNodeRenderer) Layout(size fyne.Size) {
	n := r.obj
	iconSize := theme.CurrentForWidget(n).Size(theme.SizeNameInlineIcon)
	if iconSize > nodeIcon {
		iconSize = nodeIcon
	}
	n.icon.Resize(fyne.NewSize(iconSize, iconSize))
	n.icon.Move(fyne.NewPos(0, (size.Height-iconSize)/2))

	infoW := float32(0)
	if n.fullInfo != "" {
		captionSize := theme.CurrentForWidget(n).Size(theme.SizeNameCaptionText)
		infoW = fyne.MeasureText(n.fullInfo, captionSize, fyne.TextStyle{}).Width + 8
		if infoW > size.Width*0.45 {
			infoW = size.Width * 0.45
		}
		setTextIfChanged(n.info, ellipsize(n.fullInfo, infoW, captionSize))
		n.info.Resize(fyne.NewSize(infoW, n.info.MinSize().Height))
		n.info.Move(fyne.NewPos(size.Width-infoW, (size.Height-n.info.MinSize().Height)/2))
	}

	left := iconSize + nodePad
	nameW := size.Width - left - infoW - nodePad*2
	if nameW < 0 {
		nameW = 0
	}
	text := ellipsize(n.full, nameW-6, theme.CurrentForWidget(n).Size(theme.SizeNameText))
	if n.name.Text != text {
		n.name.SetText(text)
	}
	nameH := n.name.MinSize().Height
	n.name.Resize(fyne.NewSize(nameW, nameH))
	n.name.Move(fyne.NewPos(left, (size.Height-nameH)/2))
}

// clipMiddle сжимает строку до max символов, оставляя начало и конец.
// Нужна там, где ширина ещё неизвестна (строка дерева переиспользуется
// Fyne): текст получает безопасную длину сразу, до раскладки.
func clipMiddle(s string, max int) string {
	r := []rune(s)
	if max < 5 || len(r) <= max {
		return s
	}
	head := max / 2
	tail := max - head - 1
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// ellipsize сжимает текст до ширины, оставляя начало и конец: у снимков
// различие как раз в конце имени (дата и подпись).
func ellipsize(text string, width, fontSize float32) string {
	if width <= 0 {
		return ""
	}
	if fyne.MeasureText(text, fontSize, fyne.TextStyle{}).Width <= width {
		return text
	}
	r := []rune(text)
	tail := len(r) / 3
	if tail > 12 {
		tail = 12
	}
	if tail < 4 && len(r) > tail {
		tail = 4
	}
	for head := len(r) - tail; head >= 0; head-- {
		cand := string(r[:head]) + "…" + string(r[len(r)-tail:])
		if fyne.MeasureText(cand, fontSize, fyne.TextStyle{}).Width <= width {
			return cand
		}
	}
	return "…"
}

// ---------- действия с файлом ----------

// actions показывает меню действий для узла дерева.
func (p *filesPage) actions(uid widget.TreeNodeID) {
	if uid == "" || string(uid) == filesRootUID {
		return
	}
	abs := p.absPath(uid)
	fi, err := os.Stat(abs)
	if err != nil {
		dialog.ShowError(err, p.win)
		return
	}
	name := nameOf(uid)
	var d *dialog.CustomDialog
	rows := container.NewVBox()

	if !fi.IsDir() {
		openTitle := "Открыть"
		mime := camera.MimeFor(abs)
		if mime == "application/vnd.android.package-archive" {
			openTitle = "Установить"
		}
		if mime != "" {
			rows.Add(dialogRow(openTitle, theme.FileIcon(), func() {
				d.Hide()
				if err := camera.OpenFile(abs); err != nil {
					dialog.ShowError(err, p.win)
				}
			}))
		}
	}
	rows.Add(dialogRow("Копировать в…", theme.ContentCopyIcon(), func() {
		d.Hide()
		p.copyTo(abs)
	}))
	rows.Add(dialogRow("Переименовать", theme.DocumentCreateIcon(), func() {
		d.Hide()
		p.rename(abs)
	}))
	rows.Add(dialogRow("Удалить", theme.DeleteIcon(), func() {
		d.Hide()
		p.remove(abs, fi.IsDir())
	}))
	rows.Add(dialogRow("Свойства", theme.InfoIcon(), func() {
		d.Hide()
		p.properties(abs, fi)
	}))

	d = dialog.NewCustom(name, "Закрыть", rows, p.win)
	d.Show()
}

// copyTo копирует файл или папку в выбранный каталог.
func (p *filesPage) copyTo(src string) {
	b := newFolderBrowser(p.ctrl, p.root, p.win)
	d := dialog.NewCustom("Куда копировать", "Отмена", b.object(), p.win)
	d.SetButtons([]fyne.CanvasObject{
		widget.NewButton("Отмена", func() { d.Hide() }),
		widget.NewButtonWithIcon("Копировать сюда", theme.ConfirmIcon(), func() {
			dst := b.current()
			d.Hide()
			go func() {
				target, err := copyItem(src, dst)
				fyne.Do(func() {
					if err != nil {
						dialog.ShowError(err, p.win)
						return
					}
					p.Refresh()
					dialog.ShowInformation("Скопировано", filepath.Base(target), p.win)
				})
			}()
		}),
	})
	d.Resize(dialogSize(p.win))
	d.Show()
}

// copyItem копирует файл или каталог; при совпадении имени добавляет «(копия N)».
func copyItem(src, dstDir string) (string, error) {
	name := filepath.Base(src)
	target := filepath.Join(dstDir, name)
	if _, err := os.Stat(target); err == nil {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		for i := 1; i < 100; i++ {
			target = filepath.Join(dstDir, fmt.Sprintf("%s (копия %d)%s", base, i, ext))
			if _, err := os.Stat(target); os.IsNotExist(err) {
				break
			}
		}
	}
	if target == src {
		return target, fmt.Errorf("файл уже лежит в этой папке")
	}
	if fi, err := os.Stat(src); err == nil && fi.IsDir() {
		return target, copyTree(src, target)
	}
	return target, copyOne(src, target)
}

func copyOne(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyOne(path, target)
	})
}

// rename переименовывает файл или папку в том же каталоге.
func (p *filesPage) rename(abs string) {
	entry := widget.NewEntry()
	entry.SetText(filepath.Base(abs))
	d := dialog.NewCustomConfirm("Переименовать", "Переименовать", "Отмена",
		container.NewVBox(caption("Текущее имя: "+filepath.Base(abs)), entry), func(ok bool) {
			if !ok {
				return
			}
			newName := strings.TrimSpace(entry.Text)
			if newName == "" || strings.ContainsAny(newName, `/\`) {
				dialog.ShowError(fmt.Errorf("введите имя без «/»"), p.win)
				return
			}
			target := filepath.Join(filepath.Dir(abs), newName)
			if target == abs {
				return
			}
			if _, err := os.Stat(target); err == nil {
				dialog.ShowError(fmt.Errorf("такое имя уже есть"), p.win)
				return
			}
			if err := moveFile(abs, target); err != nil {
				dialog.ShowError(err, p.win)
				return
			}
			p.Refresh()
		}, p.win)
	d.Resize(dialogSizeCompact(p.win))
	d.Show()
}

// remove удаляет файл или папку — с подтверждением: удаление разъедется по
// устройствам через синхронизацию.
func (p *filesPage) remove(abs string, isDir bool) {
	what := "файл"
	if isDir {
		what = "папку со всем содержимым"
	}
	dialog.ShowConfirm("Удалить?",
		fmt.Sprintf("Удалить %s «%s»?\n\nУдаление уйдёт через синхронизацию на сервер и другие устройства.", what, filepath.Base(abs)),
		func(ok bool) {
			if !ok {
				return
			}
			if err := os.RemoveAll(abs); err != nil {
				dialog.ShowError(err, p.win)
				return
			}
			p.Refresh()
		}, p.win)
}

// properties показывает размер, дату и путь.
func (p *filesPage) properties(abs string, fi os.FileInfo) {
	size := fi.Size()
	lines := 1
	if fi.IsDir() {
		n, bytes := eng.DirStats(abs)
		size = bytes
		lines = n
	}
	text := "Путь: " + abs + "\n"
	text += "Размер: " + humanBytes(size) + "\n"
	if fi.IsDir() {
		text += fmt.Sprintf("Файлов внутри: %d\n", lines)
	}
	text += "Изменён: " + fi.ModTime().Format("02.01.2006 15:04:05")
	dialog.ShowInformation(filepath.Base(abs), text, p.win)
}
