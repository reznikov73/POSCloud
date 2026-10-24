package main

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type ConflictItem struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// Conflicts returns conflict-copy files ("(конфликт ...)") across users.
func (a *App) Conflicts() []ConflictItem {
	out := make([]ConflictItem, 0)
	if a.auth == nil {
		return out
	}
	for _, u := range a.auth.list() {
		root := filepath.Join(a.storageRoot(), u.ID)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".blocks" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == ".manifest.json" {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return nil
			}
			name := filepath.ToSlash(rel)
			if !strings.Contains(strings.ToLower(name), "конфликт") {
				return nil
			}
			fi, statErr := d.Info()
			if statErr != nil {
				return nil
			}
			out = append(out, ConflictItem{
				UserID: u.ID, UserName: u.Name, Name: name,
				Size: fi.Size(), Modified: fi.ModTime().Format("02.01.2006 15:04"),
			})
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Name < out[j].Name
	})
	return out
}
