package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Единая шкала интерфейса. Раньше одна и та же роль была сделана разными
// числами: строка списка 45, дерево 28, строка диалога 48, поле 46. Теперь
// все размеры берутся отсюда, а высоты считаются от rowMin (норма касания
// Android — 48 dp).
const (
	spaceXS float32 = 4
	spaceS  float32 = 8
	spaceM  float32 = 12
	spaceL  float32 = 16

	rowMin float32 = 48 // минимальная высота касания

	iconSM float32 = 16
	iconMD float32 = 20

	// ширина колонок в парных полях
	colPort   float32 = 96
	colTime   float32 = 112
	colNarrow float32 = 150

	formEntryRadius float32 = 10
	radiusCard      float32 = 12
)

// fieldBox задаёт полю фиксированную ширину и высоту не меньше строки касания.
// Раньше высота была захардкожена 46 px — при увеличенном системном шрифте
// текст поля резался по вертикали.
func fieldBox(w float32, obj fyne.CanvasObject) fyne.CanvasObject {
	h := obj.MinSize().Height
	if h < rowMin {
		h = rowMin
	}
	return container.New(layout.NewGridWrapLayout(fyne.NewSize(w, h)), obj)
}

// fieldFill — поле на всю ширину, но не ниже строки касания: прозрачная
// подложка задаёт минимальную высоту, поле растягивается по контейнеру.
func fieldFill(obj fyne.CanvasObject) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, rowMin))
	return container.NewStack(spacer, obj)
}

// fieldLabel — подпись поля: мелкая, второстепенная, без переноса.
func fieldLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.SizeName = theme.SizeNameCaptionText
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapOff
	return l
}

// helperLine — подсказка под полем (например, «0 — без ограничения»).
func helperLine(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.SizeName = theme.SizeNameCaptionText
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}

// sectionLabel — заголовок группы внутри формы (чуть крупнее подписи поля).
func sectionLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.SizeName = theme.SizeNameText
	l.TextStyle = fyne.TextStyle{Bold: true}
	return l
}

// fieldRow — подпись над полем: так поля читаются на узком экране.
func fieldRow(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(fieldLabel(label), obj)
}

// pairRow — два поля в одной строке: левое тянется, правое фиксированной
// ширины. Оба одной высоты, иначе низы полей не совпадают (так было с
// адресом и портом).
func pairRow(leftLabel string, left fyne.CanvasObject, rightLabel string, right fyne.CanvasObject, rightW float32) fyne.CanvasObject {
	rightCol := container.NewVBox(fieldLabel(rightLabel), fieldBox(rightW, right))
	leftCol := container.NewVBox(fieldLabel(leftLabel), fieldFill(left))
	return container.NewBorder(nil, nil, nil, rightCol, leftCol)
}

// valueRow — пара «подпись — значение» для карточек статуса.
func valueRow(label string, value *widget.Label) fyne.CanvasObject {
	cap := fieldLabel(label)
	return container.NewVBox(cap, value)
}

// cardBox — карточка без заголовка и без двойных отступов: раньше часть
// карточек оборачивалась в NewPadded поверх собственного паддинга темы.
// Своя подложка вместо widget.Card: у Card с пустым заголовком всё равно
// остаётся полоса под заголовок, и простые блоки вырастали на 20–30 px.
func cardBox(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colCard)
	bg.CornerRadius = radiusCard
	bg.StrokeColor = colBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(content))
}

// switchRow — строка настройки с переключателем: подпись слева, переключатель
// справа, нажатие по всей строке.
func switchRow(text string, sw *pillSwitch) fyne.CanvasObject {
	l := widget.NewLabel(text)
	l.SizeName = theme.SizeNameText
	l.Wrapping = fyne.TextWrapWord
	row := container.NewBorder(nil, nil, nil,
		container.New(layout.NewGridWrapLayout(fyne.NewSize(toggleW, toggleTouchH)), sw), l)
	// нажатие по подписи тоже переключает
	return &tapRow{obj: row, onTap: func() { sw.SetChecked(!sw.Checked()) }, label: l}
}

// tapRow — строка, которая реагирует на касание целиком, при этом внутренние
// элементы (переключатель) продолжают работать сами.
type tapRow struct {
	widget.BaseWidget
	obj   fyne.CanvasObject
	label *widget.Label
	onTap func()
}

func (r *tapRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *tapRow) CreateRenderer() fyne.WidgetRenderer {
	return &tapRowRenderer{obj: r, child: r.obj}
}

func (r *tapRow) MinSize() fyne.Size { return r.obj.MinSize() }

type tapRowRenderer struct {
	obj   *tapRow
	child fyne.CanvasObject
}

func (r *tapRowRenderer) Destroy()                     {}
func (r *tapRowRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.child} }
func (r *tapRowRenderer) MinSize() fyne.Size           { return r.child.MinSize() }
func (r *tapRowRenderer) Layout(size fyne.Size) {
	r.child.Resize(size)
	r.child.Move(fyne.NewPos(0, 0))
}
func (r *tapRowRenderer) Refresh() { r.child.Refresh() }

// dialogSize — размер диалога по канве окна: жёсткие 420 px на экране 360 dp
// обрезались по краям. Верхняя граница — чтобы на планшете диалог не
// растягивался на весь экран.
func dialogSize(win fyne.Window) fyne.Size {
	w, h := float32(320), float32(480)
	if win != nil && win.Canvas() != nil {
		s := win.Canvas().Size()
		if s.Width > 0 {
			w = s.Width - 2*spaceM
			if w > 520 {
				w = 520
			}
		}
		if s.Height > 0 {
			h = s.Height - 6*spaceM
			if h > 640 {
				h = 640
			}
		}
	}
	if w < 280 {
		w = 280
	}
	if h < 320 {
		h = 320
	}
	return fyne.NewSize(w, h)
}

