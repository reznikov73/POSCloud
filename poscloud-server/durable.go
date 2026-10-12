package main

import "os"

// writeFileSync записывает данные в файл, дожидаясь реальной записи на диск
// (FlushFileBuffers). Обычный os.WriteFile ни атомарности, ни сброса кэша на
// диск не гарантирует: при внезапном выключении ПК файл может остаться
// обрезанным или пустым.
func writeFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// atomicReplaceFile пишет данные во временный файл рядом с целевым и подменяет
// им целевой файл одной операцией переименования. В любой момент времени на
// диске находится либо прежняя, либо новая версия файла целиком — состояния
// «обрезанной записи» сбой питания не создаёт.
func atomicReplaceFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := writeFileSync(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
