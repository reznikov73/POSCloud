//go:build windows

package main

import (
	"image"
	"image/color"
	"math"
	"os"
	"sync/atomic"
	"time"

	systray "github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- иконка трея (ICO генерируется программно, бинарных файлов нет) ---

const trayIconSize = 32

var (
	trayStatic []byte
	trayFrames [][]byte
)

// buildTrayIcons готовит статичную иконку и кадры для анимации (8 поворотов).
func buildTrayIcons() {
	trayStatic = makeIconICO(drawArrows(trayIconSize, 0))
	trayFrames = make([][]byte, 0, 8)
	for k := 0; k < 8; k++ {
		trayFrames = append(trayFrames, makeIconICO(drawArrows(trayIconSize, float64(k)*45)))
	}
}

// drawArrows рисует две круговые стрелки, повёрнутые на deg градусов.
func drawArrows(size int, deg float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := (float64(size) - 1) / 2
	rOut := float64(size) * 0.46
	rIn := rOut * 0.62
	rMid := (rOut + rIn) / 2
	col := color.RGBA{R: 0x2e, G: 0xa0, B: 0x43, A: 0xff}
	const pi = math.Pi
	shift := deg * pi / 180
	arcs := [2][2]float64{{0.15 * pi, 0.85 * pi}, {1.15 * pi, 1.85 * pi}}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - c
			dy := float64(y) - c
			d := math.Hypot(dx, dy)
			if d < rIn || d > rOut {
				continue
			}
			a := math.Atan2(dy, dx) - shift
			for a < 0 {
				a += 2 * pi
			}
			for a >= 2*pi {
				a -= 2 * pi
			}
			for _, ar := range arcs {
				if a >= ar[0] && a <= ar[1] {
					img.SetRGBA(x, y, col)
					break
				}
			}
		}
	}
	for _, ar := range arcs {
		end := ar[1] + shift
		ax := c + rMid*math.Cos(end)
		ay := c + rMid*math.Sin(end)
		tx := c + rMid*math.Cos(end+0.35)
		ty := c + rMid*math.Sin(end+0.35)
		px := -math.Sin(end)
		py := math.Cos(end)
		hw := (rOut - rIn) * 0.95
		fillTri(img, tx, ty, ax+px*hw, ay+py*hw, ax-px*hw, ay-py*hw, col)
	}
	return img
}

func fillTri(img *image.RGBA, x1, y1, x2, y2, x3, y3 float64, col color.RGBA) {
	minx := int(math.Floor(math.Min(x1, math.Min(x2, x3))))
	maxx := int(math.Ceil(math.Max(x1, math.Max(x2, x3))))
	miny := int(math.Floor(math.Min(y1, math.Min(y2, y3))))
	maxy := int(math.Ceil(math.Max(y1, math.Max(y2, y3))))
	b := img.Bounds()
	sign := func(ax, ay, bx, by, cx, cy float64) float64 {
		return (ax-cx)*(by-cy) - (bx-cx)*(ay-cy)
	}
	for y := miny; y <= maxy; y++ {
		for x := minx; x <= maxx; x++ {
			if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			d1 := sign(px, py, x1, y1, x2, y2)
			d2 := sign(px, py, x2, y2, x3, y3)
			d3 := sign(px, py, x3, y3, x1, y1)
			hasNeg := d1 < 0 || d2 < 0 || d3 < 0
			hasPos := d1 > 0 || d2 > 0 || d3 > 0
			if !(hasNeg && hasPos) {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

// makeIconICO собирает ICO (один кадр, 32bpp BMP) из RGBA-картинки.
func makeIconICO(img *image.RGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	xorStride := w * 4
	andStride := ((w + 31) / 32) * 4
	imgSize := 40 + xorStride*h + andStride*h
	buf := make([]byte, 22+imgSize)
	le16 := func(off int, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
	}
	le32 := func(off int, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	le16(0, 0)
	le16(2, 1)
	le16(4, 1)
	buf[6] = byte(w)
	buf[7] = byte(h)
	le16(10, 1)
	le16(12, 32)
	le32(14, imgSize)
	le32(18, 22)
	le32(22, 40)
	le32(26, w)
	le32(30, h*2)
	le16(34, 1)
	le16(36, 32)
	le32(42, xorStride*h)
	off := 22 + 40
	for y := h - 1; y >= 0; y-- {
		row := off + (h-1-y)*xorStride
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			p := row + x*4
			buf[p] = c.B
			buf[p+1] = c.G
			buf[p+2] = c.R
			buf[p+3] = c.A
		}
	}
	return buf
}

// trayInit настраивает иконку и меню трея (вызывается, когда трей готов).
func (a *App) trayInit(title string) {
	systray.SetIcon(trayStatic)
	systray.SetTooltip(title)
	mOpen := systray.AddMenuItem("Открыть окно настроек", "Показать окно")
	mOpen.Click(func() { a.trayShowWindow() })
	mQuit := systray.AddMenuItem("Закрыть программу", "Выйти из программы")
	mQuit.Click(func() { a.trayQuit() })
	systray.SetOnClick(func(menu systray.IMenu) { a.trayShowWindow() })
	systray.SetOnDClick(func(menu systray.IMenu) { a.trayShowWindow() })
	systray.SetOnRClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })
	startTrayAnimation()
}

func (a *App) trayShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

// trayQuitting — признак, что выход запрошен из трея (чтобы OnBeforeClose не блокировал закрытие).
var trayQuitting atomic.Bool

// trayQuitRequested сообщает, что выход запрошен из трея.
func trayQuitRequested() bool { return trayQuitting.Load() }

func (a *App) trayQuit() {
	trayQuitting.Store(true)
	ctx := a.ctx
	go func() {
		if ctx != nil {
			runtime.Quit(ctx)
		}
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
}

// startTrayAnimation крутит стрелки, пока идут передачи файлов.
func startTrayAnimation() {
	go func() {
		i := 0
		lastActive := false
		for {
			time.Sleep(150 * time.Millisecond)
			act := transfers.active()
			if act {
				i = (i + 1) % len(trayFrames)
				systray.SetIcon(trayFrames[i])
			} else if lastActive {
				i = 0
				systray.SetIcon(trayStatic)
			}
			lastActive = act
		}
	}()
}
