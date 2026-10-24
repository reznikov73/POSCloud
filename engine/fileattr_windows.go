//go:build windows

package engine

import "golang.org/x/sys/windows"

// HideFile ставит на файл атрибут «скрытый», не меняя остальные атрибуты.
// Если файла нет или Windows не разрешает изменить атрибуты, возвращается ошибка;
// вызывающая сторона обычно игнорирует её, чтобы не ломать синхронизацию.
func HideFile(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0 {
		return nil
	}
	return windows.SetFileAttributes(p, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
}

func hideFile(path string) {
	_ = HideFile(path)
}

// HideStateFiles скрывает основной файл состояния и его резервную копию.
func HideStateFiles(dir string) {
	hideFile(StatePath(dir))
	hideFile(StatePath(dir) + ".bak")
}
