package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ---------- B1/B2: манифест пространства, версии, tombstones ----------

// ManifestFile — запись о файле (или пустой папке) в пространстве пользователя.
type ManifestFile struct {
	Name    string   `json:"name"`
	Size    int64    `json:"size"`
	MTime   int64    `json:"mtime"`
	Hash    string   `json:"hash"`
	Version int64    `json:"version"`
	Deleted bool     `json:"deleted"`
	Dir     bool     `json:"dir,omitempty"`
	Blocks  []string `json:"blocks,omitempty"`
}

// Manifest — манифест пространства: монотонный счётчик версий и карта файлов.
type Manifest struct {
	Version int64                   `json:"version"`
	Files   map[string]ManifestFile `json:"files"`
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func manifestPath(userDir string) string { return filepath.Join(userDir, ".manifest.json") }

func loadManifest(userDir string) Manifest {
	m := Manifest{Files: map[string]ManifestFile{}}
	data, err := os.ReadFile(manifestPath(userDir))
	if err != nil {
		return m
	}
	var mm Manifest
	if json.Unmarshal(data, &mm) == nil {
		m = mm
		if m.Files == nil {
			m.Files = map[string]ManifestFile{}
		}
	}
	return m
}

func saveManifest(userDir string, m Manifest) error {
	_ = os.MkdirAll(userDir, 0755)
	b, _ := json.MarshalIndent(m, "", "  ")
	// Атомарная запись: при сбое питания манифест останется прежним, а не
	// превратится в обрезанный файл — по нему сервер отдаёт версии файлов.
	return atomicReplaceFile(manifestPath(userDir), b, 0644)
}

// reconcileManifest добавляет в манифест файлы, лежащие на диске, но отсутствующие
// в манифесте (например, загруженные до появления версий). Вызывать под a.mu.
func reconcileManifest(userDir string, m *Manifest) bool {
	changed := false
	_ = filepath.WalkDir(userDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != userDir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, rerr := filepath.Rel(userDir, p)
		if rerr != nil {
			return nil
		}
		name := filepath.ToSlash(rel)
		if _, ok := m.Files[name]; ok {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		h, herr := fileHash(p)
		if herr != nil {
			return nil
		}
		m.Version++
		m.Files[name] = ManifestFile{Name: name, Size: info.Size(), MTime: info.ModTime().Unix(), Hash: h, Version: m.Version}
		changed = true
		return nil
	})
	return changed
}

// upsertFile регистрирует новую/обновлённую версию файла (с блоками). Вызывать под a.mu.
func upsertFile(userDir string, m *Manifest, name string, size, mtime int64, hash string, blocks []string) int64 {
	m.Version++
	m.Files[name] = ManifestFile{Name: name, Size: size, MTime: mtime, Hash: hash, Version: m.Version, Blocks: blocks}
	return m.Version
}

// markDeleted помечает файл удалённым (tombstone). Вызывать под a.mu.
func markDeleted(userDir string, m *Manifest, name string) int64 {
	prev := m.Files[name]
	m.Version++
	m.Files[name] = ManifestFile{Name: name, Size: prev.Size, MTime: prev.MTime, Hash: prev.Hash, Version: m.Version, Deleted: true, Dir: prev.Dir}
	return m.Version
}

// upsertDirEntry регистрирует папку (каталог без файлов) в манифесте. Если
// запись уже есть и не удалена — ничего не меняем. Возвращает версию манифеста
// и признак изменения. Вызывать под a.mu.
func upsertDirEntry(m *Manifest, name string) (int64, bool) {
	if prev, ok := m.Files[name]; ok && !prev.Deleted {
		return m.Version, false
	}
	m.Version++
	m.Files[name] = ManifestFile{Name: name, Dir: true, Version: m.Version}
	return m.Version, true
}

// tombstoneKeep — сколько последних версий манифеста хранит отметки об
// удалении (надгробия). Всё более старое убирается, чтобы манифест не рос.
const tombstoneKeep = 2000

// pruneTombstones убирает старые надгробия, чтобы манифест не разрастался.
// Устройство, пропавшее на много тысяч изменений, в редком случае не узнает
// об удалении — данные при этом не теряются.
func pruneTombstones(m *Manifest) int {
	cut := m.Version - tombstoneKeep
	if cut <= 0 {
		return 0
	}
	removed := 0
	for name, f := range m.Files {
		if f.Deleted && f.Version <= cut {
			delete(m.Files, name)
			removed++
		}
	}
	return removed
}

// blocksDir — каталог блочного хранилища пользователя.
func blocksDir(userDir string) string { return filepath.Join(userDir, ".blocks") }

// blockPath — путь к блоку по его хэшу.
func blockPath(userDir, hash string) string { return filepath.Join(blocksDir(userDir), hash) }

// validHash проверяет, что строка — sha256 в hex (64 символа [0-9a-f]).
func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// gcBlocks удаляет из <userDir>/.blocks блоки, не упомянутые в манифесте.
// Вызывать после сохранения манифеста.
func gcBlocks(userDir string, m *Manifest) {
	used := map[string]bool{}
	for _, f := range m.Files {
		for _, h := range f.Blocks {
			used[h] = true
		}
	}
	dir := blocksDir(userDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !used[e.Name()] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
