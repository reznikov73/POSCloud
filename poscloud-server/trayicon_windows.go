//go:build windows

package main

// Формат иконки трея в Windows — ICO: его понимает оболочка Windows,
// и systray отдаёт байты напрямую в WinAPI.

// trayIconForOS возвращает статичную иконку трея.
func trayIconForOS() []byte {
	return trayICO(0)
}

// trayFrameForOS возвращает кадр анимации для поворота на deg градусов.
func trayFrameForOS(deg float64) []byte {
	return trayICO(deg)
}
