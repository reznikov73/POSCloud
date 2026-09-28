//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// ---------- снимки экрана ----------
//
// Снимок сохраняется в подпапку «Снимки экрана» внутри папки синхронизации и
// уезжает на сервер обычным механизмом синхронизации. Если пользователь снял
// галочку «Отправлять снимки на сервер», файл остаётся только на этом ПК.

const shotsSubdir = "Снимки экрана"

var (
	user32S                   = windows.NewLazySystemDLL("user32.dll")
	gdi32S                    = windows.NewLazySystemDLL("gdi32.dll")
	procGetDC                 = user32S.NewProc("GetDC")
	procReleaseDC             = user32S.NewProc("ReleaseDC")
	procGetSystemMetrics      = user32S.NewProc("GetSystemMetrics")
	procSetThreadDpiAwareness = user32S.NewProc("SetThreadDpiAwarenessContext")
	procCreateCompatibleDC    = gdi32S.NewProc("CreateCompatibleDC")
	procCreateDIBSection      = gdi32S.NewProc("CreateDIBSection")
	procSelectObject          = gdi32S.NewProc("SelectObject")
	procBitBlt                = gdi32S.NewProc("BitBlt")
	procGdiFlush              = gdi32S.NewProc("GdiFlush")
	procDeleteObjectGDI       = gdi32S.NewProc("DeleteObject")
	procDeleteDC              = gdi32S.NewProc("DeleteDC")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
	srcCopy           = 0x00CC0020
	dibRGBColors      = 0
	biRGB             = 0
)

