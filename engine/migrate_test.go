package engine

import "testing"

// Миграция при первом запуске новой схемы: вложенные правила убираются,
// уже синхронизируемым папкам первого уровня закрепляется «в обе стороны»,
// новые папки остаются с режимом по умолчанию («не синхронизировать»).
func TestMigrateRuleDefaultsPinsSyncedAndDropsNested(t *testing.T) {
	s := NewSyncer(Config{Rules: []FolderRule{
		{Path: "Старое/вложенное", Mode: FolderUp},
		{Path: "Магазин 1", Mode: FolderUp},
	}}, nil, nil)
	st := State{Files: map[string]StateFile{
		"Магазин 1/PROT/x.dbf":  {},
		"Магазин 2/Refer/y.dbf": {},
		"Пустая на вид":         {Dir: true},
		"файл.exe":              {},
	}}
	if !s.migrateRuleDefaults(st) {
		t.Fatal("миграция должна вернуть true при изменениях")
	}
	got := map[string]string{}
	for _, r := range s.ListRules() {
		got[r.Path] = r.Mode
	}
	if _, ok := got["Старое/вложенное"]; ok {
		t.Fatal("вложенное правило должно быть убрано")
	}
	if got["Магазин 1"] != FolderUp {
		t.Fatalf("существующее правило «Магазин 1» должно сохраниться: %+v", got)
	}
	if got["Магазин 2"] != FolderBoth {
		t.Fatalf("папке с состоянием должен быть закреплён режим both: %+v", got)
	}
	if got["Пустая на вид"] != FolderBoth {
		t.Fatalf("папка с записью-каталогом должна быть закреплена: %+v", got)
	}
	if _, ok := got["файл.exe"]; ok {
		t.Fatal("файлы в корне не являются папками и не закрепляются")
	}
	if s.migrateRuleDefaults(st) {
		t.Fatal("повторная миграция не должна ничего менять")
	}
}

// Без сохранённого состояния миграция ничего не закрепляет: новые папки
// по умолчанию ничего не передают.
func TestMigrateRuleDefaultsKeepsNewFoldersOff(t *testing.T) {
	s := NewSyncer(Config{}, nil, nil)
	s.migrateRuleDefaults(State{Files: map[string]StateFile{}})
	if len(s.ListRules()) != 0 {
		t.Fatalf("без состояния правила не должны появляться: %+v", s.ListRules())
	}
}

// Правило удалённой папки убирается автоматически: в списке не должно
// оставаться строки-призрака.
func TestRemoveRuleForDeletedFolder(t *testing.T) {
	s := NewSyncer(Config{Rules: []FolderRule{{Path: "Магазин 9", Mode: FolderBoth}}}, nil, nil)
	s.removeRuleForDeletedFolder("магазин 9") // регистр не важен
	if len(s.ListRules()) != 0 {
		t.Fatalf("правило должно быть убрано: %+v", s.ListRules())
	}
}
