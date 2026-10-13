package engine

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IgnoreList — шаблоны из .poscloudignore.
type IgnoreList struct{ patterns []string }

// LoadIgnore читает .poscloudignore из папки синхронизации (нет файла — пустой список).
func LoadIgnore(dir string) IgnoreList {
	data, err := os.ReadFile(filepath.Join(dir, ".poscloudignore"))
	if err != nil {
		return IgnoreList{}
	}
	return ParseIgnoreText(string(data))
}

// ParseIgnoreText разбирает текст списка исключений в набор шаблонов.
func ParseIgnoreText(text string) IgnoreList {
	var il IgnoreList
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(line, "/")
		if line != "" {
			il.patterns = append(il.patterns, line)
		}
	}
	return il
}

// Match проверяет относительный путь "a/b" по шаблонам; шаблон без "/"
// сопоставляется с любым сегментом.
func (il IgnoreList) Match(rel string) bool {
	if len(il.patterns) == 0 || rel == "" {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, pat := range il.patterns {
		if strings.Contains(pat, "/") {
			if globMatch(pat, rel) {
				return true
			}
			continue
		}
		for _, s := range segs {
			if globMatch(pat, s) {
				return true
			}
		}
	}
	return false
}

// globMatch сопоставляет шаблон (*, ?, [...], **) со строкой пути.
func globMatch(pat, s string) bool {
	return matchSegs(strings.Split(pat, "/"), strings.Split(s, "/"))
}

func matchSegs(p, s []string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == "**" {
		if matchSegs(p[1:], s) {
			return true
		}
		for i := 1; i <= len(s); i++ {
			if matchSegs(p[1:], s[i:]) {
				return true
			}
		}
		return false
	}
	if len(s) == 0 {
		return false
	}
	ok, err := path.Match(p[0], s[0])
	if err != nil || !ok {
		return false
	}
	return matchSegs(p[1:], s[1:])
}
