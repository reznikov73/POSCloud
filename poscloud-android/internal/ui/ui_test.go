package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"

	"poscloud-client-android/internal/client"
)

// TestBuildUISmoke строит весь интерфейс на тестовом драйвере Fyne:
// ловит паники в построении вкладок и заполнении полей из настроек.
func TestBuildUISmoke(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("POSCloud Client")
	ctrl := client.NewController(t.TempDir())
	st := Build(ctrl, w)
	if st.Content == nil {
		t.Fatal("контент интерфейса не создан")
	}
	st.refreshStatic(ctrl)
	st.loadFromConfig(ctrl)

	// поля заполняются из настроек по умолчанию
	if st.hostEntry.Text == "" || st.portEntry.Text == "" || st.dirValue == "" {
		t.Fatalf("поля подключения не заполнены: host=%q port=%q dir=%q", st.hostEntry.Text, st.portEntry.Text, st.dirValue)
	}
	if st.intervalEntry.Text == "" {
		t.Fatal("интервал не заполнен")
	}

	// даём фоновой загрузке списка папок отработать (сервера нет — путь ошибки)
	time.Sleep(500 * time.Millisecond)
	w.Close()
}

// TestFolderPickerOpens — диалог выбора папки строится без паники:
// системный SAF-диалог на Android ронял приложение, теперь свой браузер.
func TestFolderPickerOpens(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("POSCloud Client")
	ctrl := client.NewController(t.TempDir())
	st := Build(ctrl, w)
	st.pickFolder(ctrl)
	time.Sleep(100 * time.Millisecond)
	w.Close()
}

// TestSubdirs — список каталогов: только папки, без скрытых, по алфавиту.
func TestSubdirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"beta", "alpha", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := subdirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "beta"}
	if len(names) != len(want) {
		t.Fatalf("получено %v, ожидалось %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("получено %v, ожидалось %v", names, want)
		}
	}
}

// TestScreenshotRender рендерит интерфейс (фирменная светлая тема)
// в картинки — это скриншоты вкладок без запуска приложения.
func TestScreenshotRender(t *testing.T) {
	if os.Getenv("POSCLOUD_SHOTS") == "" {
		t.Skip("POSCLOUD_SHOTS не задан — скриншоты не нужны")
	}
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("POSCloud Client")
	ctrl := client.NewController(t.TempDir())
	st := Build(ctrl, w)
	st.loadFromConfig(ctrl)
	time.Sleep(500 * time.Millisecond)

	outDir := os.Getenv("POSCLOUD_SHOTS")
	_ = os.MkdirAll(outDir, 0755)

	names := []string{"status", "connect", "folders", "log"}
	for i, name := range names {
		st.tabs.Select(st.tabs.Items[i])
		time.Sleep(150 * time.Millisecond)

		c := software.NewCanvas()
		c.SetPadded(false)
		c.SetScale(1.0)
		c.SetContent(st.Content)
		c.Resize(fyne.NewSize(430, 800))
		time.Sleep(100 * time.Millisecond)
		img := software.RenderCanvas(c, Theme())
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
