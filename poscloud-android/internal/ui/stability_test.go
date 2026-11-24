package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"poscloud-client-android/internal/client"
)

// TestPillSwitchSilent — SetCheckedSilently двигает ползунок, не вызывая
// OnChanged: таймер отражает состояние движка, а не «желание» пользователя.
func TestPillSwitchSilent(t *testing.T) {
	test.NewApp()
	calls := 0
	sw := newPillSwitch(false, func(bool) { calls++ })
	sw.SetChecked(true)
	if calls != 1 || !sw.Checked() {
		t.Fatalf("SetChecked: calls=%d checked=%v", calls, sw.Checked())
	}
	sw.SetCheckedSilently(false)
	if calls != 1 || sw.Checked() {
		t.Fatalf("SetCheckedSilently дёрнул OnChanged: calls=%d checked=%v", calls, sw.Checked())
	}
	sw.SetCheckedSilently(false)
	if calls != 1 {
		t.Fatalf("повторная установка: calls=%d", calls)
	}
}

// TestSetTextIfChanged — подпись получает новый текст; одинаковый текст
// оставляет объект нетронутым (в Fyne SetText всегда делает Refresh).
func TestSetTextIfChanged(t *testing.T) {
	test.NewApp()
	lbl := widget.NewLabel("a")
	setTextIfChanged(lbl, "a")
	if lbl.Text != "a" {
		t.Fatalf("текст: %q", lbl.Text)
	}
	setTextIfChanged(lbl, "b")
	if lbl.Text != "b" {
		t.Fatalf("после изменения: %q", lbl.Text)
	}
	btn := widget.NewButton("x", nil)
	setButtonTextIfChanged(btn, "y")
	if btn.Text != "y" {
		t.Fatalf("кнопка: %q", btn.Text)
	}
	setButtonTextIfChanged(btn, "y")
	if btn.Text != "y" {
		t.Fatalf("кнопка после повтора: %q", btn.Text)
	}
}

// newTestController — контроллер с сохранёнными настройками сервера.
func newTestController(t *testing.T, host string, port int, token string) *client.Controller {
	t.Helper()
	ctrl := client.NewController(t.TempDir())
	ctrl.SaveSettings(host, port, ctrl.DefaultSyncDir(), 3, token, false, "", "", true, "", "", 0)
	return ctrl
}

// TestAdvancedCollapsed — блок «Дополнительно» свёрнут по умолчанию, а его
// заголовок показывает настроенный сервер; тап разворачивает. Поля при этом
// заполнены из сохранённых настроек — «Запустить» работает и свёрнутым.
func TestAdvancedCollapsed(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	ctrl := newTestController(t, "192.168.1.10", 8090, "secret-token")
	st := Build(ctrl, w)
	if !st.advancedCard.Hidden {
		t.Fatal("дополнительные настройки должны быть свёрнуты по умолчанию")
	}
	if got := st.advancedToggle.Text; !strings.Contains(got, "192.168.1.10:8090") {
		t.Fatalf("заголовок не показывает сервер: %q", got)
	}
	if st.hostEntry.Text != "192.168.1.10" || st.tokenEntry.Text != "secret-token" {
		t.Fatalf("поля не заполнены из настроек: %q / %q", st.hostEntry.Text, st.tokenEntry.Text)
	}
	st.advancedToggle.OnTapped()
	if st.advancedCard.Hidden {
		t.Fatal("тап по заголовку не развернул блок")
	}
	st.advancedToggle.OnTapped()
	if !st.advancedCard.Hidden {
		t.Fatal("повторный тап не свернул блок")
	}
	w.Close()
}

// TestAdvancedTitleWithoutServer — без подключения заголовок без адреса.
func TestAdvancedTitleWithoutServer(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	ctrl := newTestController(t, "localhost", 8090, "")
	st := Build(ctrl, w)
	if got := st.advancedToggle.Text; got != "Дополнительно" {
		t.Fatalf("заголовок без подключения: %q", got)
	}
	w.Close()
}
