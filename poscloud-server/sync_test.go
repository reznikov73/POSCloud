package main

import "testing"

// TestUpsertDirEntry проверяет регистрацию папок в манифесте: новая папка
// добавляется, повторная ничего не меняет, удалённая — восстанавливается.
func TestUpsertDirEntry(t *testing.T) {
	m := Manifest{Files: map[string]ManifestFile{}}

	v, changed := upsertDirEntry(&m, "магазин")
	if !changed || v != 1 {
		t.Fatalf("новая папка: changed=%v v=%d, ожидалось true/1", changed, v)
	}
	e := m.Files["магазин"]
	if !e.Dir || e.Deleted {
		t.Fatalf("запись не похожа на папку: %+v", e)
	}

	v2, changed2 := upsertDirEntry(&m, "магазин")
	if changed2 || v2 != v {
		t.Fatalf("повторная регистрация изменила манифест: changed=%v v=%d", changed2, v2)
	}

	markDeleted("", &m, "магазин")
	if !m.Files["магазин"].Deleted {
		t.Fatalf("папка не помечена удалённой")
	}
	v3, changed3 := upsertDirEntry(&m, "магазин")
	if !changed3 || v3 <= v2 {
		t.Fatalf("восстановление удалённой папки: changed=%v v=%d (было %d)", changed3, v3, v2)
	}
	if f := m.Files["магазин"]; f.Deleted || !f.Dir {
		t.Fatalf("запись после восстановления: %+v", f)
	}
}

// TestUpsertDirEntryReplacesEmptyFile: пустой файл-артефакт на месте папки
// заменяется записью папки, непустой файл — не трогается.
func TestUpsertDirEntryReplacesEmptyFile(t *testing.T) {
	m := Manifest{Version: 10, Files: map[string]ManifestFile{}}
	m.Files["пусто"] = ManifestFile{Name: "пусто", Version: 5}
	v, changed := upsertDirEntry(&m, "пусто")
	if !changed || v != 11 || !m.Files["пусто"].Dir {
		t.Fatalf("пустой файл: changed=%v v=%d запись=%+v", changed, v, m.Files["пусто"])
	}
	m.Files["файл"] = ManifestFile{Name: "файл", Size: 5, Version: 6}
	v2, changed2 := upsertDirEntry(&m, "файл")
	if changed2 || v2 != 11 {
		t.Fatalf("непустой файл не должен затираться: changed=%v v=%d", changed2, v2)
	}
	if m.Files["файл"].Dir || m.Files["файл"].Size != 5 {
		t.Fatalf("запись файла испорчена: %+v", m.Files["файл"])
	}
}
