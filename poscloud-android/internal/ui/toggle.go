package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// pillSwitch — переключатель «вкл/выкл» в стиле мобильных настроек:
// крупный (64×36), срабатывает одним касанием. В Fyne 2.8 готового Switch нет.
type pillSwitch struct {
	widget.BaseWidget
	OnChanged func(bool)

	on   bool
	bg   *canvas.Rectangle
	knob *canvas.Circle
}

const (
	toggleW   = 64
	toggleH   = 36
	togglePad = 4
)

func newPillSwitch(on bool, changed func(bool)) *pillSwitch {
	t := &pillSwitch{
		OnChanged: changed,
		on:        on,
		bg:        canvas.NewRectangle(colBorder),
		knob:      canvas.NewCircle(colCard),
	}
	t.bg.CornerRadius = toggleH / 2
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

// Tapped переключает по касанию.
func (t *pillSwitch) Tapped(*fyne.PointEvent) { t.SetChecked(!t.on) }

// MinSize — размер под палец.
func (t *pillSwitch) MinSize() fyne.Size { return fyne.NewSize(toggleW, toggleH) }

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

// Layout растягивает фон и ставит ползунок влево («выкл») или вправо («вкл»).
func (r *pillRenderer) Layout(size fyne.Size) {
	r.t.bg.Resize(size)
	d := size.Height - 2*togglePad
	x := float32(togglePad)
	if r.t.on {
		x = size.Width - d - togglePad
	}
	r.t.knob.Resize(fyne.NewSize(d, d))
	r.t.knob.Move(fyne.NewPos(x, togglePad))
}

// Refresh перекрашивает фон: зелёный — включено, серый — выключено.
func (r *pillRenderer) Refresh() {
	if r.t.on {
		r.t.bg.FillColor = colSuccess
	} else {
		r.t.bg.FillColor = colMuted
	}
	canvas.Refresh(r.t.bg)
	canvas.Refresh(r.t.knob)
}
