package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------- помощники для списка исключений ----------
//
// Файл .poscloudignore остаётся единственным источником правды: готовые наборы,
// выбор из папки и импорт лишь правят его текст, а предпросмотр считает эффект
// по текущему (в том числе ещё не сохранённому) тексту.

// IgnorePreset — готовый набор шаблонов исключений.
type IgnorePreset struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Patterns []string `json:"patterns"`
}

// SyncEntry — элемент папки синхронизации (для выбора исключений мышью).
type SyncEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// IgnoreStats — предпросмотр влияния списка исключений.
type IgnoreStats struct {
	Matched      int   `json:"matched"`
	Total        int   `json:"total"`
	MatchedBytes int64 `json:"matchedBytes"`
	TotalBytes   int64 `json:"totalBytes"`
}

// ignorePresetCatalog — каталог готовых наборов. Шаблон без "/" совпадает с любым
// сегментом пути, поэтому такие наборы работают на любой глубине.
var ignorePresetCatalog = []IgnorePreset{
	{
		ID:    "windows-junk",
		Title: "Мусор Windows",
		Patterns: []string{
			"Thumbs.db", "Desktop.ini", "~$*", "*.tmp", ".DS_Store",
		},
	},
	{
		ID:    "office-temp",
		Title: "Временные файлы Office",
		Patterns: []string{
			"~$*.doc?", "~$*.xls?", "~$*.ppt?",
		},
	},
	{
		ID:    "dev",
		Title: "Проекты и сборки",
		Patterns: []string{
			"node_modules", "bin", "obj", ".vs", ".idea", "__pycache__",
			"target", "dist", "build", "*.cache",
		},
	},
	{
		ID:       "vcs",
		Title:    "Служебные каталоги VCS",
		Patterns: []string{".git", ".svn", ".hg"},
	},
	{
		ID:       "logs",
		Title:    "Логи",
		Patterns: []string{"*.log"},
	},
	{
		ID:       "archives",
		Title:    "Архивы",
		Patterns: []string{"*.zip", "*.rar", "*.7z", "*.tar", "*.gz"},
	},
	{
		ID:       "media-heavy",
		Title:    "Тяжёлые медиа",
		Patterns: []string{"*.iso", "*.mkv", "*.mp4", "*.psd", "*.vmdk"},
	},
}

// Набор записывается в файл отдельным блоком, чтобы его можно было снять
// целиком и не задеть строки, добавленные вручную:
//
//	# preset: logs
//	*.log
//	# /preset: logs

func presetStartLine(id string) string { return "# preset: " + id }
func presetEndLine(id string) string   { return "# /preset: " + id }

func presetByID(id string) (IgnorePreset, bool) {
	for _, p := range ignorePresetCatalog {
		if p.ID == id {
			return p, true
		}
	}
	return IgnorePreset{}, false
}

// IgnorePresets возвращает готовые наборы исключений для интерфейса.
func (a *App) IgnorePresets() []IgnorePreset { return ignorePresetCatalog }

// AppliedIgnorePresets возвращает идентификаторы включённых наборов.
// Набор считается включённым, только если в тексте есть обе строки-маркера.
func (a *App) AppliedIgnorePresets(text string) []string {
	lines := splitIgnoreLines(text)
	out := []string{}
	for _, p := range ignorePresetCatalog {
		start := indexOfLine(lines, presetStartLine(p.ID))
		end := indexOfLine(lines, presetEndLine(p.ID))
		if start >= 0 && end > start {
			out = append(out, p.ID)
		}
	}
	return out
}

// ApplyIgnorePreset включает или выключает набор шаблонов в тексте исключений.
func (a *App) ApplyIgnorePreset(text, id string, on bool) string {
	preset, ok := presetByID(id)
	if !ok {
		return normalizeIgnoreText(text)
	}
	lines := splitIgnoreLines(text)

	if on {
		if indexOfLine(lines, presetStartLine(id)) >= 0 {
			return normalizeIgnoreText(strings.Join(lines, "\n")) // уже включён
		}
		lines = append(lines, presetStartLine(id))
		lines = append(lines, preset.Patterns...)
		lines = append(lines, presetEndLine(id))
		return normalizeIgnoreText(strings.Join(lines, "\n"))
	}

	start := indexOfLine(lines, presetStartLine(id))
	end := indexOfLine(lines, presetEndLine(id))
	if start < 0 || end < start {
		// Блок не найден или повреждён — ничего не трогаем, чтобы не удалить лишнее.
		return normalizeIgnoreText(strings.Join(lines, "\n"))
	}
	rest := append(append([]string{}, lines[:start]...), lines[end+1:]...)
	return normalizeIgnoreText(strings.Join(rest, "\n"))
}

