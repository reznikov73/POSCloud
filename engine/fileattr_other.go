//go:build !windows

package engine

// HideFile на не-Windows платформах ничего не делает: атрибут «скрытый»
// является особенностью Windows. Файлы по-прежнему начинаются с точки.
func HideFile(path string) error { return nil }

func hideFile(path string) {}

// HideStateFiles — совместимая точка входа для клиентов.
func HideStateFiles(dir string) {}
