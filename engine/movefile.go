package engine

import (
	"io"
	"os"
	"path/filepath"
)

// moveFile переносит файл на новое место. Сначала — переименованием, а если
// оно не вышло — копированием с удалением исходного.
//
// Запасной путь нужен на Android: приложение в общей памяти
// (/storage/emulated/0/…) может создавать и удалять файлы, но не
// переименовывать их — FUSE отвечает «operation not permitted». Из-за этого
// скачивание падало на последнем шаге: временный файл был уже скачан, но
// встать на место не мог, и в журнале телефона было «не удалось получить …
// rename … operation not permitted».
func moveFile(src, dst string) error {
	return moveFileWith(os.Rename, src, dst)
}

// moveFileWith — moveFile с подставным переименованием (для тестов).
func moveFileWith(rename func(string, string) error, src, dst string) error {
	if err := rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		in.Close()
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	inErr := in.Close()
	outErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if inErr != nil {
		return inErr
	}
	if outErr != nil {
		return outErr
	}
	// Исходник убрать не вышло — не страшно: главное, файл уже на месте.
	// Временные файлы движка начинаются с точки и в синхронизацию не попадают.
	_ = os.Remove(src)
	return nil
}
