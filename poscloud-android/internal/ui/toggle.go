package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// pillSwitch — переключатель «вкл/выкл» в стиле мобильных настроек: таблетка
// с круглым ползунком, срабатывает одним касанием. В Fyne 2.8 готового Switch нет.
//
// Размер у переключателя постоянный: раскладка может отдать ему хоть всю
// карточку по высоте (BorderLayout растягивает правое поле), а рисуется всё
// равно таблетка toggleW×toggleH по центру — иначе ползунок превращался в
// огромный белый круг на весь экран.
type pillSwitch struct {
	widget.BaseWidget
	OnChanged func(bool)

	on   bool
	bg   *canvas.Rectangle
	knob *canvas.Circle
}

const (
	toggleW      = 52 // ширина рисуемой таблетки, dp
	toggleH      = 32 // высота таблетки, dp
	toggleTouchH = 48 // зона касания: не меньше строки пальца
	togglePad    = 4  // отступ ползунка от края таблетки, dp
)

func newPillSwitch(on bool, changed func(bool)) *pillSwitch {
	t := &pillSwitch{
		OnChanged: changed,
		on:        on,
		bg:        canvas.NewRectangle(colTrackOff),
		knob:      canvas.NewCircle(colCard),
	}
	t.bg.CornerRadius = toggleH / 2
	t.knob.StrokeWidth = 1
	t.knob.Shadow = canvas.Shadow{
		Color:      color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x24},
		BlurRadius: 3,
		Offset:     fyne.NewPos(0, 1),
	}
	t.ExtendBaseWidget(t)
	return t
}

// Checked сообщает текущее положение.
func (t *pillSwitch) Checked() bool { return t.on }

// SetChecked переключает и вызывает OnChanged, если положение изменилось.
func (t *pillSwitch) SetChecked(on bool) {
	if on == t.on {
		return
	}
	t.on = on
	t.Refresh()
	if t.OnChanged != nil {
		t.OnChanged(on)
	}
}

// SetCheckedSilently выставляет положение, не вызывая OnChanged: так таймер
// отражает фактическое состояние движка, а не «желание» пользователя. Раньше
// для этого обнуляли OnChanged на время SetChecked — в этом окне тап
// пользователя терялся.
func (t *pillSwitch) SetCheckedSilently(on bool) {
	if on == t.on {
		return
	}
	t.on = on
	t.Refresh()
}

// Tapped переключает по касанию.
func (t *pillSwitch) Tapped(*fyne.PointEvent) { t.SetChecked(!t.on) }

// MinSize — зона касания 52×48: сама таблетка 52×32 рисуется по центру,
// но палец попадает и по вертикали (раньше касание было всего 32 dp).
func (t *pillSwitch) MinSize() fyne.Size { return fyne.NewSize(toggleW, toggleTouchH) }

// CreateRenderer возвращает отрисовщик из фона-«таблетки» и кружка-ползунка.
func (t *pillSwitch) CreateRenderer() fyne.WidgetRenderer {
	r := &pillRenderer{t: t, objs: []fyne.CanvasObject{t.bg, t.knob}}
	r.Layout(t.MinSize())
	r.Refresh()
	return r
}

type pillRenderer struct {
	t    *pillSwitch
	objs []fyne.CanvasObject
}

func (r *pillRenderer) Destroy()                     {}
func (r *pillRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *pillRenderer) MinSize() fyne.Size           { return r.t.MinSize() }

// Layout рисует таблетку постоянного размера по центру отведённого места и
// ставит ползунок влево («выкл») или вправо («вкл»).
func (r *pillRenderer) Layout(size fyne.Size) {
	w, h := float32(toggleW), float32(toggleH)
	// если места меньше таблетки — уменьшаем пропорционально, но не растягиваем
	if size.Width > 0 && size.Width < w {
		w = size.Width
		h = w * toggleH / toggleW
	}
	if size.Height > 0 && size.Height < h {
		h = size.Height
		w = h * toggleW / toggleH
	}
	x0 := (size.Width - w) / 2
	y0 := (size.Height - h) / 2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}

	r.t.bg.Resize(fyne.NewSize(w, h))
	r.t.bg.CornerRadius = h / 2
	r.t.bg.Move(fyne.NewPos(x0, y0))

	pad := h * togglePad / toggleH
	d := h - 2*pad
	x := x0 + pad
	if r.t.on {
		x = x0 + w - d - pad
	}
	r.t.knob.Resize(fyne.NewSize(d, d))
	r.t.knob.Move(fyne.NewPos(x, y0+pad))
}

// Refresh перекрашивает фон (зелёный — включено, серый — выключено) и заново
// расставляет ползунок: без Layout он оставался на месте, менялся только цвет.
func (r *pillRenderer) Refresh() {
	if r.t.on {
		r.t.bg.FillColor = colSuccess
		r.t.knob.StrokeColor = colSuccess
	} else {
		r.t.bg.FillColor = colTrackOff
		r.t.knob.StrokeColor = colTrackEdge
	}
	if size := r.t.Size(); size.Width > 0 && size.Height > 0 {
		r.Layout(size)
	}
	canvas.Refresh(r.t.bg)
	canvas.Refresh(r.t.knob)
}