// dpiPerMonitorAwareV2 — DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (значение -4).
var dpiPerMonitorAwareV2 = ^uintptr(3)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// RegionCapture — данные для выбора области в окне приложения.
type RegionCapture struct {
	ID      string `json:"id"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Preview string `json:"preview"` // уменьшенный снимок в виде data URL
}

// ShotsState — состояние папки со снимками для интерфейса.
type ShotsState struct {
	Dir   string `json:"dir"`
	Count int    `json:"count"`
	Sync  bool   `json:"sync"`
}

type screenCapture struct {
	id      string
	img     *image.RGBA
	preview string
}

var (
	pendingShotMu sync.Mutex
	pendingShot   *screenCapture
)

// captureVirtualScreen снимает весь рабочий стол (все мониторы вместе).
// Осведомлённость о DPI включается только на время захвата и на одном потоке:
// иначе координаты окажутся виртуальными и снимок выйдет размытым.
func captureVirtualScreen() (*image.RGBA, error) {
	type result struct {
		img *image.RGBA
		err error
	}
	ch := make(chan result, 1)
	go func() {
		goruntime.LockOSThread()
		defer goruntime.UnlockOSThread()
		prev, _, _ := procSetThreadDpiAwareness.Call(dpiPerMonitorAwareV2)
		defer procSetThreadDpiAwareness.Call(prev)
		img, err := captureGDI()
		ch <- result{img, err}
	}()
	r := <-ch
	return r.img, r.err
}

func captureGDI() (*image.RGBA, error) {
	x := systemMetrics(smXVirtualScreen)
	y := systemMetrics(smYVirtualScreen)
	w := systemMetrics(smCXVirtualScreen)
	h := systemMetrics(smCYVirtualScreen)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("не удалось определить размер рабочего стола")
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("нет доступа к экрану")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("не удалось создать контекст отрисовки")
	}
	defer procDeleteDC.Call(memDC)

	// Рисуем сразу в DIB-секцию: пиксели читаются из её памяти, GetDIBits не нужен.
	bi := bitmapInfo{}
	bi.Header.Size = uint32(unsafe.Sizeof(bitmapInfoHeader{}))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h) // отрицательная высота — строки сверху вниз
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	var bits unsafe.Pointer
	hbmp, _, _ := procCreateDIBSection.Call(screenDC, uintptr(unsafe.Pointer(&bi)),
		dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmp == 0 || bits == nil {
		return nil, fmt.Errorf("не удалось создать растровое изображение")
	}
	defer procDeleteObjectGDI.Call(hbmp)

	old, _, _ := procSelectObject.Call(memDC, hbmp)
	blt, _, bltErr := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, uintptr(x), uintptr(y), srcCopy)
	procGdiFlush.Call()
	procSelectObject.Call(memDC, old)
	if blt == 0 {
		return nil, fmt.Errorf("не удалось скопировать экран (код %d)", bltErr)
	}

	src := unsafe.Slice((*byte)(bits), w*h*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i+3 < len(src); i += 4 {
		img.Pix[i] = src[i+2] // BGRA -> RGBA
		img.Pix[i+1] = src[i+1]
		img.Pix[i+2] = src[i]
		img.Pix[i+3] = 0xff
	}
	return img, nil
}

func systemMetrics(index int) int {
	v, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(int32(uint32(v)))
}

// ---------- сохранение ----------

// shotName формирует имя файла снимка.
func shotName(t time.Time) string {
	return "Снимок экрана " + t.Format("2006-01-02 15-04-05") + ".png"
}

// shotDir выбирает каталог для снимков: папка синхронизации или локальная.
func (a *App) shotDir() string {
	a.mu.Lock()
	syncDir, syncOn := a.localDir, a.shotSync
	a.mu.Unlock()
	if syncOn && syncDir != "" {
		return filepath.Join(syncDir, shotsSubdir)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Pictures", "POSCloud")
}

// saveShot сохраняет снимок и возвращает путь к файлу.
func (a *App) saveShot(img image.Image) (string, error) {
	dir := a.shotDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, shotName(time.Now()))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}

	a.log("снимок экрана сохранён: " + path)
	go a.syncOnce(false)
	a.notifyShot(path)
	return path, nil
}

// notifyShot сообщает пользователю, куда попал снимок и уедет ли он на сервер.
func (a *App) notifyShot(path string) {
	if a.ctx == nil {
		return
	}
	a.mu.Lock()
	syncOn := a.shotSync
	a.mu.Unlock()

	body := "Сохранён: " + path
	if syncOn {
		body += "\nФайл будет отправлен на сервер синхронизацией."
	} else {
		body += "\nОтправка на сервер выключена — файл остаётся на этом компьютере."
	}
	_ = runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID:    "poscloud-screenshot",
		Title: "Снимок экрана",
		Body:  body,
	})
}

// ---------- привязки для интерфейса ----------

// CaptureFullScreen снимает весь экран и сохраняет снимок.
func (a *App) CaptureFullScreen() (string, error) {
	img, err := captureVirtualScreen()
	if err != nil {
		return "", err
	}
	return a.saveShot(img)
}

// StartRegionCapture снимает экран, показывает окно и отдаёт предпросмотр для выбора области.
func (a *App) StartRegionCapture() (RegionCapture, error) {
	img, err := captureVirtualScreen()
	if err != nil {
		return RegionCapture{}, err
	}
	b := img.Bounds()

	preview := scaleShot(img, previewMaxWidth)
	var buf bytes.Buffer
	if err := png.Encode(&buf, preview); err != nil {
		return RegionCapture{}, err
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	cap := &screenCapture{
		id:      fmt.Sprintf("%d", time.Now().UnixNano()),
		img:     img,
		preview: dataURL,
	}
	pendingShotMu.Lock()
	pendingShot = cap
	pendingShotMu.Unlock()

	a.trayShowWindow() // окно нужно, чтобы выделить область
	return RegionCapture{ID: cap.id, Width: b.Dx(), Height: b.Dy(), Preview: dataURL}, nil
}

// PendingRegionCapture возвращает начатый выбор области (окно опрашивает его).
func (a *App) PendingRegionCapture() *RegionCapture {
	pendingShotMu.Lock()
	defer pendingShotMu.Unlock()
	if pendingShot == nil {
		return nil
	}
	b := pendingShot.img.Bounds()
	return &RegionCapture{
		ID:      pendingShot.id,
		Width:   b.Dx(),
		Height:  b.Dy(),
		Preview: pendingShot.preview,
	}
}

// FinishRegionCapture вырезает выбранную область и сохраняет её.
// Координаты приходят долями от 0 до 1 — так масштаб окна ни на что не влияет.
func (a *App) FinishRegionCapture(id string, x, y, w, h float64) (string, error) {
	pendingShotMu.Lock()
	cap := pendingShot
	pendingShot = nil
	pendingShotMu.Unlock()

	if cap == nil || cap.id != id {
		return "", fmt.Errorf("выбор области уже неактуален")
	}
	b := cap.img.Bounds()
	rect, ok := cropRect(b.Dx(), b.Dy(), x, y, w, h)
	if !ok {
		return "", fmt.Errorf("область слишком мала")
	}
	return a.saveShot(cap.img.SubImage(rect))
}

// CancelRegionCapture отменяет выбор области.
func (a *App) CancelRegionCapture(id string) {
	pendingShotMu.Lock()
	if pendingShot != nil && (id == "" || pendingShot.id == id) {
		pendingShot = nil
	}
	pendingShotMu.Unlock()
}

// OpenShotsFolder открывает папку со снимками в проводнике.
func (a *App) OpenShotsFolder() string {
	dir := a.shotDir()
	_ = os.MkdirAll(dir, 0755)
	openInExplorer(dir)
	return dir
}

// ShotsInfo возвращает состояние папки со снимками.
func (a *App) ShotsInfo() ShotsState {
	dir := a.shotDir()
	count := 0
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
				count++
			}
		}
	}
	a.mu.Lock()
	on := a.shotSync
	a.mu.Unlock()
	return ShotsState{Dir: dir, Count: count, Sync: on}
}

// SetScreenshotSync включает и выключает отправку снимков на сервер.
func (a *App) SetScreenshotSync(on bool) ShotsState {
	a.mu.Lock()
	a.shotSync = on
	a.mu.Unlock()
	a.persistConfig()
	return a.ShotsInfo()
}

// ---------- вспомогательное ----------

const previewMaxWidth = 1400

// cropRect переводит доли (0..1) в прямоугольник пикселей.
func cropRect(w, h int, x, y, cw, ch float64) (image.Rectangle, bool) {
	cl := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	x, y, cw, ch = cl(x), cl(y), cl(cw), cl(ch)
	if w <= 0 || h <= 0 || cw <= 0 || ch <= 0 {
		return image.Rectangle{}, false
	}
	x0 := int(float64(w) * x)
	y0 := int(float64(h) * y)
	x1 := int(float64(w) * (x + cw))
	y1 := int(float64(h) * (y + ch))
	if x1 > w {
		x1 = w
	}
	if y1 > h {
		y1 = h
	}
	if x1-x0 < 8 || y1-y0 < 8 {
		return image.Rectangle{}, false
	}
	return image.Rect(x0, y0, x1, y1), true
}

// scaleShot уменьшает изображение до заданной ширины, сохраняя пропорции.
func scaleShot(src *image.RGBA, maxW int) *image.RGBA {
	b := src.Bounds()
	if maxW <= 0 || b.Dx() <= maxW {
		return src
	}
	dw := maxW
	dh := b.Dy() * maxW / b.Dx()
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	sx := float64(b.Dx()) / float64(dw)
	sy := float64(b.Dy()) / float64(dh)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var r, g, bl, a, cnt float64
			for yy := int(float64(y) * sy); yy < int(float64(y+1)*sy); yy++ {
				for xx := int(float64(x) * sx); xx < int(float64(x+1)*sx); xx++ {
					c := src.RGBAAt(xx, yy)
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
				R: clampByte(r / a),
				G: clampByte(g / a),
				B: clampByte(bl / a),
				A: clampByte(a / cnt * 255),
			})
		}
	}
	return dst
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
