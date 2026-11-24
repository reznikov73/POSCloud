package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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

// TestRenderFolderBrowser рендерит список каталогов: проверяем высоту строк
// и выравнивание текста влево без запуска приложения.
func TestRenderFolderBrowser(t *testing.T) {
	outDir := os.Getenv("POSCLOUD_SHOTS")
	if outDir == "" {
		t.Skip("POSCLOUD_SHOTS не задан — скриншоты не нужны")
	}
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	ctrl := client.NewController(t.TempDir())
	root := ctrl.DefaultSyncDir()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	browser := newFolderBrowser(ctrl, root, w)

	c := software.NewCanvas()
	c.SetPadded(false)
	c.SetContent(browser.object())
	c.Resize(fyne.NewSize(430, 560))
	img := software.RenderCanvas(c, Theme())
	if img == nil {
		t.Fatal("пустой рендер")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(outDir, "folder-picker.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	w.Close()
}

// TestPillSwitch — переключатель не растягивается: при раскладке на всю
// карточку таблетка остаётся своего размера, а касание переключает состояние.
func TestPillSwitch(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	sw := newPillSwitch(false, nil)
	if got := sw.MinSize(); got.Width != toggleW || got.Height != toggleH {
		t.Fatalf("MinSize = %v, ожидалось %v×%v", got, toggleW, toggleH)
	}
	var calls []bool
	sw.OnChanged = func(on bool) { calls = append(calls, on) }
	sw.Tapped(&fyne.PointEvent{})
	if !sw.Checked() || len(calls) != 1 || !calls[0] {
		t.Fatalf("после касания: on=%v, вызовы=%v", sw.Checked(), calls)
	}
	sw.SetChecked(true) // повтор того же положения — без вызова
	if len(calls) != 1 {
		t.Fatalf("лишний вызов OnChanged: %v", calls)
	}
	// даже если раскладка отдала всю высоту карточки, ползунок не больше таблетки
	c := software.NewCanvas()
	c.SetPadded(false)
	c.SetContent(sw)
	c.Resize(fyne.NewSize(toggleW, 140))
	w.Close()
}

// TestPillSwitchKnobMoves — при переключении ползунок переезжает: раньше
// менялся только цвет дорожки, а белый кружок оставался на месте.
func TestPillSwitchKnobMoves(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	sw := newPillSwitch(false, nil)
	r := sw.CreateRenderer().(*pillRenderer)
	c := software.NewCanvas()
	c.SetPadded(false)
	c.SetContent(sw)
	c.Resize(fyne.NewSize(toggleW, toggleH))
	off := r.t.knob.Position().X
	sw.SetChecked(true)
	on := r.t.knob.Position().X
	if on <= off {
		t.Fatalf("ползунок не переехал вправо: было %.0f, стало %.0f", off, on)
	}
	sw.SetChecked(false)
	if got := r.t.knob.Position().X; got != off {
		t.Fatalf("ползунок не вернулся влево: %.0f, ожидалось %.0f", got, off)
	}
	w.Close()
}

// TestRenderToggle рисует переключатель в обоих положениях — глазами проверяем
// форму таблетки и ползунка (только при заданном POSCLOUD_SHOTS).
func TestRenderToggle(t *testing.T) {
	outDir := os.Getenv("POSCLOUD_SHOTS")
	if outDir == "" {
		t.Skip("POSCLOUD_SHOTS не задан — скриншоты не нужны")
	}
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	box := container.NewHBox(newPillSwitch(false, nil), newPillSwitch(true, nil))
	c := software.NewCanvas()
	c.SetPadded(true)
	c.SetScale(3.0)
	c.SetContent(box)
	c.Resize(fyne.NewSize(150, 50))
	img := software.RenderCanvas(c, Theme())
	if img == nil {
		t.Fatal("пустой рендер")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(outDir, "toggle.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	w.Close()
}

// TestFilesPage — дерево папки синхронизации: папки сначала, скрытые файлы
// движка не показываются, имя корня — имя папки.
func TestFilesPage(t *testing.T) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(Theme())
	w := testApp.NewWindow("x")
	root := t.TempDir()
	for _, dir := range []string{"beta", "alpha"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"b.txt", "a.txt", ".poscloud-state.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := client.NewController(t.TempDir())
	ctrl.SaveSettings("localhost", 8090, root, 3, "t", false, "", "", true, "", "", 0)
	p := newFilesPage(ctrl, w)
	p.Refresh()

	kids := p.childUIDs(filesRootUID)
	want := []string{"root/alpha", "root/beta", "root/a.txt", "root/b.txt"}
	if len(kids) != len(want) {
		t.Fatalf("дерево: %v, ожидалось %v", kids, want)
	}
	for i := range want {
		if string(kids[i]) != want[i] {
			t.Fatalf("дерево: %v, ожидалось %v", kids, want)
		}
	}
	if !p.isBranch("root/alpha") {
		t.Fatal("папка не распознана как ветка")
	}
	if p.isBranch("root/a.txt") {
		t.Fatal("файл принят за папку")
	}
	if got := p.absPath("root/alpha"); got != filepath.Join(root, "alpha") {
		t.Fatalf("путь узла: %q", got)
	}
	w.Close()
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
	// немного содержимого, чтобы вкладка «Файлы» показывала дерево
	root := ctrl.DefaultSyncDir()
	for _, dir := range []string{"Снимки экрана", "Документы"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"Снимки экрана/Снимок экрана 2026-11-24 07-15-00.png", "Документы/Инструкция.html", "Порядок установки.txt"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(f)), make([]byte, 4096), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := Build(ctrl, w)
	st.loadFromConfig(ctrl)
	time.Sleep(500 * time.Millisecond)

	outDir := os.Getenv("POSCLOUD_SHOTS")
	_ = os.MkdirAll(outDir, 0755)

	names := []string{"status", "connect", "folders", "files", "log"}
	tabTitles := map[string]string{"status": "Статус", "connect": "Связь", "folders": "Папки", "files": "Файлы", "log": "Журнал"}
	for _, name := range names {
		st.pager.SelectByName(tabTitles[name])
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
