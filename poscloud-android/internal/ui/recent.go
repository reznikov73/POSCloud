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

// recentPage — вкладка «Лента» (заголовок «Недавние»): что менялось в папке
// синхронизации — пришло с сервера, уехало на сервер или удалено. События
// отдаёт движок пачкой в конце цикла (SetChangeFunc), история хранится
// в events.json и переживает перезапуск приложения. Подпись «Лента» короче
// «Недавних» и влезает в нижнюю панель из шести вкладок.
type recentPage struct {
	ctrl    *client.Controller
	win     fyne.Window
	state   *widget.Label
	box     *fyne.Container
	goFiles func() // переход к вкладке «Файлы» (прокидывается после сборки pager)
}

func newRecentPage(ctrl *client.Controller, win fyne.Window) *recentPage {
	p := &recentPage{ctrl: ctrl, win: win}
	p.state = caption("")
	p.state.Wrapping = fyne.TextWrapWord
	p.box = container.NewVBox()
	return p
}

// object собирает страницу: заголовок, итог, кнопка очистки и сама лента.
func (p *recentPage) object() fyne.CanvasObject {
	clearBtn := widget.NewButtonWithIcon("Очистить ленту", theme.DeleteIcon(), func() {
		p.ctrl.ClearRecentEvents()
		p.Refresh()
	})
	top := container.NewVBox(
		heading("Недавние"),
		p.state,
		clearBtn,
		widget.NewSeparator(),
	)
	return container.NewBorder(top, nil, nil, nil, container.NewScroll(p.box))
}

// Refresh перечитывает историю событий и строит строки ленты.
func (p *recentPage) Refresh() {
	events := p.ctrl.RecentEvents()
	if len(events) == 0 {
		p.state.SetText("Пока ничего не менялось")
		p.box.Objects = nil
		p.box.Refresh()
		return
	}
	p.state.SetText(pluralChanges(len(events)) + " · новые сверху")
	objs := make([]fyne.CanvasObject, 0, len(events))
	// новые события в конце истории — показываем сверху
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		objs = append(objs, p.row(ev))
	}
	p.box.Objects = objs
	p.box.Refresh()
}

// row строит строку события: значок операции, имя и время.
func (p *recentPage) row(ev eng.ChangeEvent) fyne.CanvasObject {
	var icon fyne.Resource
	var op string
	switch ev.Op {
	case "up":
		icon = theme.UploadIcon()
		op = "отправлен"
	case "down":
		icon = theme.DownloadIcon()
		op = "получен"
	case "delete":
		icon = theme.DeleteIcon()
		op = "удалён"
	default:
		icon = theme.FileIcon()
		op = ev.Op
	}
	name := ev.Name
	if base := filepath.Base(ev.Name); base != "" {
		name = base
	}
	text := fmt.Sprintf("%s · %s", name, time.Unix(ev.Time, 0).Format("02.01.2006 15:04"))
	return dialogRow(text, icon, func() { p.details(ev, op) })
}

// details показывает полное имя файла и предлагает перейти к «Файлам».
func (p *recentPage) details(ev eng.ChangeEvent, op string) {
	var d *dialog.CustomDialog
	rows := container.NewVBox(
		caption("Файл "+ev.Name),
		caption("Операция: "+op),
		caption("Время: "+time.Unix(ev.Time, 0).Format("02.01.2006 15:04:05")),
		widget.NewSeparator(),
		dialogRow("Открыть вкладку «Файлы»", theme.FolderOpenIcon(), func() {
			d.Hide()
			if p.goFiles != nil {
				p.goFiles()
			}
		}),
	)
	d = dialog.NewCustom("Изменение файла", "Закрыть", rows, p.win)
	d.Resize(fyne.NewSize(420, 420))
	d.Show()
}

// pluralChanges склоняет «изменение» по русским правилам: 1 изменение,
// 2 изменения, 5 изменений.
func pluralChanges(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d изменение", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d изменения", n)
	default:
		return fmt.Sprintf("%d изменений", n)
	}
}
