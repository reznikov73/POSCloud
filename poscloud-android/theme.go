package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// posTheme — фирменная светлая темая POSCloud: синий акцент как на иконке,
// мягкий серо-голубой фон, белые карточки, крупные заголовки.
// Все остальные аспекты (шрифты, иконки, размеры) наследуются от встроенной.
type posTheme struct {
	base fyne.Theme
}

var _ fyne.Theme = (*posTheme)(nil)

func rgb(r, g, b uint8) color.Color {
	return &color.NRGBA{R: r, G: g, B: b, A: 0xFF}
}

// фирменные цвета
var (
	colPrimary     = rgb(0x25, 0x63, 0xEB) // #2563EB — синий иконки
	colPrimaryDark = rgb(0x1D, 0x4E, 0xD8)
	colBg          = rgb(0xEF, 0xF3, 0xFA) // фон приложения
	colCard        = rgb(0xFF, 0xFF, 0xFF) // поверхности
	colText        = rgb(0x0F, 0x17, 0x2A) // основной текст
	colMuted       = rgb(0x64, 0x74, 0x8B) // второстепенный текст
	colBorder      = rgb(0xDB, 0xE2, 0xEF) // рамки
	colSuccess     = rgb(0x15, 0x9D, 0x4B)
	colDanger      = rgb(0xDC, 0x26, 0x26)
	colWarn        = rgb(0xB4, 0x53, 0x09)
)

// Color возвращает фирменные цвета; для остальных — встроенную тему.
func (t posTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return colPrimary
	case theme.ColorNamePressed:
		return colPrimaryDark
	case theme.ColorNameBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colBg
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameHeaderBackground:
		return colCard
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return colMuted
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return colBorder
	case theme.ColorNameSuccess:
		return colSuccess
	case theme.ColorNameError:
		return colDanger
	case theme.ColorNameWarning:
		return colWarn
	}
	return t.base.Color(name, theme.VariantLight)
}

// Font наследуется от встроенной темы (Noto Sans с кириллицей).
func (t posTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

// Icon наследуется от встроенной темы.
func (t posTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

// Size задаёт чуть более просторную вёрстку и крупные заголовки.
func (t posTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameText:
		return 15
	case theme.SizeNameInnerPadding:
		return 12
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameCardRadius:
		return 14
	case theme.SizeNameButtonRadius:
		return 12
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameSeparatorThickness:
		return 1
	}
	return t.base.Size(name)
}
