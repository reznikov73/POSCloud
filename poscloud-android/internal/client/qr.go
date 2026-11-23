package client

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // camera shots come as JPEG
	_ "image/png"  // screenshots come as PNG
	"io"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// maxDecodeSide ограничивает сторону снимка при распознавании: gozxing
// перебирает пиксели, и фотография 4000×3000 ищется в разы дольше.
const maxDecodeSide = 1600

// decodeScales — размеры, на которых пробуем распознать код. Плотный QR,
// снятый с экрана, на 1600 px даёт муар и не читается, а на 800–1200 px
// читается уверенно; слишком мелкий масштаб теряет модули.
var decodeScales = []int{800, 1200, maxDecodeSide}

// DecodeQRImage looks for a QR code in an image and returns its text.
// Used for connecting via code: the server shows a QR, the phone
// captures it with its camera, the shot is selected in the app and decoded.
//
// Снимок с камеры приходит с EXIF-ориентацией, которую image.Decode не
// применяет, поэтому код на фото может оказаться повёрнутым на 90°.
// Перебираем повороты, масштабы и варианты обработки (ч/б, резкость, негатив).
func DecodeQRImage(img image.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("пустое изображение")
	}
	variants := []func(image.Image) image.Image{nil, binarize, sharpen, invert}

	tried := map[[2]int]bool{}
	for _, side := range decodeScales {
		im := resizeMax(img, side)
		b := im.Bounds()
		key := [2]int{b.Dx(), b.Dy()}
		if tried[key] {
			continue
		}
		tried[key] = true
		for _, variant := range variants {
			src := im
			if variant != nil {
				src = variant(im)
			}
			if txt, ok := decodeRotations(src); ok {
				return txt, nil
			}
		}
	}

	// Последняя попытка — исходное разрешение (только без поворота и с одним
	// поворотом на 90°: полные копии 12-мегапиксельного снимка дороги по памяти).
	b := img.Bounds()
	if b.Dx()*b.Dy() <= 16<<20 {
		if txt, err := decodeQR(img); err == nil && txt != "" {
			return txt, nil
		}
		if txt, err := decodeQR(rotate(img, 90)); err == nil && txt != "" {
			return txt, nil
		}
	}
	return "", fmt.Errorf("QR-код не найден — снимите код крупнее и резче")
}

// decodeRotations пробует декодировать изображение в четырёх поворотах.
func decodeRotations(img image.Image) (string, bool) {
	for _, angle := range []int{0, 90, 180, 270} {
		if txt, err := decodeQR(rotate(img, angle)); err == nil && txt != "" {
			return txt, true
		}
	}
	return "", false
}

// decodeQR — одна попытка распознавания с подсказкой «искать тщательнее».
func decodeQR(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, hints)
	if err != nil {
		return "", err
	}
	return res.GetText(), nil
}

// DecodeQRBytes decodes a QR code from image data (PNG or JPEG).
func DecodeQRBytes(data []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать изображение: %w", err)
	}
	return DecodeQRImage(img)
}

// DecodeQRReader reads an image from a stream and decodes the QR code.
func DecodeQRReader(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 64<<20))
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать файл: %w", err)
	}
	return DecodeQRBytes(data)
}

// rotate поворачивает изображение на угол, кратный 90° (по часовой стрелке).
func rotate(src image.Image, angle int) image.Image {
	angle %= 360
	if angle == 0 {
		return src
	}
	s := toRGBA(src)
	w, h := s.Rect.Dx(), s.Rect.Dy()
	switch angle {
	case 180:
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				si := s.PixOffset(w-1-x, h-1-y)
				di := dst.PixOffset(x, y)
				copy(dst.Pix[di:di+4], s.Pix[si:si+4])
			}
		}
		return dst
	case 90:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				si := s.PixOffset(x, y)
				di := dst.PixOffset(h-1-y, x)
				copy(dst.Pix[di:di+4], s.Pix[si:si+4])
			}
		}
		return dst
	case 270:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				si := s.PixOffset(x, y)
				di := dst.PixOffset(y, w-1-x)
				copy(dst.Pix[di:di+4], s.Pix[si:si+4])
			}
		}
		return dst
	}
	return src
}

