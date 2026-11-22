package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"poscloud-client-android/internal/client"
)

// TestBuildUISmoke строит весь интерфейс на тестовом драйвере Fyne:
// ловит паники в построении вкладок и заполнении полей из настроек.
func TestBuildUISmoke(t *testing.T) {
	testApp := test.NewApp()
	w := testApp.NewWindow("POSCloud Client")
	ctrl := client.NewController(t.TempDir())
	ui := buildUI(ctrl, w)
	if ui.content == nil {
		t.Fatal("контент интерфейса не создан")
	}
	ui.refreshStatic(ctrl)
	ui.loadFromConfig(ctrl)

	// поля заполняются из настроек по умолчанию
	if ui.hostEntry.Text == "" || ui.portEntry.Text == "" || ui.dirEntry.Text == "" {
		t.Fatalf("поля подключения не заполнены: host=%q port=%q dir=%q", ui.hostEntry.Text, ui.portEntry.Text, ui.dirEntry.Text)
	}
	if ui.intervalEntry.Text == "" {
		t.Fatal("интервал не заполнен")
	}

	// даём фоновой загрузке списка папок отработать (сервера нет — путь ошибки)
	time.Sleep(500 * time.Millisecond)
	w.Close()
}

// TestScreenshotRender рендерит интерфейс (фирменная светлая тема)
// в картинки — это скриншоты вкладок без запуска приложения.
func TestScreenshotRender(t *testing.T) {
	if os.Getenv("POSCLOUD_SHOTS") == "" {
		t.Skip("POSCLOUD_SHOTS не задан — скриншоты не нужны")
	}
	testApp := test.NewApp()
	w := testApp.NewWindow("POSCloud Client")
	ctrl := client.NewController(t.TempDir())
	ui := buildUI(ctrl, w)
	ui.loadFromConfig(ctrl)
	time.Sleep(500 * time.Millisecond)

	outDir := os.Getenv("POSCLOUD_SHOTS")
	_ = os.MkdirAll(outDir, 0755)

	names := []string{"status", "connect", "folders", "log"}
	for i, name := range names {
		ui.tabs.Select(ui.tabs.Items[i])
		time.Sleep(150 * time.Millisecond)

		c := software.NewCanvas()
		c.SetPadded(false)
		c.SetScale(1.0)
		c.SetContent(ui.content)
		c.Resize(fyne.NewSize(430, 800))
		time.Sleep(100 * time.Millisecond)
		img := software.RenderCanvas(c, posTheme{base: theme.DefaultTheme()})
		if img == nil {
			t.Fatalf("вкладка %s: пустой рендер", name)
		}
		f, err := os.Create(filepath.Join(outDir, "screen-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	w.Close()
}
