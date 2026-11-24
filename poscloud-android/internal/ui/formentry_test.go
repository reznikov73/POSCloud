package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/test"

	"fyne.io/fyne/v2/container"
)

// TestFormEntryFocusAndType — обёртка поля не должна мешать фокусу и вводу.
// Цифровое поле просит цифровую клавиатуру.
func TestFormEntryFocusAndType(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(Theme())
	w := a.NewWindow("x")
	e := newFormEntry(true)
	w.SetContent(container.NewVBox(e))
	w.Resize(fyne.NewSize(320, 480))

	w.Canvas().Focus(e)
	if got := w.Canvas().Focused(); got != e {
		t.Fatalf("фокус не встал на поле: %T", got)
	}

	// касание по полю тоже ставит фокус (обёртка не должна его перехватывать)
	w.Canvas().Unfocus()
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(e)
	test.TapCanvas(w.Canvas(), fyne.NewPos(pos.X+e.Size().Width/2, pos.Y+e.Size().Height/2))
	if got := w.Canvas().Focused(); got != e {
		t.Fatalf("касание не сфокусировало поле: %T", got)
	}
	test.Type(e.Entry, "123")
	if e.Text != "123" {
		t.Fatalf("ввод не дошёл: %q", e.Text)
	}
	if e.Keyboard() != mobile.NumberKeyboard {
		t.Fatalf("клавиатура не цифровая: %v", e.Keyboard())
	}
}
