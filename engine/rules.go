// Пакет engine — общий движок синхронизации POSCloud: правила папок,
// состояние, обмен с сервером. Не зависит от интерфейса, поэтому используется
// и обычным клиентом (Wails), и служебной сборкой для старых Windows.
package engine

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
	FolderBoth = "both"
	FolderUp   = "up"
	FolderDown = "down"
	FolderOff  = "off"
)

// FolderRule — правило для папки: путь относительно корня синхронизации и режим.
type FolderRule struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// ValidFolderMode сообщает, допустим ли режим.
func ValidFolderMode(m string) bool {
	switch m {
	case FolderBoth, FolderUp, FolderDown, FolderOff:
		return true
	}
	return false
}

// FolderModeTitle возвращает человекочитаемое название режима (для журнала).
func FolderModeTitle(m string) string {
	switch m {
	case FolderBoth:
		return "в обе стороны"
	case FolderUp:
		return "только отправлять"
	case FolderDown:
		return "только получать"
	case FolderOff:
		return "не синхронизировать"
	}
	return m
}

// NormalizeRulePath приводит путь правила к виду "a/b" без крайних слешей.
func NormalizeRulePath(p string) (string, bool) {
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

// Rules — набор правил; режим выбирается по самому глубокому совпадению.
type Rules struct {
	List []FolderRule
	Free bool // режим «без правил»: синхронизировать всё в обе стороны
}

// ParseRules нормализует список правил и отбрасывает негодные записи.
// При дублях по регистру побеждает последняя запись.
func ParseRules(list []FolderRule) Rules {
	var out Rules
	for _, r := range list {
		p, ok := NormalizeRulePath(r.Path)
		if !ok || !ValidFolderMode(r.Mode) {
			continue
		}
		replaced := false
		for i := range out.List {
			if strings.EqualFold(out.List[i].Path, p) {
				out.List[i].Mode = r.Mode
				replaced = true
				break
			}
		}
		if !replaced {
			out.List = append(out.List, FolderRule{Path: p, Mode: r.Mode})
		}
	}
	return out
}

// ModeFor возвращает режим для относительного пути ("a/b" или "a/b/c.txt").
// Побеждает правило с самым глубоким совпавшим путём; без правил — FolderOff
// (новая папка ничего не передаёт, пока не выбран режим). Режим «без правил»
// (Free) синхронизирует всё в обе стороны.
func (r Rules) ModeFor(rel string) string {
	if r.Free {
		return FolderBoth
	}
	bestLen := -1
	mode := FolderOff
	name := strings.ToLower(rel)
	for _, rule := range r.List {
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

// ModeForFile — режим для файла: файлы в корне (без "/") синхронизируются
// всегда, в обе стороны; остальные — по режиму своей папки.
func (r Rules) ModeForFile(rel string) string {
	if !strings.Contains(rel, "/") {
		return FolderBoth
	}
	return r.ModeFor(rel)
}

// CanUpload сообщает, разрешена ли выгрузка на сервер для пути.
func (r Rules) CanUpload(rel string) bool {
	m := r.ModeForFile(rel)
	return m == FolderBoth || m == FolderUp
}

// CanDownload сообщает, разрешено ли получение с сервера для пути.
func (r Rules) CanDownload(rel string) bool {
	m := r.ModeForFile(rel)
	return m == FolderBoth || m == FolderDown
}

// IsOff сообщает, отключён ли путь целиком («не синхронизировать»).
func (r Rules) IsOff(rel string) bool {
	return r.ModeFor(rel) == FolderOff
}

// Covered сообщает, накрыт ли путь хоть каким-нибудь правилом
// (сам путь или любой родитель).
func (r Rules) Covered(rel string) bool {
	name := strings.ToLower(rel)
	for _, rule := range r.List {
		rp := strings.ToLower(rule.Path)
		if name == rp || strings.HasPrefix(name, rp+"/") {
			return true
		}
	}
	return false
}

// Explicit возвращает режим правила, заданного ровно для этого пути.
func (r Rules) Explicit(rel string) (string, bool) {
	for _, rule := range r.List {
		if strings.EqualFold(rule.Path, rel) {
			return rule.Mode, true
		}
	}
	return "", false
}
