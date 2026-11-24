package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// pager — нижняя навигация приложения: ряд кнопок «иконка + подпись» и одна
// видимая страница. Своя панель вместо container.AppTabs: у AppTabs на телефоне
// пятую вкладку уносит в «…» (каждой вкладке нужно ~150 px, а экран 430), и
// «Файлы» с «Журналом» оказались бы спрятаны за меню.
type pager struct {
	items []*pagerItem
	bar   *fyne.Container
	stack *fyne.Container
	cur   int
	// OnSelect вызывается после переключения (например, чтобы перечитать дерево).
	OnSelect func(name string)
}

type pagerItem struct {
	name string
	body fyne.CanvasObject
	btn  *navItem
}

func newPager() *pager {
	return &pager{cur: -1}
}

// Add добавляет страницу: подпись, иконку и содержимое.
func (p *pager) Add(name string, icon fyne.Resource, body fyne.CanvasObject) {
	idx := len(p.items)
	btn := newNavItem(name, icon, func() { p.Select(idx) })
	p.items = append(p.items, &pagerItem{name: name, body: body, btn: btn})
}

// object собирает панель и страницы. Вызывать после всех Add.
func (p *pager) object() fyne.CanvasObject {
	p.bar = container.NewGridWithColumns(len(p.items))
	objs := make([]fyne.CanvasObject, 0, len(p.items))
	for _, it := range p.items {
		p.bar.Add(it.btn)
		it.body.Hide()
		objs = append(objs, it.body)
	}
	p.stack = container.NewStack(objs...)
	if len(p.items) > 0 {
		p.Select(0)
	}
	line := canvas.NewRectangle(colBorder)
	line.SetMinSize(fyne.NewSize(0, 1))
	// Панель непрозрачная: раньше при прокрутке текст уезжал под иконки.
	barBg := canvas.NewRectangle(colCard)
	barBox := container.NewStack(barBg, p.bar)
	// Полоса под панелью — место под жест-бар Android: вставку системы
	// Fyne 2.8 не отдаёт, и подписи вкладок попадали под жест.
	inset := canvas.NewRectangle(colCard)
	inset.SetMinSize(fyne.NewSize(0, pagerInset))
	// Страницы с отступом от краёв: карточки больше не липнут к рамке экрана.
	return container.NewBorder(nil, container.NewVBox(line, barBox, inset), nil, nil,
		container.NewPadded(p.stack))
}

// Select показывает страницу с индексом i.
func (p *pager) Select(i int) {
	if i < 0 || i >= len(p.items) || i == p.cur {
		return
	}
	if p.cur >= 0 {
		p.items[p.cur].btn.SetActive(false)
	}
	p.cur = i
	for j, it := range p.items {
		if j == i {
			it.body.Show()
		} else {
			it.body.Hide()
		}
	}
	p.items[i].btn.SetActive(true)
	if p.OnSelect != nil {
		p.OnSelect(p.items[i].name)
	}
}

// Current — подпись выбранной страницы ("" — ничего не выбрано).
func (p *pager) Current() string {
	if p.cur < 0 || p.cur >= len(p.items) {
		return ""
	}
	return p.items[p.cur].name
}

// SelectByName выбирает страницу по подписи (для тестов и горячих клавиш).
func (p *pager) SelectByName(name string) {
	for i, it := range p.items {
		if it.name == name {
			p.Select(i)
			return
		}
	}
}

// Names — подписи страниц по порядку.
func (p *pager) Names() []string {
	out := make([]string, 0, len(p.items))
	for _, it := range p.items {
		out = append(out, it.name)
	}
	return out
}

// navItem — кнопка нижней панели: полоска-индикатор, иконка и подпись.
type navItem struct {
	widget.BaseWidget
	base   fyne.Resource
	icon   *widget.Icon
	label  *widget.Label
	mark   *canvas.Rectangle
	onTap  func()
	active bool
}

const navMarkHeight = 3

// pagerInset — полоса под панелью под системный жест-бар.
const pagerInset = 16

// navBarHeight — высота панели: иконка, подпись и отступы, но не меньше 56.
const navBarHeight = 56

func newNavItem(text string, icon fyne.Resource, onTap func()) *navItem {
	n := &navItem{
		base:  icon,
		icon:  widget.NewIcon(icon),
		label: widget.NewLabel(text),
		mark:  canvas.NewRectangle(colPrimary),
		onTap: onTap,
	}
	n.label.Alignment = fyne.TextAlignCenter
	n.label.SizeName = theme.SizeNameCaptionText
	n.label.TextStyle = fyne.TextStyle{Bold: false}
	n.mark.Hide()
	n.ExtendBaseWidget(n)
	return n
}

// Tapped — касание по кнопке панели.
func (n *navItem) Tapped(*fyne.PointEvent) {
	if n.onTap != nil {
		n.onTap()
	}
}

// SetActive подсвечивает выбранный раздел: цветная полоска, синяя иконка,
// жирная подпись.
func (n *navItem) SetActive(on bool) {
	if n.active == on {
		return
	}
	n.active = on
	if on {
		n.mark.Show()
		n.icon.SetResource(theme.NewPrimaryThemedResource(n.base))
		n.label.Importance = widget.HighImportance
		n.label.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		n.mark.Hide()
		n.icon.SetResource(theme.NewThemedResource(n.base))
		n.label.Importance = widget.LowImportance
		n.label.TextStyle = fyne.TextStyle{Bold: false}
	}
	n.label.Refresh()
	n.icon.Refresh()
	n.Refresh()
}

func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewVBox(
		container.NewCenter(n.icon),
		n.label,
	)
	r := &navItemRenderer{obj: n, content: content, objs: []fyne.CanvasObject{n.mark, content}}
	r.Layout(n.MinSize())
	return r
}

// MinSize — высота в палец: иконка 20, подпись 12 и отступы; не ниже 56.
func (n *navItem) MinSize() fyne.Size {
	h := theme.CurrentForWidget(n).Size(theme.SizeNameInlineIcon) + spaceXS +
		n.label.MinSize().Height + 2*spaceXS
	if h < navBarHeight {
		h = navBarHeight
	}
	return fyne.NewSize(0, h)
}

type navItemRenderer struct {
	obj     *navItem
	content *fyne.Container
	objs    []fyne.CanvasObject
}

func (r *navItemRenderer) Destroy()                     {}
func (r *navItemRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *navItemRenderer) MinSize() fyne.Size           { return r.obj.MinSize() }
func (r *navItemRenderer) Refresh()                     { canvas.Refresh(r.obj) }

// Layout: полоска сверху, содержимое по центру оставшегося места.
func (r *navItemRenderer) Layout(size fyne.Size) {
	r.obj.mark.Resize(fyne.NewSize(size.Width, navMarkHeight))
	r.obj.mark.Move(fyne.NewPos(0, 0))
	r.content.Resize(fyne.NewSize(size.Width, size.Height-navMarkHeight))
	r.content.Move(fyne.NewPos(0, navMarkHeight))
}