// AddIgnorePatterns добавляет шаблоны, не создавая дублей уже имеющихся строк.
func (a *App) AddIgnorePatterns(text string, patterns []string) string {
	lines := splitIgnoreLines(text)
	have := map[string]bool{}
	for _, l := range lines {
		have[strings.TrimSpace(l)] = true
	}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || have[p] {
			continue
		}
		lines = append(lines, p)
		have[p] = true
	}
	return normalizeIgnoreText(strings.Join(lines, "\n"))
}

// IgnorePreview считает, сколько файлов папки синхронизации попадёт под исключения.
// Учитываются те же правила, что и при синхронизации: служебные записи (с точкой
// в начале) не считаются, файл внутри исключённой папки тоже исключён.
func (a *App) IgnorePreview(text string) IgnoreStats {
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()

	var st IgnoreStats
	if dir == "" {
		return st
	}
	ign := parseIgnoreText(text)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if hasDotSegment(key) {
			return nil // служебные записи синхронизация не трогает
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		st.Total++
		st.TotalBytes += info.Size()
		if pathIgnored(key, ign) {
			st.Matched++
			st.MatchedBytes += info.Size()
		}
		return nil
	})
	return st
}

// ListSyncEntries отдаёт содержимое одного уровня папки синхронизации.
// Переход выше корня синхронизации запрещён.
func (a *App) ListSyncEntries(rel string) []SyncEntry {
	out := []SyncEntry{}
	a.mu.Lock()
	dir := a.localDir
	a.mu.Unlock()
	if dir == "" {
		return out
	}

	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	rel = strings.Trim(rel, "/")
	if rel != "" {
		if strings.Contains(rel, ":") {
			return out
		}
		clean := []string{}
		for _, p := range strings.Split(rel, "/") {
			switch p {
			case "", ".":
				continue
			case "..":
				return out // выход из папки синхронизации запрещён
			}
			clean = append(clean, p)
		}
		rel = strings.Join(clean, "/")
	}

	full := filepath.Join(dir, filepath.FromSlash(rel))
	if !withinRoot(dir, full) {
		return out
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		p := e.Name()
		if rel != "" {
			p = rel + "/" + e.Name()
		}
		var size int64
		if !e.IsDir() {
			size = info.Size()
		}
		out = append(out, SyncEntry{Name: e.Name(), Path: p, IsDir: e.IsDir(), Size: size})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ImportIgnore читает список исключений из выбранного файла.
func (a *App) ImportIgnore() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Импорт списка исключений",
		Filters: []runtime.FileFilter{
			{DisplayName: "Список исключений", Pattern: "*.txt;*.poscloudignore"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", rerr
	}
	return normalizeIgnoreText(string(b)), nil
}

// ExportIgnore сохраняет текущий текст исключений в выбранный файл.
func (a *App) ExportIgnore(text string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Экспорт списка исключений",
		DefaultFilename: ".poscloudignore",
		Filters: []runtime.FileFilter{
			{DisplayName: "Список исключений", Pattern: "*.poscloudignore;*.txt"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if werr := os.WriteFile(path, []byte(normalizeIgnoreText(text)), 0644); werr != nil {
		return "", werr
	}
	return path, nil
}

// ---------- вспомогательное ----------

// splitIgnoreLines разбирает текст на строки, отбрасывая пустые в конце.
func splitIgnoreLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if lines == nil {
		return []string{}
	}
	return lines
}

// normalizeIgnoreText приводит переводы строк к \n и убирает пустые строки в конце.
func normalizeIgnoreText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func indexOfLine(lines []string, want string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) == want {
			return i
		}
	}
	return -1
}

// pathIgnored сообщает, исключён ли файл: сам путь или любой его родительский каталог.
func pathIgnored(key string, ign ignoreList) bool {
	if ign.match(key) {
		return true
	}
	parts := strings.Split(key, "/")
	for i := 1; i < len(parts); i++ {
		if ign.match(strings.Join(parts[:i], "/")) {
			return true
		}
	}
	return false
}

// hasDotSegment сообщает, есть ли в пути сегмент, начинающийся с точки.
func hasDotSegment(key string) bool {
	for _, s := range strings.Split(key, "/") {
		if strings.HasPrefix(s, ".") {
			return true
		}
	}
	return false
}

func withinRoot(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if strings.EqualFold(p, root) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(p), strings.ToLower(root)+string(filepath.Separator))
}
