//go:build windows

package main

import (
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	systray "github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- иконка трея (ICO генерируется программно, бинарных файлов нет) ---
//
// Иконка рисуется в увеличенном размере и затем уменьшается с усреднением,
// поэтому края получаются сглаженными и не «мылят». Акцентный цвет стрелок
// задаётся константами в main.go: у клиента зелёный, у сервера красный.

const (
	trayIconSize   = 32                     // основной размер иконки в трее
	trayIconSmall  = 16                     // уменьшенный размер для мелкого масштаба
	traySuperSize  = 128                    // размер, в котором ведётся отрисовка
	trayFrameCount = 12                     // число кадров анимации
	trayFrameDelay = 100 * time.Millisecond // шаг анимации
)

// Акцентный цвет стрелок; значения заданы в main.go и различаются у клиента и сервера.
var trayAccent = color.RGBA{R: trayAccentR, G: trayAccentG, B: trayAccentB, A: 0xff}

var (
	trayStatic []byte
	trayFrames [][]byte
)

// buildTrayIcons готовит статичную иконку и кадры для анимации.
func buildTrayIcons() {
	trayStatic = trayICO(0)
	trayFrames = make([][]byte, 0, trayFrameCount)
	for k := 0; k < trayFrameCount; k++ {
		trayFrames = append(trayFrames, trayICO(360*float64(k)/float64(trayFrameCount)))
	}
}

// trayICO собирает ICO (кадры 32 и 16 пикселей) для поворота на deg градусов.
func trayICO(deg float64) []byte {
	super := drawSyncArrows(traySuperSize, deg)
	return makeIconICO(downscale(super, trayIconSize), downscale(super, trayIconSmall))
}

// drawSyncArrows рисует две круговые стрелки с заострёнными наконечниками,
// повёрнутые на deg градусов. Обе стрелки закручены по часовой стрелке.
func drawSyncArrows(size int, deg float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := (float64(size) - 1) / 2
	rOut := float64(size) * 0.42
	rIn := rOut * 0.62
	rMid := (rOut + rIn) / 2
	hw := (rOut - rIn) / 2 // половина толщины дуги
	headLen := hw * 1.9    // длина наконечника
	headHalf := hw * 1.35  // половина ширины наконечника
	shift := deg * math.Pi / 180

	// Две дуги по 100° с широкими промежутками: при мелком размере стрелки
	// не должны сливаться в сплошное кольцо. Наконечник — на конце дуги.
	arcs := [2][2]float64{{205, 305}, {25, 125}}

	for _, ar := range arcs {
		a0 := ar[0]*math.Pi/180 + shift
		a1 := ar[1]*math.Pi/180 + shift

		// тело дуги
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dx := float64(x) - c
				dy := float64(y) - c
				d := math.Hypot(dx, dy)
				if d < rIn || d > rOut {
					continue
				}
				if angleBetween(math.Atan2(dy, dx), a0, a1) {
					img.SetRGBA(x, y, trayAccent)
				}
			}
		}

		// скруглённое начало дуги
		disc(img, c+rMid*math.Cos(a0), c+rMid*math.Sin(a0), hw, trayAccent)

		// заострённый наконечник, направленный по ходу дуги
		px, py := c+rMid*math.Cos(a1), c+rMid*math.Sin(a1)
		tx, ty := -math.Sin(a1), math.Cos(a1)
		nx, ny := math.Cos(a1), math.Sin(a1)
		fillTri(img,
			px+tx*headLen, py+ty*headLen,
			px+nx*headHalf, py+ny*headHalf,
			px-nx*headHalf, py-ny*headHalf,
			trayAccent)
	}

	applyVerticalShade(img, trayAccent)
	return img
}

// angleBetween сообщает, попадает ли угол a в дугу от a0 до a1 (по возрастанию).
func angleBetween(a, a0, a1 float64) bool {
	d := math.Mod(a-a0, 2*math.Pi)
	if d < 0 {
		d += 2 * math.Pi
	}
	return d <= a1-a0
}

// applyVerticalShade накладывает лёгкий вертикальный градиент, чтобы иконка была «сочнее».
func applyVerticalShade(img *image.RGBA, base color.RGBA) {
	b := img.Bounds()
	if b.Dy() == 0 {
		return
	}
	for y := 0; y < b.Dy(); y++ {
		f := 1.22 - 0.55*float64(y)/float64(b.Dy())
		for x := 0; x < b.Dx(); x++ {
			c := img.RGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{
				R: shade(base.R, f),
				G: shade(base.G, f),
				B: shade(base.B, f),
				A: c.A,
			})
		}
	}
}

func shade(v uint8, f float64) uint8 {
	x := float64(v) * f
	if x > 255 {
		x = 255
	}
	if x < 0 {
		x = 0
	}
	return uint8(x)
}

// disc закрашивает круг радиусом r.
func disc(img *image.RGBA, cx, cy, r float64, col color.RGBA) {
	b := img.Bounds()
	for y := int(math.Floor(cy - r)); y <= int(math.Ceil(cy+r)); y++ {
		for x := int(math.Floor(cx - r)); x <= int(math.Ceil(cx+r)); x++ {
			if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, col)
			}
		}
	}
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

