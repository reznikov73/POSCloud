package client

import (
	"bytes"
	"image"
	"image/color"
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
