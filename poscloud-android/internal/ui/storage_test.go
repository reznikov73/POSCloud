package ui

import (
	"errors"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"poscloud-client-android/internal/client"
)

// TestBeginStorage — пересчёт по кнопке всегда, без кнопки — не чаще раза в
// минуту и никогда поверх уже идущего расчёта.
func TestBeginStorage(t *testing.T) {
	ui := &State{}
	now := time.Now()
	if !beginStorage(ui, false, now) {
		t.Fatal("первый расчёт должен быть разрешён")
	}
	if ui.storageBusy.Load() != 1 || ui.storageAt.Load() != now.UnixNano() {
		t.Fatal("после разрешения расчёт должен считаться идущим с временем старта")
	}
	if beginStorage(ui, true, now) {
		t.Fatal("поверх идущего расчёта запускать нельзя, даже по кнопке")
	}
	ui.storageBusy.Store(0)
	if beginStorage(ui, false, now.Add(10*time.Second)) {
		t.Fatal("раньше минуты без кнопки пересчитывать нельзя")
	}
	if !beginStorage(ui, false, now.Add(storageTTL)) {
		t.Fatal("через минуту пересчёт должен быть разрешён")
	}
	ui.storageBusy.Store(0)
	if !beginStorage(ui, true, now.Add(storageTTL)) {
		t.Fatal("по кнопке пересчёт должен быть разрешён в любой момент")
	}
}

// TestApplyStorage — карточка показывает размеры с числом файлов, а когда
// серверных цифр нет — прочерк и причину под строками.
func TestApplyStorage(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	ui := &State{win: testApp.NewWindow("x")}
	ui.storageLocal = newStorageValue("")
	ui.storageServer = newStorageValue("")
	ui.storageNote = caption("")
	ui.storageNote.Hide()

	ui.applyStorage(client.StorageStats{LocalFiles: 3, LocalBytes: 160, ServerFiles: 2, ServerBytes: 1100})
	if ui.storageLocal.Text != "160 Б · 3 файла" {
		t.Fatalf("здесь: %q", ui.storageLocal.Text)
	}
	if ui.storageServer.Text != "1.1 КБ · 2 файла" {
		t.Fatalf("на сервере: %q", ui.storageServer.Text)
	}
	if ui.storageNote.Visible() {
		t.Fatal("с серверными цифрами подпись не нужна")
	}

	ui.applyStorage(client.StorageStats{LocalFiles: 1, LocalBytes: 5 << 20, ServerErr: client.ErrNoServer})
	if ui.storageServer.Text != "—" {
		t.Fatalf("без сервера: %q", ui.storageServer.Text)
	}
	if !ui.storageNote.Visible() || ui.storageNote.Text != "Подключитесь к серверу на вкладке «Связь» — здесь появятся цифры с сервера." {
		t.Fatalf("подпись без подключения: visible=%v %q", ui.storageNote.Visible(), ui.storageNote.Text)
	}

	ui.applyStorage(client.StorageStats{ServerErr: errors.New("dial tcp: timeout")})
	if !ui.storageNote.Visible() || ui.storageNote.Text != "Сервер сейчас недоступен — цифры с сервера появятся после цикла синхронизации." {
		t.Fatalf("подпись без связи: visible=%v %q", ui.storageNote.Visible(), ui.storageNote.Text)
	}

	// снова с сервером — подпись прячется
	ui.applyStorage(client.StorageStats{})
	if ui.storageNote.Visible() {
		t.Fatal("подпись должна спрятаться")
	}
}

// TestStorageFolderRows — разбивка по папкам: строка на папку через разделитель,
// корень называется «Файлы в корне», подпись — только то, что есть.
func TestStorageFolderRows(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	_ = testApp.NewWindow("x")
	objs := storageFolderRows([]client.StorageFolder{
		{Path: "фото", Files: 2, Bytes: 1 << 20, LocFiles: 1, LocBytes: 100},
		{Path: "документы", Files: 0, Bytes: 0, LocFiles: 1, LocBytes: 10},
		{Path: "сервер", Files: 1, Bytes: 50, LocFiles: 0, LocBytes: 0},
		{Root: true, LocFiles: 1, LocBytes: 10},
	})
	if len(objs) != 7 { // 4 строки + 3 разделителя
		t.Fatalf("объектов %d, ожидалось 7", len(objs))
	}
	row, ok := objs[0].(*listRow)
	if !ok {
		t.Fatalf("первый объект — %T, ожидалась listRow", objs[0])
	}
	if row.title.Text != "фото" || row.sub.Text != "здесь 100 Б · на сервере 1.0 МБ" {
		t.Fatalf("строка «фото»: %q / %q", row.title.Text, row.sub.Text)
	}
	row = objs[2].(*listRow)
	if row.sub.Text != "только здесь · 10 Б · 1 файл" {
		t.Fatalf("строка «документы»: %q", row.sub.Text)
	}
	row = objs[4].(*listRow)
	if row.sub.Text != "только на сервере · 50 Б · 1 файл" {
		t.Fatalf("строка «сервер»: %q", row.sub.Text)
	}
	row = objs[6].(*listRow)
	if row.title.Text != "Файлы в корне" {
		t.Fatalf("строка корня: %q", row.title.Text)
	}
}