// binarize переводит изображение в чёрно-белое по средней яркости:
// так устойчивее распознаются снимки экрана с бликами и серым фоном.
func binarize(src image.Image) image.Image {
	s := toRGBA(src)
	w, h := s.Rect.Dx(), s.Rect.Dy()
	var sum, n uint64
	for i := 0; i < len(s.Pix); i += 4 {
		sum += uint64(s.Pix[i]) + uint64(s.Pix[i+1]) + uint64(s.Pix[i+2])
		n += 3
	}
	var thr uint32
	if n > 0 {
		thr = uint32(sum / n)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(s.Pix); i += 4 {
		lum := (uint32(s.Pix[i])*299 + uint32(s.Pix[i+1])*587 + uint32(s.Pix[i+2])*114) / 1000
		v := uint8(0)
		if lum > thr {
			v = 255
		}
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = v, v, v, 255
	}
	return dst
}

// sharpen повышает резкость: снимок с рук часто смазан, и границы модулей
// QR «плывут» — после усиления контуров декодер находит код.
func sharpen(src image.Image) image.Image {
	s := toRGBA(src)
	w, h := s.Rect.Dx(), s.Rect.Dy()
	dst := image.NewRGBA(s.Rect)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			di := dst.PixOffset(x, y)
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				si := s.PixOffset(x, y)
				copy(dst.Pix[di:di+4], s.Pix[si:si+4])
				continue
			}
			for c := 0; c < 3; c++ {
				v := 5*int(s.Pix[s.PixOffset(x, y)+c]) -
					int(s.Pix[s.PixOffset(x-1, y)+c]) -
					int(s.Pix[s.PixOffset(x+1, y)+c]) -
					int(s.Pix[s.PixOffset(x, y-1)+c]) -
					int(s.Pix[s.PixOffset(x, y+1)+c])
				if v < 0 {
					v = 0
				}
				if v > 255 {
					v = 255
				}
				dst.Pix[di+c] = uint8(v)
			}
			dst.Pix[di+3] = 255
		}
	}
	return dst
}

// invert даёт негатив: светлый код на тёмном фоне так читается.
func invert(src image.Image) image.Image {
	s := toRGBA(src)
	dst := image.NewRGBA(s.Rect)
	for i := 0; i < len(s.Pix); i += 4 {
		dst.Pix[i] = 255 - s.Pix[i]
		dst.Pix[i+1] = 255 - s.Pix[i+1]
		dst.Pix[i+2] = 255 - s.Pix[i+2]
		dst.Pix[i+3] = 255
	}
	return dst
}

// resizeMax уменьшает изображение так, чтобы большая сторона не превышала
// maxSide; усреднение по блокам сохраняет контраст модулей QR.
func resizeMax(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h > side {
		side = h
	}
	if side <= maxSide {
		return src
	}
	nw := w * maxSide / side
	nh := h * maxSide / side
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	s := toRGBA(src)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0, sy1 := y*h/nh, (y+1)*h/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < nw; x++ {
			sx0, sx1 := x*w/nw, (x+1)*w/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, bl, a, cnt uint32
			for sy := sy0; sy < sy1; sy++ {
				row := sy * s.Stride
				for sx := sx0; sx < sx1; sx++ {
					i := row + sx*4
					r += uint32(s.Pix[i])
					g += uint32(s.Pix[i+1])
					bl += uint32(s.Pix[i+2])
					a += uint32(s.Pix[i+3])
					cnt++
				}
			}
			if cnt == 0 {
				continue
			}
			di := dst.PixOffset(x, y)
			dst.Pix[di] = uint8(r / cnt)
			dst.Pix[di+1] = uint8(g / cnt)
			dst.Pix[di+2] = uint8(bl / cnt)
			dst.Pix[di+3] = uint8(a / cnt)
		}
	}
	return dst
}

// toRGBA приводит изображение к *image.RGBA для быстрой попиксельной работы.
func toRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}
