package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StateFile — что движок знает о файле (или папке) в синхронизации.
type StateFile struct {
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	Version int64  `json:"version"`
	Dir     bool   `json:"dir,omitempty"`
}

// State — локальное состояние синхронизации (.poscloud-state.json).
type State struct {
	ServerVersion int64                `json:"serverVersion"`
	Files         map[string]StateFile `json:"files"`
}

// StatePath — путь к файлу состояния внутри папки синхронизации.
func StatePath(dir string) string { return filepath.Join(dir, ".poscloud-state.json") }

// LoadState читает состояние; отсутствующий или битый файл — пустое состояние.
// Если основной файл повреждён, пробуем резервную копию .bak.
func LoadState(dir string) State {
	s := State{Files: map[string]StateFile{}}
	for _, p := range []string{StatePath(dir), StatePath(dir) + ".bak"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var ss State
		if json.Unmarshal(data, &ss) == nil {
			if ss.Files == nil {
				ss.Files = map[string]StateFile{}
			}
			return ss
		}
	}
	return s
}

// SaveState записывает состояние атомарно и сохраняет предыдущую версию в .bak.
func SaveState(dir string, s State) {
	b, _ := json.MarshalIndent(s, "", "  ")
	p := StatePath(dir)
	if old, err := os.ReadFile(p); err == nil && len(old) > 0 && json.Valid(old) {
		if err := writeFileAtomic(p+".bak", old, 0644); err == nil {
			hideFile(p + ".bak")
		}
	}
	if err := writeFileAtomic(p, b, 0644); err == nil {
		hideFile(p)
	}
}

// writeFileAtomic пишет файл через временный файл и переименование,
// чтобы внезапное завершение процесса не оставило наполовину записанный JSON.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// HashPath считает sha256 файла.
func HashPath(path string) (string, error) {
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

// FileSize возвращает размер файла (0, если файл недоступен).
func FileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// LocalPath превращает относительный путь синхронизации ("a/b") в путь на диске.
func LocalPath(dir, name string) string {
	return filepath.Join(dir, filepath.FromSlash(name))
}

// SanitizeRel проверяет относительный путь и приводит его к виду "a/b".
func SanitizeRel(p string) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.Trim(p, "/")
	if p == "" {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	return p, true
}

// LocalScan возвращает карту «имя → состояние» по локальной папке.
// Если файл не менялся (совпадают размер и время), хэш берётся из состояния без пересчёта.
func LocalScan(dir string, st State, rules Rules) map[string]StateFile {
	out := map[string]StateFile{}
	ign := LoadIgnore(dir)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if d.IsDir() {
			if p == dir {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || ign.Match(key) || rules.IsOff(key) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || ign.Match(key) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		size := info.Size()
		mt := info.ModTime().Unix()
		if prev, ok := st.Files[key]; ok && prev.Size == size && prev.MTime == mt && prev.Hash != "" {
			out[key] = StateFile{Hash: prev.Hash, Size: size, MTime: mt, Version: prev.Version}
			return nil
		}
		h, herr := HashPath(p)
		if herr != nil {
			return nil
		}
		out[key] = StateFile{Hash: h, Size: size, MTime: mt}
		return nil
	})
	return out
}

// LocalDirs собирает подкаталоги папки синхронизации (для переноса пустых
// папок). Скрытые, исключённые и отключённые правилами пропускаются.
func LocalDirs(dir string, rules Rules) []string {
	var out []string
	ign := LoadIgnore(dir)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() {
			return nil
		}
		if p == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(d.Name(), ".") || ign.Match(key) || rules.IsOff(key) {
			return filepath.SkipDir
		}
		out = append(out, key)
		return nil
	})
	sort.Strings(out)
	return out
}

// StateCoversDir сообщает, известна ли папка состоянию: есть запись для неё
// самой (файл или папка) либо что-то внутри неё.
func StateCoversDir(files map[string]StateFile, dir string) bool {
	ld := strings.ToLower(filepath.ToSlash(dir))
	prefix := ld + "/"
	for k := range files {
		lk := strings.ToLower(filepath.ToSlash(k))
		if lk == ld || strings.HasPrefix(lk, prefix) {
			return true
		}
	}
	return false
}

// ConflictName строит имя конфликтной копии файла.
func ConflictName(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return base + " (конфликт " + time.Now().Format("2006-01-02_15-04-05") + ")" + ext
}

// PruneEmptyDirs удаляет опустевшие каталоги вверх до корня синхронизации.
func PruneEmptyDirs(root, filePath string) {
	root = filepath.Clean(root)
	dir := filepath.Dir(filepath.Clean(filePath))
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// DirStats считает файлы и суммарный размер каталога (скрытые пропускаются).
func DirStats(root string) (int, int64) {
	var n int
	var bytes int64
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			n++
			bytes += info.Size()
		}
		return nil
	})
	return n, bytes
}

// HasStatePrefix сообщает, есть ли в состоянии записи о файлах внутри папки.
func HasStatePrefix(st State, prefix string) bool {
	p := strings.ToLower(prefix) + "/"
	for k := range st.Files {
		if strings.HasPrefix(strings.ToLower(filepath.ToSlash(k)), p) {
			return true
		}
	}
	return false
}
