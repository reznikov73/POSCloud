package main

import "strings"

// ---------- режимы папок (выборочная синхронизация) ----------
//
// Режимы задаются для папок первого уровня папки синхронизации; всё вложенное
// наследует режим своей папки первого уровня:
//   off  — не синхронизировать (по умолчанию: новая папка ничего не передаёт);
//   both — в обе стороны;
//   up   — только отправлять на сервер (изменения с сервера не применяются);
//   down — только получать с сервера (локальные изменения не выгружаются).
// Файлы в корне (вне папок) синхронизируются всегда, в обе стороны.
// free — специальный режим «без правил»: всё синхронизируется в обе стороны.
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
type folderRules struct {
	rules []FolderRule
	free  bool // режим «без правил»: синхронизировать всё в обе стороны
}

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
// Побеждает правило с самым глубоким совпавшим путём; без правил — folderOff
// (новая папка ничего не передаёт, пока не выбран режим). Режим «без правил»
// (free) синхронизирует всё в обе стороны.
func (r folderRules) modeFor(rel string) string {
	if r.free {
		return folderBoth
	}
	bestLen := -1
	mode := folderOff
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

// modeForFile — режим для файла: файлы в корне (без "/") синхронизируются
// всегда, в обе стороны; остальные — по режиму своей папки.
func (r folderRules) modeForFile(rel string) string {
	if !strings.Contains(rel, "/") {
		return folderBoth
	}
	return r.modeFor(rel)
}

func (r folderRules) canUpload(rel string) bool {
	m := r.modeForFile(rel)
	return m == folderBoth || m == folderUp
}

func (r folderRules) canDownload(rel string) bool {
	m := r.modeForFile(rel)
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