// dialogSizeCompact — для коротких диалогов (переименование, подтверждение):
// ширина от канвы, высота ограничена, чтобы не растягивать пустое окно.
func dialogSizeCompact(win fyne.Window) fyne.Size {
	s := dialogSize(win)
	if s.Height > 360 {
		s.Height = 360
	}
	return s
}

// bottomPad — пустая полоса в конце прокручиваемой формы, чтобы последнее
// поле можно было поднять выше закреплённых кнопок.
func bottomPad() fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(0, spaceL))
	return r
}

// buttonBox — кнопка на всю ширину и высотой строки касания: MinSize кнопки
// в теме 36 u, а палец ждёт 48.
func buttonBox(b fyne.CanvasObject) fyne.CanvasObject {
	return fieldFill(b)
}

// formEntry — поле формы: умеет просить цифровую клавиатуру и подкручивать
// прокрутку к себе при фокусе. В Fyne 2.8 клавиатуру выбирает сам виджет,
// поэтому цифровые поля — обёртка над Entry, а не отдельный виджет.
type formEntry struct {
	*widget.Entry
	numeric bool
	scroll  *container.Scroll
}

func newFormEntry(numeric bool) *formEntry {
	return &formEntry{Entry: widget.NewEntry(), numeric: numeric}
}

// Keyboard просит цифровую клавиатуру для порта, интервала и лимита.
func (e *formEntry) Keyboard() mobile.KeyboardType {
	if e.numeric {
		return mobile.NumberKeyboard
	}
	return e.Entry.Keyboard()
}

// Tapped ставит фокус на поле по касанию. У Entry.Tapped пустое тело
// (фокус ставит драйвер), а у обёртки драйвер может не найти внутренний
// Entry — поэтому фокусируем себя явно.
func (e *formEntry) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
		c.Focus(e)
	}
}

// FocusGained прокручивает форму к полю: на телефоне клавиатура закрывает
// нижнюю половину экрана, и поле ввода оставалось под ней.
func (e *formEntry) FocusGained() {
	e.Entry.FocusGained()
	e.ensureVisible()
}

// formScrollRetry — когда повторяем прокрутку после фокуса: клавиатура
// появляется с анимацией, и окно ужимается под неё не сразу.
var formScrollRetry = []time.Duration{300 * time.Millisecond, 700 * time.Millisecond}

// ensureVisible прокручивает форму к полю — сразу и ещё два раза, когда
// раскладка устоится. Первый расчёт упирается в прежний максимум прокрутки
// (окно ещё не ужалось под клавиатуру), и поле внизу длинной формы остаётся
// под ней — так на эмуляторе не показывался «Лимит скачивания». Повторы
// безопасны: поле не в фокусе или уже видно — ничего не происходит.
func (e *formEntry) ensureVisible() {
	if e.scroll == nil {
		return
	}
	e.scrollToField()
	if !e.isFocused() {
		return
	}
	for _, delay := range formScrollRetry {
		time.AfterFunc(delay, func() {
			fyne.Do(func() {
				if e.isFocused() {
					e.scrollToField()
				}
			})
		})
	}
}

// scrollToField прокручивает форму так, чтобы поле было видно целиком;
// если оно уже видно с отступом — ничего не делает.
func (e *formEntry) scrollToField() {
	if e.scroll == nil || e.Size().Width <= 0 {
		return
	}
	drv := fyne.CurrentApp().Driver()
	if drv == nil {
		return
	}
	s := e.scroll
	y := drv.AbsolutePositionForObject(e).Y
	top := drv.AbsolutePositionForObject(s).Y
	if y == 0 && top > 0 {
		// драйвер возвращает (0,0) для объектов, которых нет на канве
		// (скрытые поля): прокрутка к «нулю» уводила форму к началу
		return
	}
	if y >= top+spaceS && y+e.Size().Height <= top+s.Size().Height-spaceS {
		return
	}
	target := s.Offset.Y + (y - top) - 3*spaceL
	if target < 0 {
		target = 0
	}
	s.ScrollToOffset(fyne.NewPos(s.Offset.X, target))
}

// isFocused — стоит ли фокус канвы на этом поле (повтор прокрутки нужен
// только пока пользователь не ушёл на другое поле).
func (e *formEntry) isFocused() bool {
	drv := fyne.CurrentApp().Driver()
	if drv == nil {
		return false
	}
	c := drv.CanvasForObject(e)
	return c != nil && c.Focused() == fyne.Focusable(e)
}

// setScroll привязывает поле к прокручиваемой форме (для FocusGained).
func (e *formEntry) setScroll(s *container.Scroll) { e.scroll = s }

// chain связывает поля цепочкой «Далее» на клавиатуре: последнее поле
// закрывает клавиатуру.
func chain(entries ...*formEntry) {
	for i, e := range entries {
		if e == nil {
			continue
		}
		next := (*formEntry)(nil)
		if i+1 < len(entries) {
			next = entries[i+1]
		}
		e.OnSubmitted = func(string) {
			if next == nil || next.Disabled() {
				if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
					c.Unfocus()
				}
				return
			}
			if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
				c.Focus(next)
			}
		}
	}
}
