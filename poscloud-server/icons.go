package main

import (
	_ "embed"
	"image"
	"image/png"
	"bytes"
)

// Иконки встроены в бинарник: рядом с исполняемым файлом их может не быть,
// а иконка нужна и в трее, и на окне.
//
// tray-32.png — маленькая (панель трея), app-256.png — крупная (окно, меню).

//go:embed assets/tray-32.png
var trayIconPNG []byte

//go:embed assets/app-256.png
var appIconPNG []byte

// windowIcon возвращает иконку окна в виде изображения.
// Wails принимает []byte в формате PNG.
func windowIcon() []byte {
	return appIconPNG
}

// decodePNG разбирает PNG в изображение (для отрисовки трея на Linux,
// где systray ожидает PNG/JPEG, а не ICO).
func decodePNG(data []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(data))
}
