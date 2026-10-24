package main

// Тонкий слой совместимости: код синхронизации живёт в общем движке
// poscloud-engine, чтобы тот же набор использовала и служебная сборка
// для старых Windows.

import (
	"net/http"

	eng "poscloud-engine"
)

type FolderRule = eng.FolderRule
type folderRules = eng.Rules
type clientState = eng.State
type stateFile = eng.StateFile
type manifestFile = eng.ManifestFile
type ignoreList = eng.IgnoreList
type syncConfig = eng.Config
type syncer = eng.Syncer
type ServerFolderNode = eng.ServerFolderNode
type connectionInfo = eng.ConnectionInfo
type pairResult = eng.PairResult

const (
	folderBoth = eng.FolderBoth
	folderUp   = eng.FolderUp
	folderDown = eng.FolderDown
	folderOff  = eng.FolderOff
)

func validFolderMode(m string) bool { return eng.ValidFolderMode(m) }

func folderModeTitle(m string) string { return eng.FolderModeTitle(m) }

func normalizeRulePath(p string) (string, bool) { return eng.NormalizeRulePath(p) }

func parseFolderRules(list []FolderRule) folderRules { return eng.ParseRules(list) }

func sanitizeRel(p string) (string, bool) { return eng.SanitizeRel(p) }

func statePath(dir string) string { return eng.StatePath(dir) }

func loadState(dir string) clientState { return eng.LoadState(dir) }

func saveState(dir string, s clientState) { eng.SaveState(dir, s) }

func hashPath(path string) (string, error) { return eng.HashPath(path) }

func fileSize(path string) int64 { return eng.FileSize(path) }

func localPath(dir, name string) string { return eng.LocalPath(dir, name) }

func stateCoversDir(files map[string]stateFile, dir string) bool {
	return eng.StateCoversDir(files, dir)
}

func conflictName(name string) string { return eng.ConflictName(name) }

func pruneEmptyDirs(root, filePath string) { eng.PruneEmptyDirs(root, filePath) }

func dirStats(root string) (int, int64) { return eng.DirStats(root) }

func hasStatePrefix(st clientState, prefix string) bool { return eng.HasStatePrefix(st, prefix) }

func localScan(dir string, st clientState, rules folderRules) map[string]stateFile {
	return eng.LocalScan(dir, st, rules)
}

func localDirs(dir string, rules folderRules) []string { return eng.LocalDirs(dir, rules) }

func loadIgnore(dir string) ignoreList { return eng.LoadIgnore(dir) }

func parseIgnoreText(text string) ignoreList { return eng.ParseIgnoreText(text) }

func fetchChanges(base, token string, since int64) (int64, []manifestFile, error) {
	return eng.FetchChanges(base, token, since)
}

func fetchManifest(base, token string) (int64, []manifestFile, error) {
	return eng.FetchManifest(base, token)
}

func uploadFile(base, dir, name, token string) (string, int64, int64, error) {
	return eng.UploadFile(base, dir, name, token)
}

func downloadFile(base, dir, name string, mtime int64, token string) error {
	return eng.DownloadFile(base, dir, name, mtime, token)
}

func deleteRemote(base, name, token string) error { return eng.DeleteRemote(base, name, token) }

func mkdirRemote(base, name, token string) (int64, error) { return eng.MkdirRemote(base, name, token) }

func authHeader(req *http.Request, token string) { eng.AuthHeader(req, token) }

// newSyncer создаёт движок синхронизации с колбеками клиента.
func newSyncer(cfg syncConfig, logf func(string), save func(syncConfig)) *syncer {
	if logf == nil {
		logf = func(string) {}
	}
	if save == nil {
		save = func(syncConfig) {}
	}
	return eng.NewSyncer(cfg, logf, func(c syncConfig) { save(c) })
}

func testConnection(host string, port int, token string, secure bool) string {
	return eng.TestConnection(host, port, token, secure)
}

func configureTLS(fingerprint string) error { return eng.ConfigureTLS(fingerprint) }

func baseURL(host string, port int, secure bool) string { return eng.BaseURL(host, port, secure) }

func parseConnectionCode(raw string) (connectionInfo, error) { return eng.ParseConnectionCode(raw) }

func pairConnection(base, code, deviceName string) (pairResult, error) {
	return eng.Pair(base, code, deviceName)
}
