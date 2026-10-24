package main

import "sync"

// retentionConfig — сроки хранения истории/корзины и осиротевших блоков.
type retentionConfig struct {
	VersionsDays     int
	VersionsCount    int
	TrashDays        int
	OrphanBlocksDays int
}

var (
	retentionMu sync.RWMutex
	retention   = retentionConfig{
		VersionsDays:     30,
		VersionsCount:    10,
		TrashDays:        30,
		OrphanBlocksDays: 0,
	}
)

func currentRetention() retentionConfig {
	retentionMu.RLock()
	defer retentionMu.RUnlock()
	return retention
}

func setRetention(r retentionConfig) {
	retentionMu.Lock()
	retention = r
	retentionMu.Unlock()
}
