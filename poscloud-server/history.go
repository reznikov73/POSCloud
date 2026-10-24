package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// HistoryItem — одна сохранённая версия файла или элемент корзины.
type HistoryItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // version | trash
	Version   int64  `json:"version"`
	Size      int64  `json:"size"`
	MTime     int64  `json:"mtime"`
	Hash      string `json:"hash"`
	CreatedAt string `json:"createdAt"`
}

func historyRoot(userDir string) string {
	return filepath.Join(filepath.Dir(userDir), ".history", filepath.Base(userDir))
}

func historyIndexPath(userDir string) string {
	return filepath.Join(historyRoot(userDir), "history.json")
}

func historySnapshotsDir(userDir string) string {
	return filepath.Join(historyRoot(userDir), "snapshots")
}

func historySnapshotPath(userDir, id string) string {
	return filepath.Join(historySnapshotsDir(userDir), id+".bak")
}

func loadHistory(userDir string) []HistoryItem {
	data, err := os.ReadFile(historyIndexPath(userDir))
	if err != nil {
		return nil
	}
	var items []HistoryItem
	if json.Unmarshal(data, &items) != nil {
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items
}

func saveHistory(userDir string, items []HistoryItem) error {
	if err := os.MkdirAll(historyRoot(userDir), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return atomicReplaceFile(historyIndexPath(userDir), b, 0644)
}

// snapshotUserFile сохраняет копию текущего файла в историю.
func snapshotUserFile(userDir, name, kind string, version, size, mtime int64, hash string) error {
	src := filepath.Join(userDir, filepath.FromSlash(name))
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(historySnapshotsDir(userDir), 0700); err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixNano(), version)
	dst := historySnapshotPath(userDir, id)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dst)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	item := HistoryItem{
		ID: id, Name: name, Kind: kind, Version: version,
		Size: size, MTime: mtime, Hash: hash,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	items := append([]HistoryItem{item}, loadHistory(userDir)...)
	pruneHistoryItems(userDir, &items)
	return saveHistory(userDir, items)
}

// snapshotExisting сохраняет текущую версию файла перед заменой/удалением.
func (a *App) snapshotExisting(userDir, clean string, prev ManifestFile) {
	if prev.Dir || prev.Deleted {
		return
	}
	if err := snapshotUserFile(userDir, clean, "version", prev.Version, prev.Size, prev.MTime, prev.Hash); err != nil {
		a.log("история " + clean + ": " + err.Error())
	}
}

// pruneHistoryItems оставляет 10 версий/30 дней и 30 дней корзины.
func pruneHistoryItems(userDir string, items *[]HistoryItem) {
	now := time.Now()
	perName := map[string]int{}
	kept := make([]HistoryItem, 0, len(*items))
	for _, it := range *items { // уже отсортированы от новых к старым
		created, err := time.Parse(time.RFC3339, it.CreatedAt)
		if err != nil {
			created = now
		}
		age := now.Sub(created)
		if it.Kind == "trash" {
			if age > 30*24*time.Hour {
				_ = os.Remove(historySnapshotPath(userDir, it.ID))
				continue
			}
			kept = append(kept, it)
			continue
		}
		perName[it.Name]++
		if age > 30*24*time.Hour || perName[it.Name] > 10 {
			_ = os.Remove(historySnapshotPath(userDir, it.ID))
			continue
		}
		kept = append(kept, it)
	}
	*items = kept
}

func findHistoryItem(userDir, id string) (HistoryItem, bool) {
	for _, it := range loadHistory(userDir) {
		if it.ID == id {
			return it, true
		}
	}
	return HistoryItem{}, false
}

func removeHistoryItem(userDir, id string) {
	items := loadHistory(userDir)
	out := items[:0]
	for _, it := range items {
		if it.ID == id {
			_ = os.Remove(historySnapshotPath(userDir, it.ID))
			continue
		}
		out = append(out, it)
	}
	_ = saveHistory(userDir, out)
}

// History возвращает версии конкретного файла.
func (a *App) History(userID, name string) []HistoryItem {
	dir := filepath.Join(a.storageRoot(), userID)
	out := make([]HistoryItem, 0)
	for _, it := range loadHistory(dir) {
		if it.Kind == "version" && it.Name == name {
			out = append(out, it)
		}
	}
	return out
}

// Trash возвращает корзину пользователя.
func (a *App) Trash(userID string) []HistoryItem {
	dir := filepath.Join(a.storageRoot(), userID)
	out := make([]HistoryItem, 0)
	for _, it := range loadHistory(dir) {
		if it.Kind == "trash" {
			out = append(out, it)
		}
	}
	return out
}

// RestoreHistory восстанавливает выбранную версию файла.
func (a *App) RestoreHistory(userID, itemID string) error {
	return a.restoreHistoryItem(userID, itemID)
}

// RestoreTrash восстанавливает файл из корзины.
func (a *App) RestoreTrash(userID, itemID string) error {
	return a.restoreHistoryItem(userID, itemID)
}

func (a *App) restoreHistoryItem(userID, itemID string) error {
	dir := filepath.Join(a.storageRoot(), userID)
	item, ok := findHistoryItem(dir, itemID)
	if !ok {
		return fmt.Errorf("версия не найдена")
	}
	src := historySnapshotPath(dir, item.ID)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("снимок версии не найден")
	}
	dst := filepath.Join(dir, filepath.FromSlash(item.Name))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Перед восстановлением сохраняем текущую версию, чтобы её тоже можно было вернуть.
	a.mu.Lock()
	m := a.loadManifestCached(dir)
	prev, hasPrev := m.Files[item.Name]
	a.mu.Unlock()
	if hasPrev && !prev.Deleted && !prev.Dir {
		_ = snapshotUserFile(dir, item.Name, "version", prev.Version, prev.Size, prev.MTime, prev.Hash)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		in.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	in.Close()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
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
	ver := upsertFile(dir, &m, item.Name, fi.Size(), fi.ModTime().Unix(), hash, nil)
	_ = a.saveManifestCached(dir, m)
	gcBlocks(dir, &m)
	a.notifyVersion(userID, ver)
	a.mu.Unlock()
	removeHistoryItem(dir, itemID)
	a.log("восстановлен " + item.Name + " (версия " + item.CreatedAt + ", пользователь " + userID + ")")
	return nil
}
