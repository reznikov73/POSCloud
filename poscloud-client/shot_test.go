//go:build windows

package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
