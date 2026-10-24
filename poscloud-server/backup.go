package main

import (
	"archive/zip"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const backupFormat = "poscloud-backup/v1"

const (
	maxBackupFile = 8 << 20
	maxBackupZip  = 24 << 20
)

type backupManifest struct {
	Format    string `json:"format"`
	CreatedAt string `json:"createdAt"`
	DataDir   string `json:"dataDir"`
}

// BackupResult — результат создания или восстановления резервной копии.
type BackupResult struct {
	Path       string `json:"path,omitempty"`
	SafetyPath string `json:"safetyPath,omitempty"`
	Error      string `json:"error,omitempty"`
}

// CreateBackup сохраняет настройки, реестр пользователей, токены и TLS
// в выбранный ZIP-файл. Сами файлы пользователей в копию не входят.
func (a *App) CreateBackup() BackupResult {
	defaultName := "POSCloud-backup-" + time.Now().Format("20060102-150405") + ".zip"
	defaultDir := ""
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		defaultDir = filepath.Join(home, "Documents")
		if _, err := os.Stat(defaultDir); err != nil {
			defaultDir = home
		}
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Сохранить резервную копию POSCloud",
		DefaultFilename:  defaultName,
		DefaultDirectory: defaultDir,
		Filters: []runtime.FileFilter{
			{DisplayName: "Резервная копия POSCloud (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return BackupResult{Error: "не удалось открыть диалог сохранения: " + err.Error()}
	}
	if strings.TrimSpace(path) == "" {
		return BackupResult{Error: "сохранение отменено"}
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		path += ".zip"
	}
	if err := a.writeBackup(path); err != nil {
		return BackupResult{Error: err.Error()}
	}
	a.log("создана резервная копия настроек и доступов")
	return BackupResult{Path: path}
}

// RestoreBackup восстанавливает настройки и реестр из ZIP. Сервер должен быть
// остановлен. Перед заменой текущие файлы копируются в .restore-backup-*.
func (a *App) RestoreBackup() BackupResult {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите резервную копию POSCloud",
		Filters: []runtime.FileFilter{
			{DisplayName: "Резервная копия POSCloud (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return BackupResult{Error: "не удалось открыть диалог выбора: " + err.Error()}
	}
	if strings.TrimSpace(path) == "" {
		return BackupResult{Error: "восстановление отменено"}
	}
	return a.restoreBackup(path)
}

func (a *App) writeBackup(path string) error {
	data, err := a.collectBackup()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("не удалось создать каталог: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("не удалось записать файл: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("не удалось сохранить файл: %w", err)
	}
	return nil
}

func (a *App) collectBackup() ([]byte, error) {
	a.mu.Lock()
	dataDir := strings.TrimSpace(a.srvDir)
	root := a.root()
	a.mu.Unlock()
	if dataDir == "" {
		dataDir = root
	}
	if a.auth != nil {
		a.auth.flush()
	}
	usersPath := filepath.Join(dataDir, "users.json")
	users, err := os.ReadFile(usersPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать реестр пользователей: %w", err)
	}
	if !json.Valid(users) {
		return nil, fmt.Errorf("реестр пользователей повреждён")
	}

	entries := map[string][]byte{"users.json": users}
	if b, err := os.ReadFile(filepath.Join(root, "server.json")); err == nil {
		if !json.Valid(b) {
			return nil, fmt.Errorf("server.json повреждён")
		}
		entries["server.json"] = b
	}
	if b, err := os.ReadFile(usersPath + ".bak"); err == nil && json.Valid(b) {
		entries["users.json.bak"] = b
	}
	cert := filepath.Join(dataDir, "tls", "server.crt")
	key := filepath.Join(dataDir, "tls", "server.key")
	if c, err := os.ReadFile(cert); err == nil {
		if k, err := os.ReadFile(key); err == nil {
			entries["tls/server.crt"] = c
			entries["tls/server.key"] = k
		}
	}

	manifest := backupManifest{
		Format:    backupFormat,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		DataDir:   dataDir,
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipEntry(zw, "backup.json", mb); err != nil {
		_ = zw.Close()
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeZipEntry(zw, name, entries[name]); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("не удалось сформировать архив: %w", err)
	}
	if buf.Len() > maxBackupZip {
		return nil, fmt.Errorf("резервная копия слишком большая")
	}
	return buf.Bytes(), nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetMode(0600)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func readBackup(path string) (map[string][]byte, backupManifest, error) {
	var manifest backupManifest
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, manifest, fmt.Errorf("не удалось открыть архив: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > 20 {
		return nil, manifest, fmt.Errorf("в архиве слишком много файлов")
	}
	entries := map[string][]byte{}
	total := 0
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if f.FileInfo().IsDir() {
			continue
		}
		switch name {
		case "backup.json", "server.json", "users.json", "users.json.bak", "tls/server.crt", "tls/server.key":
		default:
			return nil, manifest, fmt.Errorf("неизвестный файл в архиве: %s", name)
		}
		if f.UncompressedSize64 > maxBackupFile {
			return nil, manifest, fmt.Errorf("файл %s слишком большой", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, manifest, fmt.Errorf("не удалось прочитать %s: %w", name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxBackupFile+1))
		_ = rc.Close()
		if err != nil {
			return nil, manifest, fmt.Errorf("не удалось прочитать %s: %w", name, err)
		}
		if len(data) > maxBackupFile {
			return nil, manifest, fmt.Errorf("файл %s слишком большой", name)
		}
		total += len(data)
		if total > maxBackupZip {
			return nil, manifest, fmt.Errorf("архив слишком большой")
		}
		if _, exists := entries[name]; exists {
			return nil, manifest, fmt.Errorf("повторяющийся файл в архиве: %s", name)
		}
		entries[name] = data
	}
	mb, ok := entries["backup.json"]
	if !ok || json.Unmarshal(mb, &manifest) != nil || manifest.Format != backupFormat {
		return nil, manifest, fmt.Errorf("это не резервная копия POSCloud")
	}
	users, ok := entries["users.json"]
	if !ok || !json.Valid(users) {
		return nil, manifest, fmt.Errorf("в архиве нет корректного users.json")
	}
	var uf usersFile
	if err := json.Unmarshal(users, &uf); err != nil || uf.Users == nil {
		return nil, manifest, fmt.Errorf("реестр пользователей в архиве повреждён")
	}
	if srv, ok := entries["server.json"]; ok {
		var cfg savedConfig
		if json.Unmarshal(srv, &cfg) != nil {
			return nil, manifest, fmt.Errorf("server.json в архиве повреждён")
		}
	}
	cert, hasCert := entries["tls/server.crt"]
	key, hasKey := entries["tls/server.key"]
	if hasCert != hasKey {
		return nil, manifest, fmt.Errorf("в архиве неполная пара TLS-сертификата")
	}
	if hasCert {
		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return nil, manifest, fmt.Errorf("TLS-сертификат в архиве повреждён: %w", err)
		}
	}
	return entries, manifest, nil
}

func (a *App) restoreBackup(path string) BackupResult {
	a.mu.Lock()
	if a.srvRun {
		a.mu.Unlock()
		return BackupResult{Error: "остановите сервер перед восстановлением"}
	}
	currentData := strings.TrimSpace(a.srvDir)
	root := a.root()
	oldAuth := a.auth
	a.mu.Unlock()
	if currentData == "" {
		currentData = root
	}

	entries, manifest, err := readBackup(path)
	if err != nil {
		return BackupResult{Error: err.Error()}
	}
	targetData := currentData
	if srv, ok := entries["server.json"]; ok {
		var cfg savedConfig
		_ = json.Unmarshal(srv, &cfg)
		if strings.TrimSpace(cfg.DataDir) != "" {
			targetData = strings.TrimSpace(cfg.DataDir)
		}
	}
	if targetData == "" {
		targetData = root
	}

	safetyDir := filepath.Join(targetData, ".restore-backup-"+time.Now().Format("20060102-150405"))
	safetyPath, err := copyCurrentBackupFiles(root, currentData, safetyDir)
	if err != nil {
		return BackupResult{Error: "не удалось сохранить текущие файлы: " + err.Error()}
	}

	if oldAuth != nil {
		oldAuth.stopFlusher()
	}
	restored := false
	changed := false
	defer func() {
		if !restored && oldAuth != nil && !changed {
			oldAuth.startFlusher()
		}
	}()

	if srv, ok := entries["server.json"]; ok {
		changed = true
		if err := atomicReplaceFile(filepath.Join(root, "server.json"), srv, 0600); err != nil {
			return BackupResult{Error: "не удалось записать server.json: " + err.Error()}
		}
	}
	if err := os.MkdirAll(targetData, 0755); err != nil {
		return BackupResult{Error: "не удалось создать каталог данных: " + err.Error()}
	}
	usersPath := filepath.Join(targetData, "users.json")
	changed = true
	if err := atomicReplaceFile(usersPath, entries["users.json"], 0600); err != nil {
		return BackupResult{Error: "не удалось записать users.json: " + err.Error()}
	}
	if bak, ok := entries["users.json.bak"]; ok {
		_ = atomicReplaceFile(usersPath+".bak", bak, 0600)
	} else {
		_ = atomicReplaceFile(usersPath+".bak", entries["users.json"], 0600)
	}
	if cert, ok := entries["tls/server.crt"]; ok {
		tlsDir := filepath.Join(targetData, "tls")
		if err := os.MkdirAll(tlsDir, 0755); err != nil {
			return BackupResult{Error: "не удалось создать каталог TLS: " + err.Error()}
		}
		if err := atomicReplaceFile(filepath.Join(tlsDir, "server.crt"), cert, 0600); err != nil {
			return BackupResult{Error: "не удалось записать TLS-сертификат: " + err.Error()}
		}
		if err := atomicReplaceFile(filepath.Join(tlsDir, "server.key"), entries["tls/server.key"], 0600); err != nil {
			return BackupResult{Error: "не удалось записать TLS-ключ: " + err.Error()}
		}
	}

	a.mu.Lock()
	a.srvDir = targetData
	a.srvFP = ""
	a.srvErr = ""
	a.mu.Unlock()
	a.loadConfig()
	newAuth := newAuthStore(a.usersStorePath())
	newAuth.setLogger(a.log)
	newAuth.startFlusher()
	a.mu.Lock()
	a.auth = newAuth
	a.srvDir = targetData
	a.mu.Unlock()
	restored = true
	a.log("восстановлена резервная копия настроек и доступов")
	_ = manifest
	return BackupResult{Path: path, SafetyPath: safetyPath}
}

// copyCurrentBackupFiles копирует текущие служебные файлы в отдельный каталог.
// Это страховка перед восстановлением; файлы пользователей сюда не входят.
func copyCurrentBackupFiles(root, dataDir, dst string) (string, error) {
	items := []string{
		filepath.Join(root, "server.json"),
		filepath.Join(dataDir, "users.json"),
		filepath.Join(dataDir, "users.json.bak"),
		filepath.Join(dataDir, "tls", "server.crt"),
		filepath.Join(dataDir, "tls", "server.key"),
	}
	copied := 0
	for _, src := range items {
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		rel, err := filepath.Rel(dataDir, src)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(src)
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(out, data, 0600); err != nil {
			return "", err
		}
		copied++
	}
	if copied == 0 {
		return "", nil
	}
	return dst, nil
}
