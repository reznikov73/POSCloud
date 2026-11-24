package client

import (
	"os"
	"path/filepath"
	"testing"
)

// TestControllerConflicts — контроллер отдаёт список конфликтных копий из
// папки синхронизации (движок сканирует папку, сеть не нужна).
func TestControllerConflicts(t *testing.T) {
	root := t.TempDir()
	sync := t.TempDir()
	cf := "заметка (конфликт 2026-11-24_10-00-00).txt"
	for name, data := range map[string]string{
		cf:            "локальная правка",
		"заметка.txt": "серверная версия",
		"обычный.txt": "не конфликт",
	} {
		if err := os.WriteFile(filepath.Join(sync, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := NewController(root)
	c.SaveSettings("localhost", 8090, sync, 3, "t", false, "", "", true, "", "", 0)

	list := c.Conflicts()
	if len(list) != 1 {
		t.Fatalf("ожидался один конфликт, получено %d: %v", len(list), list)
	}
	if list[0].Conflict != cf || list[0].Original != "заметка.txt" {
		t.Errorf("пара: %+v", list[0])
	}
	if list[0].Size != int64(len("локальная правка")) {
		t.Errorf("размер: %d", list[0].Size)
	}

	// неизвестный режим отвергается, файл не трогается
	if err := c.ResolveConflict(cf, "какой"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестного режима")
	}
	if _, err := os.Stat(filepath.Join(sync, cf)); err != nil {
		t.Fatalf("копия не должна была тронуться: %v", err)
	}
}
