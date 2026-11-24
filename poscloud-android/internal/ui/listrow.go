package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// listRow — строка списка: значок слева, название и подпись под ним. Название
// и подпись режем сами по измеренной ширине: Label в Fyne не обрезает уже
// разложенный текст, и длинные имена файлов уходили за правый край экрана
// (так было в ленте «Недавние» и в списке конфликтных копий).
//
// Строка нужна одна на все списки — дерево «Файлов» рисует свою fileNode
// (у неё размер справа, а не подпись снизу), остальное собирается здесь.
type listRow struct {
	widget.BaseWidget
	icon      *widget.Icon
	title     *widget.Label
	sub       *widget.Label
	fullTitle string
	fullSub   string
	onTap     func()
}

const listRowPad = 8

func newListRow(onTap func()) *listRow {
	r := &listRow{
		icon:  widget.NewIcon(theme.FileIcon()),
		title: widget.NewLabel(""),
		sub:   caption(""),
		onTap: onTap,
	}
	r.title.TextStyle = fyne.TextStyle{}
	r.title.Wrapping = fyne.TextWrapOff
	r.sub.Wrapping = fyne.TextWrapOff
	r.sub.SizeName = theme.SizeNameCaptionText
	r.ExtendBaseWidget(r)
	return r
}

// set обновляет строку: полные тексты хранятся здесь, обрезка — при раскладке.
func (r *listRow) set(icon fyne.Resource, title, sub string) {
	if icon != nil {
		r.icon.SetResource(icon)
	}
	r.fullTitle = title
	r.fullSub = sub
	r.title.SetText(title)
	r.sub.SetText(sub)
}

// Tapped — касание по строке.
func (r *listRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *listRow) MinSize() fyne.Size {
	th := theme.CurrentForWidget(r)
	line := fyne.MeasureText("Ай", th.Size(theme.SizeNameText), fyne.TextStyle{}).Height +
		fyne.MeasureText("Ай", th.Size(theme.SizeNameCaptionText), fyne.TextStyle{}).Height + 1
	h := fyne.Max(th.Size(theme.SizeNameInlineIcon), line) + 2*listRowPad
	if h < rowMin {
		h = rowMin
	}
	return fyne.NewSize(0, h)
}

func (r *listRow) CreateRenderer() fyne.WidgetRenderer {
	rd := &listRowRenderer{obj: r, objs: []fyne.CanvasObject{r.icon, r.title, r.sub}}
	rd.Layout(r.MinSize())
	return rd
}

type listRowRenderer struct {
	obj  *listRow
	objs []fyne.CanvasObject
}

func (r *listRowRenderer) Destroy()                     {}
func (r *listRowRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *listRowRenderer) MinSize() fyne.Size           { return r.obj.MinSize() }

func (r *listRowRenderer) Refresh() {
	r.Layout(r.obj.Size())
	canvas.Refresh(r.obj)
}

// Layout: значок по центру слева, название сверху, подпись снизу; оба текста
// обрезаются по оставшейся ширине.
func (r *listRowRenderer) Layout(size fyne.Size) {
	o := r.obj
	th := theme.CurrentForWidget(o)

	iconSize := th.Size(theme.SizeNameInlineIcon)
	o.icon.Resize(fyne.NewSize(iconSize, iconSize))
	o.icon.Move(fyne.NewPos(0, (size.Height-iconSize)/2))

	left := iconSize + listRowPad*2
	width := size.Width - left
	if width < 0 {
		width = 0
	}

	titleSize := th.Size(theme.SizeNameText)
	subSize := th.Size(theme.SizeNameCaptionText)
	// Высоту берём из MeasureText, а не из MinSize подписей: MinSize
	// включает отступы темы, и подпись уезжала к следующей строке.
	titleH := fyne.MeasureText("Ай", titleSize, fyne.TextStyle{}).Height
	subH := fyne.MeasureText("Ай", subSize, fyne.TextStyle{}).Height
	const gap = 3
	total := titleH + gap + subH
	top := (size.Height - total) / 2

	o.title.Move(fyne.NewPos(left, top))
	o.title.Resize(fyne.NewSize(width, titleH))
	setTextIfChanged(o.title, ellipsize(o.fullTitle, width, titleSize))

	o.sub.Move(fyne.NewPos(left, top+titleH+gap))
	o.sub.Resize(fyne.NewSize(width, subH))
	setTextIfChanged(o.sub, ellipsize(o.fullSub, width, subSize))
}
