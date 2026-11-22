//go:build linux

package main

// Формат иконки трея в Linux — PNG.
//
// systray на Linux регистрируется как StatusNotifierItem и передаёт иконку
// как массив пикселей; байты он разбирает через image.Decode, который НЕ
// понимает ICO. Раньше отдавался ICO, и в журнале появлялось
// "Failed to read icon format image: unknown format", а иконка не появлялась.

// trayIconForOS возвращает статичную иконку трея.
func trayIconForOS() []byte {
	return trayIconPNG
}

// trayFrameForOS возвращает кадр анимации для поворота на deg градусов.
// В Linux анимации нет: StatusNotifierItem не рассчитан на частую смену
// иконки, а лишние вызовы DBus нагружают шину. Отдаём ту же статичную.
func trayFrameForOS(deg float64) []byte {
	_ = deg
	return trayIconPNG
}
