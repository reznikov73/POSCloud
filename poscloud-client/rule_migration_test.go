package main

import "testing"

// Миграция при первом запуске новой схемы: вложенные правила убираются,
// уже синхронизируемым папкам первого уровня закрепляется «в обе стороны»,
// новые папки остаются с режимом по умолчанию («не синхронизировать»).
func TestMigrateRuleDefaultsPinsSyncedAndDropsNested(t *testing.T) {
	a := newTestApp(t, "")
	a.folderRules = []FolderRule{
		{Path: "Старое/вложенное", Mode: folderUp},
		{Path: "Магазин 1", Mode: folderUp},
	}
	st := clientState{Files: map[string]stateFile{
		"Магазин 1/PROT/x.dbf":  {},
		"Магазин 2/Refer/y.dbf": {},
		"Пустая на вид":         {Dir: true},
		"файл.exe":              {},
	}}
	if !a.migrateRuleDefaults(st) {
		t.Fatal("миграция должна вернуть true при изменениях")
	}
	got := map[string]string{}
	for _, r := range a.folderRules {
		got[r.Path] = r.Mode
	}
	if _, ok := got["Старое/вложенное"]; ok {
		t.Fatal("вложенное правило должно быть убрано")
	}
	if got["Магазин 1"] != folderUp {
		t.Fatalf("существующее правило «Магазин 1» должно сохраниться: %+v", got)
	}
	if got["Магазин 2"] != folderBoth {
		t.Fatalf("папке с состоянием должен быть закреплён режим both: %+v", got)
	}
	if got["Пустая на вид"] != folderBoth {
		t.Fatalf("папка с записью-каталогом должна быть закреплена: %+v", got)
	}
	if _, ok := got["файл.exe"]; ok {
		t.Fatal("файлы в корне не являются папками и не закрепляются")
	}
	if a.migrateRuleDefaults(st) {
		t.Fatal("повторная миграция не должна ничего менять")
	}
}

// Без сохранённого состояния миграция ничего не закрепляет: новые папки
// по умолчанию ничего не передают.
func TestMigrateRuleDefaultsKeepsNewFoldersOff(t *testing.T) {
	a := newTestApp(t, "")
	a.migrateRuleDefaults(clientState{Files: map[string]stateFile{}})
	if len(a.folderRules) != 0 {
		t.Fatalf("без состояния правила не должны появляться: %+v", a.folderRules)
	}
}

// Правило удалённой папки убирается автоматически: в списке не должно
// оставаться строки-призрака.
func TestRemoveRuleForDeletedFolder(t *testing.T) {
	a := newTestApp(t, "")
	a.folderRules = []FolderRule{{Path: "Магазин 9", Mode: folderBoth}}
	a.removeRuleForDeletedFolder("магазин 9") // регистр не важен
	if len(a.folderRules) != 0 {
		t.Fatalf("правило должно быть убрано: %+v", a.folderRules)
	}
}
