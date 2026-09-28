//go:build windows

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// Имя файла снимка содержит дату и время и имеет расширение .png.
func TestShotName(t *testing.T) {
	got := shotName(time.Date(2026, 9, 28, 9, 0, 5, 0, time.Local))
	want := "Снимок экрана 2026-09-28 09-00-05.png"
	if got != want {
		t.Fatalf("получено %q, ожидалось %q", got, want)
	}
}

// Доли переводятся в пиксели, выход за границы обрезается, мелкая область отклоняется.
func TestCropRect(t *testing.T) {
	if r, ok := cropRect(1920, 1080, 0.5, 0.5, 0.5, 0.5); !ok || r != image.Rect(960, 540, 1920, 1080) {
		t.Fatalf("обычный случай разобран неверно: %v %v", r, ok)
	}
	if r, ok := cropRect(1920, 1080, -0.5, -0.5, 2, 2); !ok || r != image.Rect(0, 0, 1920, 1080) {
		t.Fatalf("выход за границы не обрезан: %v %v", r, ok)
	}
	if _, ok := cropRect(1920, 1080, 0.5, 0.5, 0, 0.5); ok {
		t.Fatal("нулевая ширина должна отклоняться")
	}
	if _, ok := cropRect(1920, 1080, 0, 0, 0.001, 0.001); ok {
		t.Fatal("слишком мелкая область должна отклоняться")
	}
}

// Каталог снимков зависит от галочки «отправлять на сервер».
func TestShotDirSelection(t *testing.T) {
	a := newTestApp(t, "")
	dir := t.TempDir()
	a.localDir = dir

	a.shotSync = true
	want := filepath.Join(dir, shotsSubdir)
	if got := a.shotDir(); got != want {
		t.Fatalf("при включённой отправке ожидался %q, получено %q", want, got)
	}

	a.shotSync = false
	home, _ := os.UserHomeDir()
	want = filepath.Join(home, "Pictures", "POSCloud")
	if got := a.shotDir(); got != want {
		t.Fatalf("при выключенной отправке ожидался %q, получено %q", want, got)
	}
}

// Предпросмотр уменьшается пропорционально, маленькое изображение не растягивается.
func TestScaleShot(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3000, 1500))
	got := scaleShot(src, 1200)
	if got.Bounds().Dx() != 1200 || got.Bounds().Dy() != 600 {
		t.Fatalf("получен %v, ожидалось 1200x600", got.Bounds())
	}
	if same := scaleShot(src, 4000); same.Bounds().Dx() != 3000 {
		t.Fatal("увеличение не требуется — изображение должно остаться прежним")
	}
}

// Настоящий захват экрана: пиксели читаются, картинка не пустая.
func TestCaptureScreenLive(t *testing.T) {
	img, err := captureVirtualScreen()
	if err != nil {
		t.Skipf("захват экрана недоступен в этом сеансе: %v", err)
	}
	b := img.Bounds()
	if b.Dx() < 100 || b.Dy() < 100 {
		t.Fatalf("подозрительно маленький снимок: %v", b)
	}
	colors := map[uint32]bool{}
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			px := img.RGBAAt(x, y)
			colors[uint32(px.R)<<16|uint32(px.G)<<8|uint32(px.B)] = true
		}
	}
	if len(colors) < 10 {
		t.Fatalf("на снимке всего %d цветов — похоже, пиксели не прочитаны", len(colors))
	}
	t.Logf("снимок %dx%d, различных цветов %d", b.Dx(), b.Dy(), len(colors))
}

