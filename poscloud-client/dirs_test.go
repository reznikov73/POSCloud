package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestStateCoversDir — определение, известна ли папка состоянию (сама запись
// или что-то внутри неё).
func TestStateCoversDir(t *testing.T) {
	files := map[string]stateFile{
		"фото/под/1.jpg": {Version: 1},
		"пустая":         {Dir: true, Version: 2},
	}
	cases := []struct {
		dir  string
		want bool
	}{
		{"фото", true},
		{"фото/под", true},
		{"пустая", true},
		{"ПУСТАЯ", true},
		{"новая", false},
		{"пустая/вложенная", false},
	}
	for _, c := range cases {
		if got := stateCoversDir(files, c.dir); got != c.want {
			t.Errorf("stateCoversDir(%q) = %v, ожидалось %v", c.dir, got, c.want)
		}
	}
}

// TestLocalDirs — сбор каталогов для переноса пустых папок: скрытые,
// исключённые и отключённые правилами пропускаются.
func TestLocalDirs(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"a/b", "skip/x", "offf/y", ".hidden/z", "a/.git"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".poscloudignore"), []byte("skip\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rules := parseFolderRules([]FolderRule{
		{Path: "a", Mode: folderBoth},
		{Path: "offf", Mode: folderOff},
	})
	got := localDirs(dir, rules)
	want := []string{"a", "a/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("localDirs = %v, ожидалось %v", got, want)
	}
}
