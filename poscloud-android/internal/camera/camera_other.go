//go:build !android

package camera

import "errors"

// Capture доступен только на Android: на других системах камеры нет.
func Capture(displayName, relativePath string) (string, error) {
	return "", errors.New("съёмка доступна только на Android")
}

// Delete доступен только на Android.
func Delete(uri string) error {
	return errors.New("съёмка доступна только на Android")
}
