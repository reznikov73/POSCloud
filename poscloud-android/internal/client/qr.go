package client

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // camera shots come as JPEG
	_ "image/png"  // screenshots come as PNG
	"io"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// DecodeQRImage looks for a QR code in an image and returns its text.
// Used for connecting via code: the server shows a QR, the phone
// captures it with its camera, the shot is selected in the app and decoded.
func DecodeQRImage(img image.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("empty image")
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("failed to prepare the shot: %w", err)
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", fmt.Errorf("QR code not found — capture the code larger and sharper")
	}
	return res.GetText(), nil
}

// DecodeQRBytes decodes a QR code from image data (PNG or JPEG).
func DecodeQRBytes(data []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to read the image: %w", err)
	}
	return DecodeQRImage(img)
}

// DecodeQRReader reads an image from a stream and decodes the QR code.
func DecodeQRReader(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 64<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read the file: %w", err)
	}
	return DecodeQRBytes(data)
}
