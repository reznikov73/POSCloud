package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveStateAtomicAndBackup(t *testing.T) {
	dir := t.TempDir()
	st := State{ServerVersion: 1, Files: map[string]StateFile{
		"a.txt": {Hash: "h1", Size: 1, MTime: 1, Version: 1},
	}}
	SaveState(dir, st)
	if _, err := os.Stat(StatePath(dir)); err != nil {
		t.Fatalf("state file: %v", err)
	}

	st.ServerVersion = 2
	st.Files["a.txt"] = StateFile{Hash: "h2", Size: 2, MTime: 2, Version: 2}
	SaveState(dir, st)
	got := LoadState(dir)
	if got.ServerVersion != 2 || got.Files["a.txt"].Hash != "h2" {
		t.Fatalf("после второго сохранения получено %+v", got)
	}
	if _, err := os.Stat(StatePath(dir) + ".bak"); err != nil {
		t.Fatalf("резервная копия: %v", err)
	}

	// Повреждённый основной файл — загрузка должна взять .bak.
	if err := os.WriteFile(StatePath(dir), []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	got = LoadState(dir)
	if got.ServerVersion != 1 || got.Files["a.txt"].Hash != "h1" {
		t.Fatalf("из .bak ожидалось предыдущее состояние, получено %+v", got)
	}

	entries, _ := os.ReadDir(filepath.Join(dir))
	for _, e := range entries {
		if len(e.Name()) > 5 && e.Name()[:5] == ".tmp-" {
			t.Fatalf("временный файл остался: %s", e.Name())
		}
	}
}
