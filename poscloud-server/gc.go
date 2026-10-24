package main

import (
	"fmt"
	"path/filepath"
	"time"
)

// gcLoop раз в сутки применяет сроки хранения к истории/корзине и блокам.
func (a *App) gcLoop() {
	time.Sleep(time.Minute)
	for {
		a.runGC()
		time.Sleep(24 * time.Hour)
	}
}

func (a *App) runGC() {
	if a.auth == nil {
		return
	}
	var users int
	for _, u := range a.auth.list() {
		dir := filepath.Join(a.storageRoot(), u.ID)
		items := loadHistory(dir)
		pruneHistoryItems(dir, &items)
		_ = saveHistory(dir, items)
		a.mu.Lock()
		m := a.loadManifestCached(dir)
		gcBlocks(dir, &m)
		a.mu.Unlock()
		users++
	}
	a.log(fmt.Sprintf("GC: обработано пользователей %d (версии %d дн./%d шт., корзина %d дн., блоки %d дн.)",
		users, currentRetention().VersionsDays, currentRetention().VersionsCount,
		currentRetention().TrashDays, currentRetention().OrphanBlocksDays))
}
