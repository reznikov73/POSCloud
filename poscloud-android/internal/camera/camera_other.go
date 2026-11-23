//go:build !android

package camera

import (
	"errors"
	"os"
)

// Shot — снимок экрана (на других системах недоступен).
type Shot struct {
	ID   int64
	Name string
	Size int64
	URI  string
}

// Capture доступен только на Android: на других системах камеры нет.
func Capture(displayName, relativePath string) (string, error) {
	return "", errors.New("съёмка доступна только на Android")
}

// Size доступен только на Android.
func Size(uri string) int64 { return -1 }

// Open доступен только на Android.
func Open(uri string) (*os.File, error) {
	return nil, errors.New("съёмка доступна только на Android")
}

// Delete доступен только на Android.
func Delete(uri string) error {
	return errors.New("съёмка доступна только на Android")
}

// Screenshots доступен только на Android.
func Screenshots() ([]Shot, error) {
	return nil, errors.New("снимки экрана доступны только на Android")
}
