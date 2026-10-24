package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ConflictItem struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	Name     string `json:"name"`
	Original string `json:"original"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// conflictOriginalName убирает суффикс « (конфликт …)» из имени копии.
func conflictOriginalName(name string) string {
	marker := " (конфликт "
	lower := strings.ToLower(name)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	end := strings.Index(name[idx:], ")")
	if end < 0 {
		return ""
	}
	return name[:idx] + name[idx+end+1:]
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
				UserID: u.ID, UserName: u.Name, Name: name, Original: conflictOriginalName(name),
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

// ResolveConflict: server — оставить серверную версию (удалить копию),
// conflict — заменить оригинал конфликтной копией, both — оставить обе.
func (a *App) ResolveConflict(userID, conflictName, mode string) error {
	clean, ok := sanitizeRel(conflictName)
	if !ok {
		return fmt.Errorf("недопустимый путь конфликта")
	}
	if mode == "both" {
		return nil
	}
	if mode == "server" {
		return a.DeleteUserFile(userID, clean)
	}
	if mode != "conflict" {
		return fmt.Errorf("неизвестное действие")
	}
	original := conflictOriginalName(clean)
	if original == "" {
		return fmt.Errorf("не удалось определить исходный файл")
	}
	original, ok = sanitizeRel(original)
	if !ok {
		return fmt.Errorf("недопустимое имя исходного файла")
	}
	dir := filepath.Join(a.storageRoot(), userID)
	src := filepath.Join(dir, filepath.FromSlash(clean))
	dst := filepath.Join(dir, filepath.FromSlash(original))
	a.mu.Lock()
	m := a.loadManifestCached(dir)
	prev, hasPrev := m.Files[original]
	a.mu.Unlock()
	if hasPrev && !prev.Deleted && !prev.Dir {
		_ = snapshotUserFile(dir, original, "version", prev.Version, prev.Size, prev.MTime, prev.Hash)
	}
	if err := copyFileAtomic(src, dst); err != nil {
		return err
	}
	fi, err := os.Stat(dst)
	if err != nil {
		return err
	}
	hash, err := fileHash(dst)
	if err != nil {
		return err
	}
	a.mu.Lock()
	m = a.loadManifestCached(dir)
	ver := upsertFile(dir, &m, original, fi.Size(), fi.ModTime().Unix(), hash, nil)
	_ = a.saveManifestCached(dir, m)
	gcBlocks(dir, &m)
	a.notifyVersion(userID, ver)
	a.mu.Unlock()
	if err := a.DeleteUserFile(userID, clean); err != nil {
		return err
	}
	a.log("конфликт разрешён в пользу " + clean + " (пользователь " + userID + ")")
	return nil
}

func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".conflict-*")
	if err != nil {
		return err
	}
	tn := tmp.Name()
	_, copyErr := io.Copy(tmp, in)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tn)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if err := os.Rename(tn, dst); err != nil {
		_ = os.Remove(tn)
		return err
	}
	return nil
}
