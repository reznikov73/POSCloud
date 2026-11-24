package ui

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"poscloud-client-android/internal/client"
)

// ---------- карточка «Хранилище» на «Статусе» ----------
//
// Показывает, сколько занято здесь и на сервере; по касанию — разбивку по
// папкам первого уровня. Цифры считает Controller.StorageStats (обход папки
// плюс запрос манифеста) — это долго, поэтому всегда в горутине.

// storageTTL — без просьбы пользователя пересчитываем хранилище не чаще раза
// в минуту: обход папки на внешнем хранилище (FUSE) небыстрый, а манифест —
// лишний поход на сервер.
const storageTTL = time.Minute

// newStorageValue — значение справа в строке карточки: как цифры «Статуса».
func newStorageValue(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.SizeName = theme.SizeNameSubHeadingText
	l.Alignment = fyne.TextAlignTrailing
	return l
}

// newStorageCard строит карточку «Хранилище» для «Статуса»: заголовок, кнопка
// пересчёта, строки «Здесь» и «На сервере». Виджеты кладёт в State — их
// обновляет applyStorage. По касанию — разбивка по папкам.
func newStorageCard(ctrl *client.Controller, ui *State) fyne.CanvasObject {
	ui.storageLocal = newStorageValue("—")
	ui.storageServer = newStorageValue("—")
	ui.storageNote = caption("")
	ui.storageNote.Wrapping = fyne.TextWrapWord
	ui.storageNote.Hide()
	ui.btnStorage = iconButton(theme.ViewRefreshIcon(), func() { refreshStorage(ctrl, ui, true) })
	title := widget.NewLabelWithStyle("Хранилище", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameSubHeadingText
	card := &tapRow{onTap: func() { ui.showStorageDialog(ctrl) }}
	card.obj = cardBox(container.NewVBox(
		container.NewBorder(nil, nil, nil, ui.btnStorage, title),
		ui.storageNote,
		storageRow("Здесь", ui.storageLocal),
		storageRow("На сервере", ui.storageServer),
	))
	card.ExtendBaseWidget(card)
	return card
}

// storageRow — пара «подпись слева, значение справа» в карточке.
func storageRow(label string, v *widget.Label) fyne.CanvasObject {
	return container.NewBorder(nil, nil, caption(label), nil, v)
}

// beginStorage — можно ли начать пересчёт: по кнопке — всегда, иначе не чаще
// раза в storageTTL и никогда поверх уже идущего расчёта. При разрешении
// помечает расчёт идущим и запоминает время старта.
func beginStorage(ui *State, force bool, now time.Time) bool {
	if !force {
		if at := ui.storageAt.Load(); at != 0 && now.Sub(time.Unix(0, at)) < storageTTL {
			return false
		}
	}
	if !ui.storageBusy.CompareAndSwap(0, 1) {
		return false
	}
	ui.storageAt.Store(now.UnixNano())
	return true
}

// refreshStorage пересчитывает цифры хранилища в горутине и показывает их.
// Безопасно вызывать из любой горутины: виджеты трогаем только через fyne.Do.
func refreshStorage(ctrl *client.Controller, ui *State, force bool) {
	if !beginStorage(ui, force, time.Now()) {
		return
	}
	fyne.Do(func() {
		ui.btnStorage.Disable()
		if ui.storageLast == nil {
			setTextIfChanged(ui.storageLocal, "Считаем…")
			setTextIfChanged(ui.storageServer, "—")
		}
	})
	go func() {
		st := ctrl.StorageStats()
		fyne.Do(func() {
			ui.storageLast = &st
			ui.btnStorage.Enable()
			ui.applyStorage(st)
			ui.storageBusy.Store(0)
		})
	}()
}

// applyStorage показывает цифры в карточке: по строке «здесь» и «на сервере»,
// под ними — почему серверных цифр нет (если их нет).
func (ui *State) applyStorage(st client.StorageStats) {
	setTextIfChanged(ui.storageLocal, fmt.Sprintf("%s · %s", humanBytes(st.LocalBytes), pluralFiles(st.LocalFiles)))
	note := ""
	switch {
	case st.ServerErr == nil:
		setTextIfChanged(ui.storageServer, fmt.Sprintf("%s · %s", humanBytes(st.ServerBytes), pluralFiles(st.ServerFiles)))
	case errors.Is(st.ServerErr, client.ErrNoServer):
		setTextIfChanged(ui.storageServer, "—")
		note = "Подключитесь к серверу на вкладке «Связь» — здесь появятся цифры с сервера."
	default:
		setTextIfChanged(ui.storageServer, "—")
		note = "Сервер сейчас недоступен — цифры с сервера появятся после цикла синхронизации."
	}
	if note == "" {
		showIf(ui.storageNote, false)
		return
	}
	setTextIfChanged(ui.storageNote, note)
	showIf(ui.storageNote, true)
}

// showStorageDialog — разбивка хранилища по папкам первого уровня.
func (ui *State) showStorageDialog(ctrl *client.Controller) {
	st := ui.storageLast
	if st == nil {
		refreshStorage(ctrl, ui, true)
		dialog.ShowInformation("Хранилище", "Цифры ещё считаются — коснитесь карточки ещё раз через несколько секунд.", ui.win)
		return
	}
	d := dialog.NewCustom("Хранилище по папкам", "Закрыть", storageDialogContent(st), ui.win)
	d.Resize(dialogSize(ui.win))
	d.Show()
}

// storageDialogContent — содержимое диалога: итоги и разбивка по папкам.
func storageDialogContent(st *client.StorageStats) fyne.CanvasObject {
	rows := container.NewVBox(
		caption(fmt.Sprintf("Здесь: %s · %s", humanBytes(st.LocalBytes), pluralFiles(st.LocalFiles))),
		caption(fmt.Sprintf("На сервере: %s · %s", humanBytes(st.ServerBytes), pluralFiles(st.ServerFiles))),
		widget.NewSeparator(),
	)
	objs := storageFolderRows(st.Folders)
	if len(objs) == 0 {
		rows.Add(caption("Папок пока нет."))
	}
	for _, o := range objs {
		rows.Add(o)
	}
	return container.NewVScroll(rows)
}

// storageFolderRows — строки разбивки: папка, под ней размеры здесь и на
// сервере (только то, что есть). Строки — общие listRow с обрезкой по ширине.
func storageFolderRows(folders []client.StorageFolder) []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, len(folders)*2)
	for _, f := range folders {
		if len(objs) > 0 {
			objs = append(objs, widget.NewSeparator())
		}
		row := newListRow(nil)
		row.set(theme.FolderIcon(), storageFolderTitle(f), storageFolderSub(f))
		objs = append(objs, row)
	}
	return objs
}

func storageFolderTitle(f client.StorageFolder) string {
	if f.Root {
		return "Файлы в корне"
	}
	return f.Path
}

// storageFolderSub — подпись строки папки: короткая, чтобы влезть в одну
// строку на 360 dp. Когда файлы есть и здесь, и на сервере, счётчики
// опускаем (они на вкладке «Папки») — важнее сравнить размеры.
func storageFolderSub(f client.StorageFolder) string {
	switch {
	case f.LocFiles == 0:
		return fmt.Sprintf("только на сервере · %s · %s", humanBytes(f.Bytes), pluralFiles(f.Files))
	case f.Files == 0:
		return fmt.Sprintf("только здесь · %s · %s", humanBytes(f.LocBytes), pluralFiles(f.LocFiles))
	default:
		return fmt.Sprintf("здесь %s · на сервере %s", humanBytes(f.LocBytes), humanBytes(f.Bytes))
	}
}
