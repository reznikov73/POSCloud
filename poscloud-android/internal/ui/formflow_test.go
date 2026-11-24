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
