//go:build !android

package camera

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// PhotoPermissionGranted: вне Android разрешения не нужны.
func PhotoPermissionGranted() bool { return true }

// RequestPhotoPermission доступен только на Android.
func RequestPhotoPermission() error {
	return errors.New("разрешения запрашиваются только на Android")
}

// OpenAppSettings доступен только на Android.
func OpenAppSettings() error {
	return errors.New("настройки приложения доступны только на Android")
}

// MimeFor — тип файла по расширению (кроссплатформенная часть).
func MimeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".log", ".md", ".json", ".xml", ".csv":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".mp4":
		return "video/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".apk":
		return "application/vnd.android.package-archive"
	}
	return ""
}

// OpenFile доступен только на Android.
func OpenFile(path string) error {
	return errors.New("открытие файлов доступно только на Android")
}
// AppVersion доступен только на Android.
func AppVersion(pkg string) string { return "" }