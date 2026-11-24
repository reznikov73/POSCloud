package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"

	"poscloud-client-android/internal/client"
)

// TestRenderStorageDialog рендерит содержимое диалога «Хранилище по папкам»
// (только при заданном POSCLOUD_SHOTS): проверяем, что подписи строк с
// размерами влезают в одну строку на ширине телефона.
func TestRenderStorageDialog(t *testing.T) {
	outDir := os.Getenv("POSCLOUD_SHOTS")
	if outDir == "" {
		t.Skip("POSCLOUD_SHOTS не задан — скриншоты не нужны")
	}
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	_ = testApp.NewWindow("x")
	st := &client.StorageStats{LocalFiles: 42, LocalBytes: 215 << 20, ServerFiles: 210, ServerBytes: 3 << 30}
	st.Folders = []client.StorageFolder{
		{Path: "Снимки экрана", Files: 150, Bytes: 2 << 30, LocFiles: 30, LocBytes: 200 << 20},
		{Path: "Документы", Files: 60, Bytes: 1 << 30, LocFiles: 12, LocBytes: 15 << 20},
		{Path: "Музыка", Files: 0, Bytes: 0, LocFiles: 2, LocBytes: 7 << 20},
		{Path: "Архив", Files: 20, Bytes: 500 << 20, LocFiles: 0, LocBytes: 0},
		{Root: true, LocFiles: 1, LocBytes: 10},
	}
	c := software.NewCanvas()
	c.SetPadded(false)
	c.SetContent(storageDialogContent(st))
	c.Resize(fyne.NewSize(360, 420))
	img := software.RenderCanvas(c, Theme())
	if img == nil {
		t.Fatal("пустой рендер")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(outDir, "dialog-storage.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}