// Полный путь снимка: захват, сохранение файла, пригодность файла.
func TestCaptureFullScreenSavesFile(t *testing.T) {
	a := newTestApp(t, "")
	dir := t.TempDir()
	a.localDir = dir
	a.shotSync = true

	path, err := a.CaptureFullScreen()
	if err != nil {
		t.Skipf("захват экрана недоступен в этом сеансе: %v", err)
	}
	if want := filepath.Join(dir, shotsSubdir); !strings.HasPrefix(path, want) {
		t.Fatalf("снимок сохранён не в папку снимков: %q, ожидался префикс %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("файл снимка не создан: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("файл снимка пуст")
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("файл не читается как PNG: %v", err)
	}
	if cfg.Width < 100 || cfg.Height < 100 {
		t.Fatalf("неожиданный размер снимка: %dx%d", cfg.Width, cfg.Height)
	}
	t.Logf("снимок %dx%d сохранён: %s", cfg.Width, cfg.Height, path)
}

// Прямоугольник окна пересекается со снятой областью экрана (учёт второго монитора).
func TestWindowCropRect(t *testing.T) {
	screen := image.Rect(-1920, 0, 1920, 1080) // второй монитор слева
	if r, ok := windowCropRect(screen, [4]int{-800, 100, -200, 500}); !ok || r != image.Rect(-800, 100, -200, 500) {
		t.Fatalf("обычный случай: %v %v", r, ok)
	}
	// окно выходит за снятую область — обрезаем
	if r, ok := windowCropRect(screen, [4]int{-2000, -50, -1000, 200}); !ok || r != image.Rect(-1920, 0, -1000, 200) {
		t.Fatalf("обрезка не сработала: %v %v", r, ok)
	}
	// окно целиком вне области
	if _, ok := windowCropRect(screen, [4]int{5000, 5000, 6000, 6000}); ok {
		t.Fatal("окно вне области должно отклоняться")
	}
}

// Изображение для буфера обмена: заголовок CF_DIB и пиксели снизу вверх в BGRA.
func TestDibBytes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for x := 0; x < 3; x++ {
		img.Set(x, 0, color.RGBA{R: 255, A: 255}) // верх — красный
		img.Set(x, 1, color.RGBA{B: 255, A: 255}) // низ — синий
	}
	buf := dibBytes(img)
	if len(buf) != 40+3*2*4 {
		t.Fatalf("размер буфера %d, ожидалось %d", len(buf), 40+3*2*4)
	}
	le := func(off int) int {
		return int(buf[off]) | int(buf[off+1])<<8 | int(buf[off+2])<<16 | int(buf[off+3])<<24
	}
	if le(0) != 40 || le(4) != 3 || le(8) != 2 {
		t.Fatalf("заголовок неверен: размер=%d ширина=%d высота=%d", le(0), le(4), le(8))
	}
	if int(buf[14])|int(buf[15])<<8 != 32 {
		t.Fatalf("глубина цвета не 32: %d", int(buf[14])|int(buf[15])<<8)
	}
	first := buf[40:44]
	if first[0] != 255 || first[1] != 0 || first[2] != 0 {
		t.Fatalf("первая строка должна быть нижней (синей) в порядке BGRA: %v", first)
	}
	last := buf[40+3*4 : 40+3*4+4]
	if last[2] != 255 || last[0] != 0 {
		t.Fatalf("последняя строка должна быть верхней (красной): %v", last)
	}
}

// Комбинации горячих клавиш описаны полностью и не пересекаются.
func TestHotkeySpecs(t *testing.T) {
	specs := hotkeySpecs()
	if len(specs) != 3 {
		t.Fatalf("комбинаций %d, ожидалось 3", len(specs))
	}
	ids := map[uintptr]bool{}
	vks := map[uintptr]bool{}
	for _, s := range specs {
		if s.run == nil || s.id == 0 || s.vk == 0 {
			t.Fatalf("неполное описание: %+v", s)
		}
		if ids[s.id] || vks[s.vk] {
			t.Fatalf("повтор идентификатора или клавиши: %+v", s)
		}
		ids[s.id] = true
		vks[s.vk] = true
	}
}

// Снимок активного окна: размер должен совпасть с прямоугольником самого окна
// (оба измерения делаются на потоке с включённой осведомлённостью о DPI).
func TestCaptureActiveWindowLive(t *testing.T) {
	img, err := captureActiveWindow()
	if err != nil {
		t.Skipf("снимок активного окна недоступен: %v", err)
	}

	var wantW, wantH int
	measErr := withDpiAwareness(func() error {
		hwnd, _, _ := procGetForegroundWindow.Call()
		if hwnd == 0 {
			return fmt.Errorf("активное окно не определено")
		}
		var r winRect
		if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
			return fmt.Errorf("не удалось получить прямоугольник окна")
		}
		wantW = int(r.Right - r.Left)
		wantH = int(r.Bottom - r.Top)
		return nil
	})
	if measErr != nil {
		t.Skipf("не удалось измерить активное окно: %v", measErr)
	}

	b := img.Bounds()
	if b.Dx() < 8 || b.Dy() < 8 {
		t.Fatalf("подозрительно маленький снимок окна: %v", b)
	}
	if b.Dx() != wantW || b.Dy() != wantH {
		t.Fatalf("размер снимка %dx%d не совпал с окном %dx%d", b.Dx(), b.Dy(), wantW, wantH)
	}
	t.Logf("снимок активного окна %dx%d", b.Dx(), b.Dy())
}
