package main

import "strings"

// ---------- режимы папок (выборочная синхронизация подпапок) ----------
//
// Внутри основной папки синхронизации для любой подпапки можно задать режим:
//   both — в обе стороны (по умолчанию);
//   up   — только отправлять на сервер (изменения с сервера не применяются);
//   down — только получать с сервера (локальные изменения не выгружаются);
//   off  — не синхронизировать вовсе.
// Правила хранятся в настройках клиента и действуют только на этом компьютере.

const (
	folderBoth = "both"
	folderUp   = "up"
	folderDown = "down"
	folderOff  = "off"
)

// FolderRule — правило для подпапки: путь относительно корня синхронизации и режим.
type FolderRule struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

func validFolderMode(m string) bool {
	switch m {
	case folderBoth, folderUp, folderDown, folderOff:
		return true
	}
	return false
}

// folderModeTitle возвращает человекочитаемое название режима (для журнала).
func folderModeTitle(m string) string {
	switch m {
	case folderBoth:
		return "в обе стороны"
	case folderUp:
		return "только отправлять"
	case folderDown:
		return "только получать"
	case folderOff:
		return "не синхронизировать"
	}
	return m
}

// normalizeRulePath приводит путь правила к виду "a/b" без крайних слешей.
func normalizeRulePath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return p, true
}

// folderRules — набор правил; режим выбирается по самому глубокому совпадению.
type folderRules struct{ rules []FolderRule }

// parseFolderRules нормализует список правил и отбрасывает негодные записи.
func parseFolderRules(list []FolderRule) folderRules {
	var out folderRules
	for _, r := range list {
		p, ok := normalizeRulePath(r.Path)
		if !ok || !validFolderMode(r.Mode) {
			continue
		}
		replaced := false
		for i := range out.rules {
			if strings.EqualFold(out.rules[i].Path, p) {
				out.rules[i].Mode = r.Mode
				replaced = true
				break
			}
		}
		if !replaced {
			out.rules = append(out.rules, FolderRule{Path: p, Mode: r.Mode})
		}
	}
	return out
}

// modeFor возвращает режим для относительного пути ("a/b" или "a/b/c.txt").
// Побеждает правило с самым глубоким совпавшим путём; без правил — folderBoth.
func (r folderRules) modeFor(rel string) string {
	bestLen := -1
	mode := folderBoth
	name := strings.ToLower(rel)
	for _, rule := range r.rules {
		rp := strings.ToLower(rule.Path)
		if name == rp || strings.HasPrefix(name, rp+"/") {
			if len(rp) > bestLen {
				bestLen = len(rp)
				mode = rule.Mode
			}
		}
	}
	return mode
}

func (r folderRules) canUpload(rel string) bool {
	m := r.modeFor(rel)
	return m == folderBoth || m == folderUp
}

func (r folderRules) canDownload(rel string) bool {
	m := r.modeFor(rel)
	return m == folderBoth || m == folderDown
}

func (r folderRules) isOff(rel string) bool {
	return r.modeFor(rel) == folderOff
}

// covered сообщает, накрыт ли путь хоть каким-нибудь правилом (сам путь или любой родитель).
func (r folderRules) covered(rel string) bool {
	name := strings.ToLower(rel)
	for _, rule := range r.rules {
		rp := strings.ToLower(rule.Path)
		if name == rp || strings.HasPrefix(name, rp+"/") {
			return true
		}
	}
	return false
}

// explicit возвращает режим правила, заданного ровно для этого пути.
func (r folderRules) explicit(rel string) (string, bool) {
	for _, rule := range r.rules {
		if strings.EqualFold(rule.Path, rel) {
			return rule.Mode, true
		}
	}
	return "", false
}

// pendingBlocked сообщает, нужно ли отложить скачивание файла: включён режим
// «сначала спрашивать», на путь нет ни одного правила и верхнеуровневая папка
// ещё не освоена на этом ПК. Файлы в корне не откладываются.
// mastered — множество освоенных верхнеуровневых папок (в нижнем регистре).
func pendingBlocked(rel string, ask bool, r folderRules, mastered map[string]bool) bool {
	if !ask {
		return false
	}
	i := strings.Index(rel, "/")
	if i <= 0 {
		return false
	}
	if r.covered(rel) {
		return false
	}
	return !mastered[strings.ToLower(rel[:i])]
}
