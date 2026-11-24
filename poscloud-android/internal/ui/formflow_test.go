package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"poscloud-client-android/internal/client"
)

// TestSwitchRowToggles — касание по подписи переключает настройку: строка
// настройки нажимается целиком, а не только маленькая таблетка.
func TestSwitchRowToggles(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(Theme())
	w := a.NewWindow("x")
	sw := newPillSwitch(false, nil)
	row := switchRow("Тихие часы", sw)
	w.SetContent(container.NewVBox(row))
	w.Resize(fyne.NewSize(360, 200))

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(row)
	test.TapCanvas(w.Canvas(), fyne.NewPos(pos.X+20, pos.Y+row.MinSize().Height/2))
	if !sw.Checked() {
		t.Fatal("касание по подписи не включило переключатель")
	}
	test.TapCanvas(w.Canvas(), fyne.NewPos(pos.X+20, pos.Y+row.MinSize().Height/2))
	if sw.Checked() {
		t.Fatal("повторное касание не выключило переключатель")
	}
}

// TestChainSkipsHiddenQuietFields — «Далее» после «Интервала» ведёт к полю
// лимита, когда тихие часы выключены (поля скрыты), и к «с» — когда включены:
// раньше цепочка уводила фокус в невидимые поля.
// Fyne молча отказывается фокусировать скрытые объекты, поэтому проверяем
// цепочку на форме без карточки «Дополнительно» (все поля видимы).
func TestChainSkipsHiddenQuietFields(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(Theme())
	w := a.NewWindow("x")
	ui := &State{}
	ui.hostEntry = newFormEntry(false)
	ui.portEntry = newFormEntry(true)
	ui.tokenEntry = newFormEntry(false)
	ui.deviceEntry = newFormEntry(false)
	ui.intervalEntry = newFormEntry(true)
	ui.quietFrom = newFormEntry(false)
	ui.quietTo = newFormEntry(false)
	ui.maxDownEntr = newFormEntry(true)
	ui.quietRow = container.NewVBox(ui.quietFrom, ui.quietTo)
	box := container.NewVBox(ui.hostEntry, ui.portEntry, ui.tokenEntry,
		ui.deviceEntry, ui.intervalEntry, ui.quietRow, ui.maxDownEntr)
	sc := container.NewVScroll(box)
	w.SetContent(sc)
	w.Resize(fyne.NewSize(320, 640))
	ui.setFormScroll(sc)

	// тихие часы выключены: «Далее» от «Интервала» — сразу к лимиту
	ui.quietRow.Hide()
	ui.chainAll()
	w.Canvas().Focus(ui.intervalEntry)
	ui.intervalEntry.OnSubmitted("")
	if got := w.Canvas().Focused(); got != ui.maxDownEntr {
		t.Fatalf("при скрытых тихих часах «Далее» должен вести к лимиту, получено %T", got)
	}
	ui.maxDownEntr.OnSubmitted("")
	if got := w.Canvas().Focused(); got != nil {
		t.Fatalf("последнее поле должно закрывать цепочку, фокус остался на %T", got)
	}

	// включаем тихие часы — цепочка перестраивается и ведёт через их поля
	ui.quietRow.Show()
	ui.chainAll()
	w.Canvas().Focus(ui.intervalEntry)
	ui.intervalEntry.OnSubmitted("")
	if got := w.Canvas().Focused(); got != ui.quietFrom {
		t.Fatalf("при включённых тихих часах «Далее» должен вести к «с», получено %T", got)
	}
	ui.quietFrom.OnSubmitted("")
	if got := w.Canvas().Focused(); got != ui.quietTo {
		t.Fatalf("«Далее» от «с» должен вести к «по», получено %T", got)
	}
	ui.quietTo.OnSubmitted("")
	if got := w.Canvas().Focused(); got != ui.maxDownEntr {
		t.Fatalf("«Далее» от «по» должен вести к лимиту, получено %T", got)
	}
	w.Close()
}

// TestSaveSettingsInlineErrors — ошибки ввода показываются под полями и не
// сохраняются: тихие часы с мусором не превращаются в пустые молча.
func TestSaveSettingsInlineErrors(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(Theme())
	w := a.NewWindow("x")
	ctrl := client.NewController(t.TempDir())
	st := Build(ctrl, w)

	st.quietSwitch.SetCheckedSilently(true)
	showIf(st.quietRow, true)
	st.quietFrom.SetText("25:00")
	st.quietTo.SetText("07:00")
	st.saveSettings(ctrl)

	if !st.quietErr.Visible() || st.quietErr.Text == "" {
		t.Fatalf("ошибка тихих часов не показана: %q", st.quietErr.Text)
	}
	if got := ctrl.Config().QuietFrom; got != "" {
		t.Fatalf("неверное время сохранилось: %q", got)
	}

	st.quietFrom.SetText("23:00")
	st.quietTo.SetText("07:00")
	st.maxDownEntr.SetText("999999")
	st.saveSettings(ctrl)
	if st.quietErr.Visible() {
		t.Fatal("после исправления ошибка осталась")
	}
	if got := ctrl.Config().QuietFrom; got != "23:00" {
		t.Fatalf("тихие часы не сохранены: %q", got)
	}
	if !st.saveInfo.Visible() {
		t.Fatal("подтверждение сохранения не показано")
	}
}
