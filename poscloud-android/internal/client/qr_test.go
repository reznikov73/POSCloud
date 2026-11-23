package client

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// renderQR draws a QR code with text (white background, black modules).
func renderQR(t *testing.T, text string) image.Image {
	t.Helper()
	writer := qrcode.NewQRCodeWriter()
	mat, err := writer.Encode(text, gozxing.BarcodeFormat_QR_CODE, 512, 512, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			if mat.Get(x, y) {
				img.Set(x, y, color.Black)
			} else {
				img.Set(x, y, color.White)
			}
		}
	}
	return img
}

func TestDecodeQRImage(t *testing.T) {
	code := "poscloud://connect?v=1&host=192.168.1.42&port=8090&tls=0&code=pcp_example123"
	got, err := DecodeQRImage(renderQR(t, code))
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Fatalf("decoded wrong text: %q", got)
	}
}

func TestDecodeQRBytes(t *testing.T) {
	code := "poscloud://connect?v=1&host=example.local&port=8090&tls=1&fp=AA:BB&code=pcp_x"
	// use PNG encoding via image/png
	var buf bytes.Buffer
	if err := pngEncode(renderQR(t, code), &buf); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeQRBytes(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Fatalf("decoded wrong text: %q", got)
	}
}

func TestDecodeQRNotFound(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.White)
		}
	}
	if _, err := DecodeQRImage(img); err == nil {
		t.Fatal("expected a 'QR not found' error on an empty image")
	}
}

func TestDecodeQRReader(t *testing.T) {
	code := "poscloud://connect?v=1&host=10.0.0.5&port=9000&tls=0&code=pcp_reader"
	var buf bytes.Buffer
	if err := pngEncode(renderQR(t, code), &buf); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeQRReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Fatalf("decoded wrong text: %q", got)
	}
}

// pngEncode encodes an image to PNG via the standard image/png.
func pngEncode(img image.Image, buf *bytes.Buffer) error {
	return png.Encode(buf, img)
}

// blurBox усредняет пиксели в окне radius — имитация смазанного снимка.
func blurBox(src image.Image, radius int) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			var r, g, bl, n int
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					xx, yy := x+dx, y+dy
					if xx < 0 || yy < 0 || xx >= b.Dx() || yy >= b.Dy() {
						continue
					}
					cr, cg, cb, _ := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
					r += int(cr >> 8)
					g += int(cg >> 8)
					bl += int(cb >> 8)
					n++
				}
			}
			dst.Set(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return dst
}

// TestDecodeQRImageBlurred — смазанный снимок: помогает усиление резкости.
func TestDecodeQRImageBlurred(t *testing.T) {
	code := "poscloud://connect?v=1&host=192.168.1.9&port=8090&tls=0&code=pcp_blur"
	got, err := DecodeQRImage(blurBox(renderQR(t, code), 2))
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Fatalf("неверный текст: %q", got)
	}
}

// TestDecodeQRImageRotated — снимок с камеры часто приходит повёрнутым:
// EXIF-ориентацию image.Decode не применяет, декодер обязан перебрать повороты.
func TestDecodeQRImageRotated(t *testing.T) {
	code := "poscloud://connect?v=1&host=192.168.1.42&port=8090&tls=0&code=pcp_rot"
	base := renderQR(t, code)
	for _, angle := range []int{90, 180, 270} {
		got, err := DecodeQRImage(rotate(base, angle))
		if err != nil {
			t.Fatalf("поворот %d°: %v", angle, err)
		}
		if got != code {
			t.Fatalf("поворот %d°: неверный текст %q", angle, got)
		}
	}
}

// TestDecodeQRImagePhotoLike — «фотография»: код занимает малую часть кадра,
// фон неровный, снимок больше предела уменьшения.
func TestDecodeQRImagePhotoLike(t *testing.T) {
	code := "poscloud://connect?v=1&host=10.1.2.3&port=8090&tls=1&fp=AB&code=pcp_photo"
	qr := renderQR(t, code)
	const w, h = 2400, 1600
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(180 + (x*7+y*13)%50)
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 255
		}
	}
	draw.Draw(img, image.Rect(1200, 700, 1200+512, 700+512), qr, image.Point{}, draw.Over)

	got, err := DecodeQRImage(rotate(img, 90))
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Fatalf("неверный текст: %q", got)
	}
}
