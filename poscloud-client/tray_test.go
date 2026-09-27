//go:build windows

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Иконка трея собирается: статичная и все кадры анимации, а ICO содержит два размера.
func TestTrayIconsBuilt(t *testing.T) {
	buildTrayIcons()
	if len(trayStatic) < 100 {
		t.Fatal("статичная иконка трея не собрана")
	}
	if len(trayFrames) != trayFrameCount {
		t.Fatalf("кадров анимации %d, ожидалось %d", len(trayFrames), trayFrameCount)
	}
	if trayStatic[0] != 0 || trayStatic[1] != 0 || trayStatic[2] != 1 || trayStatic[3] != 0 {
		t.Fatalf("неверная сигнатура ICO: %v", trayStatic[:4])
	}
	if n := int(trayStatic[4]) + int(trayStatic[5])*256; n != 2 {
		t.Fatalf("кадров в ICO %d, ожидалось 2 (32 и 16 пикселей)", n)
	}
}

// Стрелки должны быть нарисованы акцентным цветом приложения.
func TestTrayIconUsesAccentColor(t *testing.T) {
	img := drawSyncArrows(traySuperSize, 0)
	b := img.Bounds()
	found, opaque := 0, 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := img.RGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			opaque++
			if c.R <= trayAccentR && c.G <= trayAccentG && c.B <= trayAccentB {
				found++
			}
		}
	}
	if opaque == 0 {
		t.Fatal("иконка пустая")
	}
	if found*100/opaque < 40 {
		t.Fatalf("акцентным цветом закрашено лишь %d%% пикселей", found*100/opaque)
	}
}

// Кадры поворота должны отличаться — иначе анимации не будет.
// Иконка состоит из двух одинаковых стрелок напротив друг друга, поэтому
// сравниваем поворот на 90° (при 180° картинка совпадает сама с собой).
func TestTrayFramesDiffer(t *testing.T) {
	a := drawSyncArrows(traySuperSize, 0)
	b := drawSyncArrows(traySuperSize, 90)
	bs := b.Bounds()
	same := 0
	for y := 0; y < bs.Dy(); y++ {
		for x := 0; x < bs.Dx(); x++ {
			if a.RGBAAt(x, y) == b.RGBAAt(x, y) {
				same++
			}
		}
	}
	if same == bs.Dx()*bs.Dy() {
		t.Fatal("кадры поворота на 90° полностью совпадают")
	}
}

// Предпросмотр для разработчика. В обычном прогоне тест пропускается:
//
//	POSCLOUD_ICON_PREVIEW=<каталог> go test -run TestWriteTrayPreview
func TestWriteTrayPreview(t *testing.T) {
	dir := os.Getenv("POSCLOUD_ICON_PREVIEW")
	if dir == "" {
		t.Skip("POSCLOUD_ICON_PREVIEW не задан")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, img image.Image) {
		if err := savePNG(filepath.Join(dir, name), img); err != nil {
			t.Fatal(err)
		}
	}
	super := drawSyncArrows(traySuperSize, 0)
	save("tray-32.png", downscale(super, trayIconSize))
	save("tray-16.png", downscale(super, trayIconSmall))
	save("tray-32-zoom8.png", zoom(downscale(super, trayIconSize), 8))
	for k := 0; k < trayFrameCount; k += 3 {
		deg := 360 * float64(k) / float64(trayFrameCount)
		frame := downscale(drawSyncArrows(traySuperSize, deg), trayIconSize)
		save(fmt.Sprintf("tray-frame-%02d.png", k), zoom(frame, 6))
	}
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// zoom увеличивает изображение методом «ближайшего соседа» — так удобно
// рассматривать мелкие иконки.
func zoom(src *image.RGBA, k int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*k, b.Dy()*k))
	for y := 0; y < b.Dy()*k; y++ {
		for x := 0; x < b.Dx()*k; x++ {
			dst.SetRGBA(x, y, src.RGBAAt(x/k, y/k))
		}
	}
	return dst
}
