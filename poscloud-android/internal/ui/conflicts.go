package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	eng "poscloud-engine"

	"poscloud-client-android/internal/client"
)

// conflictsCard — карточка конфликтов на вкладке «Файлы». Конфликтные копии
// создаёт движок, когда версии файла разошлись между устройствами: локальная
// правка сохраняется копией с пометкой « (конфликт …)» и лежит в папке, пока
// человек не выберет, какая версия верна. Нижняя панель занята пятью
// вкладками (кнопки входят впритык), поэтому конфликты — карточка здесь, а
// не шестая вкладка.
type conflictsCard struct {
	ctrl   *client.Controller
	files  *filesPage
	win    fyne.Window
	label  *widget.Label
	button *widget.Button
	box    *fyne.Container
}

func newConflictsCard(ctrl *client.Controller, files *filesPage, win fyne.Window) *conflictsCard {
	c := &conflictsCard{ctrl: ctrl, files: files, win: win}
	c.label = caption("")
	c.label.Wrapping = fyne.TextWrapWord
	c.button = widget.NewButtonWithIcon("Разрешить…", theme.WarningIcon(), func() { c.show() })
	c.button.Importance = widget.HighImportance
	return c
}

// object — контейнер, содержимое которого зависит от наличия конфликтов:
// пустое состояние — строка, а не карточка на 200 px.
func (c *conflictsCard) object() fyne.CanvasObject {
	c.box = container.NewVBox()
	return c.box
}

// Refresh перечитывает список конфликтных копий — локальный обход папки
// синхронизации, без сети.
func (c *conflictsCard) Refresh() {
	list := c.ctrl.Conflicts()
	if len(list) == 0 {
		c.label.SetText("Конфликтов нет")
		c.button.Hide()
		if c.box != nil {
			c.box.Objects = []fyne.CanvasObject{c.label}
			c.box.Refresh()
		}
		return
	}
	var size int64
	for _, p := range list {
		size += p.Size
	}
	c.label.SetText(fmt.Sprintf("Конфликты: %s (%s) — выберите версию",
		pluralConflicts(len(list)), humanBytes(size)))
	c.button.Show()
	if c.box != nil {
		c.box.Objects = []fyne.CanvasObject{cardBox(container.NewVBox(c.label, c.button))}
		c.box.Refresh()
	}
}

// show открывает список конфликтных копий: тап по строке — выбор версии.
func (c *conflictsCard) show() {
	list := c.ctrl.Conflicts()
	if len(list) == 0 {
		c.Refresh()
		return
	}
	rows := container.NewVBox()
	var d *dialog.CustomDialog
	for i := range list {
		pair := list[i]
		title := pair.Original
		if title == "" {
			title = pair.Conflict
		}
		// В списке — имя оригинала и дата копии: полное имя конфликтной
		// копии в строку не влезает и читать его тяжело.
		sub := "копия " + time.Unix(pair.MTime, 0).Format("02.01.2006 15:04") +
			" · " + humanBytes(pair.Size)
		row := newListRow(func() {
			d.Hide()
			c.choose(pair)
		})
		row.set(theme.FileIcon(), filepath.Base(title), sub)
		rows.Add(row)
	}
	d = dialog.NewCustom("Конфликтные копии", "Закрыть", container.NewVScroll(rows), c.win)
	d.Resize(dialogSize(c.win))
	d.Show()
}

// choose спрашивает, какую версию оставить, и применяет выбор.
func (c *conflictsCard) choose(pair eng.ConflictPair) {
	orig := pair.Original
	if orig == "" {
		orig = pair.Conflict
	}
	// Короткий заголовок и три отдельные строки вместо одной длинной фразы:
	// в заголовок диалога длинное имя файла не влезает, а в сплошном тексте
	// два похожих имени сливаются.
	var d *dialog.CustomDialog
	rows := container.NewVBox(
		caption("Файл «"+filepath.Base(orig)+"» правили на двух устройствах:"),
		caption("Моя версия — копия от "+time.Unix(pair.MTime, 0).Format("02.01.2006 15:04")+
			" ("+humanBytes(pair.Size)+")."),
		caption("Серверная — то, что сейчас лежит под именем файла."),
		widget.NewSeparator(),
		dialogRow("Оставить мою версию", theme.DocumentIcon(), func() {
			d.Hide()
			c.apply(pair, "local")
		}),
		dialogRow("Оставить серверную", theme.DownloadIcon(), func() {
			d.Hide()
			c.apply(pair, "server")
		}),
	)
	d = dialog.NewCustom("Какую версию оставить?", "Закрыть", rows, c.win)
	d.Resize(dialogSize(c.win))
	d.Show()
}

// apply выполняет выбор в горутине (уходит на сервер) и обновляет карточку
// и дерево файлов.
func (c *conflictsCard) apply(pair eng.ConflictPair, mode string) {
	what := "Оставляем мою версию…"
	if mode == "server" {
		what = "Оставляем серверную версию…"
	}
	d := dialog.NewProgress("Разрешение конфликта", what, c.win)
	d.Show()
	go func() {
		err := c.ctrl.ResolveConflict(pair.Conflict, mode)
		fyne.Do(func() {
			d.Hide()
			if err != nil {
				dialog.ShowError(err, c.win)
				return
			}
			c.Refresh()
			if c.files != nil {
				c.files.Refresh()
			}
		})
	}()
}

// pluralConflicts склоняет «конфликт» по русским правилам: 1 конфликт,
// 2 конфликта, 5 конфликтов, 11 конфликтов.
func pluralConflicts(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d конфликт", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d конфликта", n)
	default:
		return fmt.Sprintf("%d конфликтов", n)
	}
}