// downscale уменьшает изображение усреднением с учётом прозрачности,
// поэтому края остаются гладкими и не «сереют».
func downscale(src *image.RGBA, n int) *image.RGBA {
	s := src.Bounds().Dx()
	dst := image.NewRGBA(image.Rect(0, 0, n, n))
	if s == 0 || n <= 0 {
		return dst
	}
	step := float64(s) / float64(n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var r, g, bl, a float64
			var cnt float64
			for sy := int(float64(y) * step); sy < int(float64(y+1)*step); sy++ {
				for sx := int(float64(x) * step); sx < int(float64(x+1)*step); sx++ {
					c := src.RGBAAt(sx, sy)
					af := float64(c.A) / 255
					r += float64(c.R) * af
					g += float64(c.G) * af
					bl += float64(c.B) * af
					a += af
					cnt++
				}
			}
			if cnt == 0 || a == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(clamp255(r / a)),
				G: uint8(clamp255(g / a)),
				B: uint8(clamp255(bl / a)),
				A: uint8(clamp255(a / cnt * 255)),
			})
		}
	}
	return dst
}

func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// makeIconICO собирает ICO из переданных изображений (32bpp, с маской прозрачности).
func makeIconICO(imgs ...*image.RGBA) []byte {
	n := len(imgs)
	if n == 0 {
		return nil
	}
	frames := make([][]byte, n)
	offsets := make([]int, n)
	total := 6 + 16*n
	for i, img := range imgs {
		frames[i] = icoFrame(img)
		offsets[i] = total
		total += len(frames[i])
	}

	buf := make([]byte, total)
	le16 := func(off, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
	}
	le32 := func(off, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	le16(0, 0)
	le16(2, 1) // тип: иконка
	le16(4, n)

	for i, img := range imgs {
		w := img.Bounds().Dx()
		h := img.Bounds().Dy()
		e := 6 + 16*i
		if w >= 256 {
			buf[e] = 0
		} else {
			buf[e] = byte(w)
		}
		if h >= 256 {
			buf[e+1] = 0
		} else {
			buf[e+1] = byte(h)
		}
		buf[e+2] = 0
		buf[e+3] = 0
		le16(e+4, 1)
		le16(e+6, 32)
		le32(e+8, len(frames[i]))
		le32(e+12, offsets[i])
		copy(buf[offsets[i]:], frames[i])
	}
	return buf
}

// icoFrame собирает один кадр ICO: BITMAPINFOHEADER + пиксели + маска прозрачности.
func icoFrame(img *image.RGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	xorStride := w * 4
	andStride := ((w + 31) / 32) * 4
	buf := make([]byte, 40+xorStride*h+andStride*h)

	le16 := func(off, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
	}
	le32 := func(off, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	le32(0, 40)
	le32(4, w)
	le32(8, h*2)
	le16(12, 1)
	le16(14, 32)
	le32(20, xorStride*h)

	for y := h - 1; y >= 0; y-- {
		row := 40 + (h-1-y)*xorStride
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			p := row + x*4
			buf[p] = c.B
			buf[p+1] = c.G
			buf[p+2] = c.R
			buf[p+3] = c.A
		}
	}

	// Маска: бит выставлен там, где пиксель прозрачный.
	maskOff := 40 + xorStride*h
	for y := h - 1; y >= 0; y-- {
		row := maskOff + (h-1-y)*andStride
		for x := 0; x < w; x++ {
			if img.RGBAAt(x, y).A < 128 {
				buf[row+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	return buf
}

// trayInit настраивает иконку и меню трея (вызывается, когда трей готов).
func (a *App) trayInit(title string) {
	systray.SetIcon(trayStatic)
	systray.SetTooltip(title)
	a.buildTrayMenu()
	systray.SetOnClick(func(menu systray.IMenu) { a.trayShowWindow() })
	systray.SetOnDClick(func(menu systray.IMenu) { a.trayShowWindow() })
	systray.SetOnRClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })
	startTrayAnimation()
	a.startTrayMenuRefresh()
}

// startTrayMenuRefresh раз в секунду обновляет подписи в шапке меню.
func (a *App) startTrayMenuRefresh() {
	go func() {
		for {
			time.Sleep(time.Second)
			a.updateTrayMenu()
		}
	}()
}

// openInExplorer открывает каталог или файл в проводнике.
func openInExplorer(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("explorer", path).Start()
}

// showAbout показывает информационное окно.
func (a *App) showAbout(title, text string) {
	if a.ctx == nil {
		return
	}
	_, _ = runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: text,
	})
}

// copyText кладёт текст в буфер обмена.
func (a *App) copyText(text string) {
	if a.ctx == nil || text == "" {
		return
	}
	_ = runtime.ClipboardSetText(a.ctx, text)
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
			time.Sleep(trayFrameDelay)
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
