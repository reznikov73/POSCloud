package engine

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config — движковая часть настроек (хранится в client.json).
type Config struct {
	Host                 string       `json:"host"`
	Port                 int          `json:"port"`
	Dir                  string       `json:"dir"`
	Interval             int          `json:"interval"`
	Token                string       `json:"token"`
	Rules                []FolderRule `json:"folderRules,omitempty"`
	RulesFree            bool         `json:"rulesFree,omitempty"`
	RuleDefaultsMigrated bool         `json:"ruleDefaultsMigrated,omitempty"`
}

// Status — состояние синхронизации для интерфейса.
type Status struct {
	LastSync string
	Count    int
	Online   bool
	ErrMsg   string
}

// Syncer выполняет синхронизацию папки без привязки к интерфейсу: один и тот
// же код используют обычный клиент (Wails) и служебная сборка для старых Windows.
type Syncer struct {
	mu     sync.Mutex
	syncMu sync.Mutex

	cfg          Config
	logf         func(string)
	save         func(Config) // вызывается, когда движок меняет настройки (правила) — владелец сохраняет их
	needManifest bool

	lastSync string
	count    int
	online   bool
	errMsg   string
}

// NewSyncer создаёт движок синхронизации. logf может быть nil (журнал не ведётся),
// save может быть nil (настройки не сохраняются при изменении движком).
func NewSyncer(cfg Config, logf func(string), save func(Config)) *Syncer {
	if logf == nil {
		logf = func(string) {}
	}
	if save == nil {
		save = func(Config) {}
	}
	return &Syncer{cfg: cfg, logf: logf, save: save}
}

func (s *Syncer) log(msg string) { s.logf(msg) }

// configLocked возвращает копию настроек (правила копируются глубоко).
func (s *Syncer) configLocked() Config {
	c := s.cfg
	c.Rules = append([]FolderRule(nil), s.cfg.Rules...)
	return c
}

// Config возвращает копию текущих настроек.
func (s *Syncer) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configLocked()
}

// Update применяет новые настройки (сохранение выполняет владелец).
func (s *Syncer) Update(cfg Config) {
	s.mu.Lock()
	s.cfg.Host = cfg.Host
	s.cfg.Port = cfg.Port
	s.cfg.Dir = cfg.Dir
	s.cfg.Interval = cfg.Interval
	s.cfg.Token = cfg.Token
	s.cfg.Rules = append([]FolderRule(nil), cfg.Rules...)
	s.cfg.RulesFree = cfg.RulesFree
	s.cfg.RuleDefaultsMigrated = cfg.RuleDefaultsMigrated
	s.mu.Unlock()
}

// Status возвращает текущее состояние синхронизации.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{LastSync: s.lastSync, Count: s.count, Online: s.online, ErrMsg: s.errMsg}
}

// saveNow сохраняет настройки через колбэк владельца.
func (s *Syncer) saveNow() {
	s.save(s.Config())
}

// Rules возвращает разобранные правила (для внешних проверок).
func (s *Syncer) Rules() Rules {
	s.mu.Lock()
	list := append([]FolderRule(nil), s.cfg.Rules...)
	free := s.cfg.RulesFree
	s.mu.Unlock()
	r := ParseRules(list)
	r.Free = free
	return r
}

// ruleIndexLocked ищет правило по пути без учёта регистра. Вызывать под s.mu.
func (s *Syncer) ruleIndexLocked(p string) int {
	low := strings.ToLower(p)
	for i, r := range s.cfg.Rules {
		if strings.ToLower(strings.Trim(r.Path, "/")) == low {
			return i
		}
	}
	return -1
}

// ListRules возвращает правила папок (для интерфейса).
func (s *Syncer) ListRules() []FolderRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FolderRule, 0, len(s.cfg.Rules))
	out = append(out, s.cfg.Rules...)
	return out
}

// AddRule добавляет правило для папки; если правило уже есть — меняет режим.
func (s *Syncer) AddRule(path, mode string) error {
	clean, ok := NormalizeRulePath(path)
	if !ok {
		return fmt.Errorf("недопустимый путь папки")
	}
	if !ValidFolderMode(mode) {
		return fmt.Errorf("недопустимый режим")
	}
	s.mu.Lock()
	if i := s.ruleIndexLocked(clean); i >= 0 {
		s.cfg.Rules[i].Mode = mode
	} else {
		s.cfg.Rules = append(s.cfg.Rules, FolderRule{Path: clean, Mode: mode})
	}
	s.mu.Unlock()
	s.saveNow()
	s.log("папка «" + clean + "»: режим — " + FolderModeTitle(mode))
	s.MarkManifestResync()
	go s.SyncOnce(false)
	return nil
}

// SetRuleMode меняет режим существующего правила.
func (s *Syncer) SetRuleMode(path, mode string) error {
	clean, ok := NormalizeRulePath(path)
	if !ok {
		return fmt.Errorf("недопустимый путь папки")
	}
	if !ValidFolderMode(mode) {
		return fmt.Errorf("недопустимый режим")
	}
	s.mu.Lock()
	i := s.ruleIndexLocked(clean)
	if i < 0 {
		s.mu.Unlock()
		return fmt.Errorf("правило для папки не найдено")
	}
	s.cfg.Rules[i].Mode = mode
	s.mu.Unlock()
	s.saveNow()
	s.log("папка «" + clean + "»: режим — " + FolderModeTitle(mode))
	s.MarkManifestResync()
	go s.SyncOnce(false)
	return nil
}

// RemoveRule убирает правило папки.
func (s *Syncer) RemoveRule(path string) error {
	clean, ok := NormalizeRulePath(path)
	if !ok {
		return fmt.Errorf("недопустимый путь папки")
	}
	s.mu.Lock()
	i := s.ruleIndexLocked(clean)
	if i < 0 {
		s.mu.Unlock()
		return fmt.Errorf("правило для папки не найдено")
	}
	s.cfg.Rules = append(s.cfg.Rules[:i], s.cfg.Rules[i+1:]...)
	s.mu.Unlock()
	s.saveNow()
	s.log("папка «" + clean + "»: правило убрано")
	s.MarkManifestResync()
	go s.SyncOnce(false)
	return nil
}

// removeRuleForDeletedFolder убирает правило папки, когда её удаление уже
// разъехалось по устройствам: чтобы в списке не оставалась строка-призрак.
func (s *Syncer) removeRuleForDeletedFolder(rel string) {
	s.mu.Lock()
	i := s.ruleIndexLocked(rel)
	var removed string
	if i >= 0 {
		removed = s.cfg.Rules[i].Path
		s.cfg.Rules = append(s.cfg.Rules[:i], s.cfg.Rules[i+1:]...)
	}
	s.mu.Unlock()
	if removed != "" {
		s.saveNow()
		s.log("папка «" + removed + "» удалена — правило убрано")
	}
}

// MarkManifestResync ставит пометку «перечитать полный манифест»: так после
// смены правил движок сразу подтягивает всё, что теперь разрешено.
func (s *Syncer) MarkManifestResync() {
	s.mu.Lock()
	s.needManifest = true
	s.mu.Unlock()
}

// Resync перечитывает полный список файлов сервера и запускает синхронизацию.
func (s *Syncer) Resync() {
	s.MarkManifestResync()
	go s.SyncOnce(true)
}

// RulesFreeEnabled сообщает, включён ли режим «без правил».
func (s *Syncer) RulesFreeEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.RulesFree
}

// SetRulesFree включает/выключает режим «без правил»: всё содержимое папки
// синхронизируется в обе стороны, правила папок не действуют.
func (s *Syncer) SetRulesFree(on bool) error {
	s.mu.Lock()
	s.cfg.RulesFree = on
	s.mu.Unlock()
	s.saveNow()
	if on {
		s.log("режим без правил включён: синхронизируется всё в обе стороны")
	} else {
		s.log("режим без правил выключен: действуют правила папок")
	}
	s.MarkManifestResync()
	go s.SyncOnce(false)
	return nil
}

// migrateRuleDefaults — разовая миграция при первом запуске новой схемы:
//  1. правила глубже первого уровня больше не поддерживаются — убираем их;
//  2. папкам первого уровня, уже участвовавшим в синхронизации (есть записи
//     состояния), закрепляем «в обе стороны», чтобы их поведение не изменилось.
//     Все новые папки при этом остаются с режимом «не синхронизировать».
//
// Возвращает true, если правила изменились.
func (s *Syncer) migrateRuleDefaults(st State) bool {
	s.mu.Lock()
	if s.cfg.RuleDefaultsMigrated {
		s.mu.Unlock()
		return false
	}
	s.cfg.RuleDefaultsMigrated = true
	existing := append([]FolderRule(nil), s.cfg.Rules...)
	s.mu.Unlock()

	// корневые папки, уже освоенные этим компьютером (по записям состояния):
	// файлы внутри папок и записи самих папок
	roots := map[string]string{} // нижний регистр -> имя как в состоянии
	for k, sf := range st.Files {
		k = filepath.ToSlash(k)
		if i := strings.Index(k, "/"); i > 0 {
			roots[strings.ToLower(k[:i])] = k[:i]
		} else if sf.Dir {
			roots[strings.ToLower(k)] = k
		}
	}

	var kept []FolderRule
	dropped := 0
	for _, r := range existing {
		clean, ok := NormalizeRulePath(r.Path)
		if !ok || strings.Contains(clean, "/") {
			dropped++
			continue
		}
		kept = append(kept, FolderRule{Path: clean, Mode: r.Mode})
	}
	has := func(p string) bool {
		for _, r := range kept {
			if strings.EqualFold(r.Path, p) {
				return true
			}
		}
		return false
	}
	pinned := 0
	for _, name := range roots {
		if has(name) {
			continue
		}
		kept = append(kept, FolderRule{Path: name, Mode: FolderBoth})
		pinned++
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Path < kept[j].Path })

	s.mu.Lock()
	s.cfg.Rules = kept
	s.mu.Unlock()
	s.saveNow()
	if dropped > 0 || pinned > 0 {
		s.log(fmt.Sprintf("миграция правил папок: убрано вложенных %d, закреплено прежних %d", dropped, pinned))
	}
	return dropped > 0 || pinned > 0
}

// SyncOnce — один цикл двусторонней синхронизации (хэши/версии, удаления, конфликты).
func (s *Syncer) SyncOnce(verbose bool) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	dir := s.cfg.Dir
	tok := s.cfg.Token
	host, port := s.cfg.Host, s.cfg.Port
	list := append([]FolderRule(nil), s.cfg.Rules...)
	free := s.cfg.RulesFree
	needMF := s.needManifest
	s.needManifest = false
	s.mu.Unlock()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0755)
	base := fmt.Sprintf("http://%s:%d", host, port)
	rules := ParseRules(list)
	rules.Free = free

	st := LoadState(dir)
	if s.migrateRuleDefaults(st) {
		s.mu.Lock()
		list = append([]FolderRule(nil), s.cfg.Rules...)
		s.mu.Unlock()
		rules = ParseRules(list)
		rules.Free = free
	}

	srvVer, changes, err := FetchChanges(base, tok, st.ServerVersion)
	if err != nil {
		s.mu.Lock()
		s.errMsg = "нет связи с сервером"
		s.online = false
		s.mu.Unlock()
		s.log("нет связи с " + base + " - " + err.Error())
		return
	}
	s.mu.Lock()
	s.errMsg = ""
	s.online = true
	s.mu.Unlock()

	srv := map[string]ManifestFile{}
	for _, c := range changes {
		if _, ok := SanitizeRel(c.Name); !ok {
			continue
		}
		srv[c.Name] = c
	}
	if needMF {
		if mv, mf, err := FetchManifest(base, tok); err == nil {
			for _, m := range mf {
				if _, ok := SanitizeRel(m.Name); !ok {
					continue
				}
				if _, ok := srv[m.Name]; !ok {
					srv[m.Name] = m
				}
			}
			if mv > srvVer {
				srvVer = mv
			}
			s.log(fmt.Sprintf("полная сверка с сервером: файлов %d", len(mf)))
		} else {
			s.log("не удалось перечитать манифест: " + err.Error())
		}
	}
	local := LocalScan(dir, st, rules)

	dirNames := map[string]bool{}
	names := map[string]bool{}
	for n := range local {
		names[n] = true
	}
	for n := range st.Files {
		if _, ok := SanitizeRel(n); !ok {
			continue
		}
		names[n] = true
	}
	for n := range srv {
		names[n] = true
	}

	next := State{ServerVersion: st.ServerVersion, Files: map[string]StateFile{}}
	up, down, del, skipped := 0, 0, 0, 0

	for name := range names {
		ls, hasLocal := local[name]
		ss, hasState := st.Files[name]
		cs, hasSrv := srv[name]

		// записи о папках обрабатываем отдельным проходом ниже
		if (hasSrv && cs.Dir) || (hasState && ss.Dir) {
			dirNames[name] = true
			continue
		}
		// файл-артефакт от устаревшего клиента на месте локальной папки:
		// разбираемся в отдельном проходе, как с папками
		if hasSrv && !cs.Dir {
			if fi, err := os.Stat(LocalPath(dir, name)); err == nil && fi.IsDir() {
				dirNames[name] = true
				continue
			}
		}

		mode := rules.ModeForFile(name)
		if mode == FolderOff {
			// папка отключена: файл не трогаем, прежнее состояние бережём на случай включения
			if hasState {
				next.Files[name] = ss
			}
			skipped++
			continue
		}
		// carry переносит прежнее состояние, когда файл сейчас не трогаем.
		carry := func() {
			if hasState {
				next.Files[name] = ss
			}
		}
		allowUp := mode == FolderBoth || mode == FolderUp
		allowDown := mode == FolderBoth || mode == FolderDown

		switch {
		// --- сервер удалил файл (tombstone) ---
		case hasSrv && cs.Deleted:
			if !allowDown {
				// «только отправлять»: удаления с сервера не применяем
				carry()
				skipped++
				continue
			}
			if hasLocal {
				if hasState && ls.Hash == ss.Hash {
					_ = os.Remove(LocalPath(dir, name))
					PruneEmptyDirs(dir, LocalPath(dir, name))
					del++
					s.log("вниз удаление: " + name)
				} else {
					cf := ConflictName(name)
					_ = os.Rename(LocalPath(dir, name), LocalPath(dir, cf))
					if mode == FolderBoth {
						if h, v, mt, err := UploadFile(base, dir, cf, tok); err == nil {
							next.Files[cf] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, cf)), MTime: mt, Version: v}
							up++
						}
					}
					s.log("конфликт (удаление vs правка): " + name + " -> " + cf)
				}
			}

		// --- у сервера есть файл ---
		case hasSrv && !cs.Deleted:
			switch {
			case !hasLocal:
				if !allowDown {
					// «только отправлять»: вниз не качаем; если файл был и удалён у нас — удаляем и на сервере
					if hasState {
						if err := DeleteRemote(base, name, tok); err == nil {
							del++
							s.log("вверх удаление: " + name)
						} else {
							carry()
						}
					}
					skipped++
					continue
				}
				if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
					next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
					down++
					s.log("вниз: " + name)
				} else {
					carry()
				}
			case ls.Hash == cs.Hash:
				next.Files[name] = StateFile{Hash: ls.Hash, Size: ls.Size, MTime: ls.MTime, Version: cs.Version}
			case !hasState:
				// первый синк: локальная и серверная версии расходятся
				switch mode {
				case FolderUp:
					// «только отправлять»: источник — это устройство, выгружаем локальную версию
					if h, v, mt, err := UploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, name)), MTime: mt, Version: v}
						up++
						s.log("вверх: " + name)
					}
				case FolderDown:
					// «только получать»: локальные правки не выгружаем, но бережём копией
					cf := ConflictName(name)
					if data, err := os.ReadFile(LocalPath(dir, name)); err == nil {
						_ = os.WriteFile(LocalPath(dir, cf), data, 0644)
					}
					if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
					}
					s.log("конфликт (первый синк, получающая папка): " + name + " -> " + cf)
				default:
					// в обе стороны: делаем конфликтную копию, чтобы не затереть серверную версию
					cf := ConflictName(name)
					if data, err := os.ReadFile(LocalPath(dir, name)); err == nil {
						_ = os.WriteFile(LocalPath(dir, cf), data, 0644)
						if h, v, mt, err := UploadFile(base, dir, cf, tok); err == nil {
							next.Files[cf] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, cf)), MTime: mt, Version: v}
							up++
						}
					}
					if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
					}
					s.log("конфликт (первый синк): " + name + " -> " + cf)
				}
			case hasState && ls.Hash == ss.Hash:
				// локально без изменений, сервер новее
				if allowDown {
					if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
						s.log("вниз: " + name)
					} else {
						carry()
					}
				} else {
					// «только отправлять»: новую серверную версию не принимаем
					carry()
					skipped++
				}
			case hasState && cs.Version > ss.Version:
				// обе стороны изменили
				switch mode {
				case FolderUp:
					// «только отправлять»: побеждает локальная версия
					if h, v, mt, err := UploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, name)), MTime: mt, Version: v}
						up++
						s.log("вверх (приоритет устройства): " + name)
					}
				case FolderDown:
					// «только получать»: ставим серверную версию, локальные правки сохраняем копией
					cf := ConflictName(name)
					_ = os.Rename(LocalPath(dir, name), LocalPath(dir, cf))
					if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
					}
					s.log("конфликт (получающая папка): " + name + " -> " + cf)
				default:
					// в обе стороны: конфликтная копия + серверная версия
					cf := ConflictName(name)
					if data, err := os.ReadFile(LocalPath(dir, name)); err == nil {
						_ = os.WriteFile(LocalPath(dir, cf), data, 0644)
						if h, v, mt, err := UploadFile(base, dir, cf, tok); err == nil {
							next.Files[cf] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, cf)), MTime: mt, Version: v}
							up++
						}
					}
					if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
					}
					s.log("конфликт (правки): " + name + " -> " + cf)
				}
			default:
				// локально изменено, сервер не менял -> выгружаем
				if allowUp {
					if h, v, mt, err := UploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, name)), MTime: mt, Version: v}
						up++
						s.log("вверх: " + name)
					} else {
						carry()
					}
				} else {
					// «только получать»: локальные правки возвращаем к серверной версии
					if err := DownloadFile(base, dir, name, cs.MTime, tok); err == nil {
						next.Files[name] = StateFile{Hash: cs.Hash, Size: cs.Size, MTime: cs.MTime, Version: cs.Version}
						down++
						s.log("вниз (восстановление): " + name)
					} else {
						carry()
					}
				}
			}

		// --- сервер не менял этот файл с прошлого раза ---
		default:
			if hasLocal {
				if hasState && ls.Hash == ss.Hash {
					next.Files[name] = StateFile{Hash: ls.Hash, Size: ls.Size, MTime: ls.MTime, Version: ss.Version}
				} else if allowUp {
					if h, v, mt, err := UploadFile(base, dir, name, tok); err == nil {
						next.Files[name] = StateFile{Hash: h, Size: FileSize(LocalPath(dir, name)), MTime: mt, Version: v}
						up++
						s.log("вверх: " + name)
					} else {
						carry()
					}
				} else if hasState {
					// «только получать»: изменённый файл возвращаем к серверной версии
					if err := DownloadFile(base, dir, name, ss.MTime, tok); err == nil {
						next.Files[name] = ss
						down++
						s.log("вниз (восстановление): " + name)
					} else {
						carry()
					}
				} else {
					// «только получать»: новый локальный файл не выгружаем — оставляем как есть
					carry()
					skipped++
				}
			} else if hasState {
				if allowUp {
					// локально удалён, сервер не менял -> удаляем на сервере (tombstone)
					if err := DeleteRemote(base, name, tok); err == nil {
						del++
						s.log("вверх удаление: " + name)
						PruneEmptyDirs(dir, LocalPath(dir, name))
					} else {
						carry()
					}
				} else {
					// «только получать»: локальное удаление не уходит на сервер — файл возвращается
					if err := DownloadFile(base, dir, name, ss.MTime, tok); err == nil {
						next.Files[name] = ss
						down++
						s.log("вниз (восстановление): " + name)
					} else if strings.Contains(err.Error(), "404") {
						// файла на сервере уже нет — считаем, что удаление состоялось
						del++
					} else {
						carry()
					}
				}
			}
		}
	}

	// ==== папки: перенос пустых каталогов ====
	for n := range dirNames {
		ss, hasState := st.Files[n]
		cs, hasSrv := srv[n]
		mode := rules.ModeFor(n)
		if mode == FolderOff {
			if hasState {
				next.Files[n] = ss
			}
			skipped++
			continue
		}
		allowUp := mode == FolderBoth || mode == FolderUp
		allowDown := mode == FolderBoth || mode == FolderDown
		lp := LocalPath(dir, n)
		_, statErr := os.Stat(lp)
		hasLocal := statErr == nil
		switch {
		case hasSrv && cs.Deleted && cs.Dir:
			if !allowDown {
				if hasState {
					next.Files[n] = ss
				}
				skipped++
				continue
			}
			removedLocal := !hasLocal
			if hasLocal {
				if err := os.Remove(lp); err == nil {
					PruneEmptyDirs(dir, lp)
					del++
					s.log("вниз удаление (папка): " + n)
					removedLocal = true
				}
			}
			if removedLocal {
				s.removeRuleForDeletedFolder(n)
			}
		case hasSrv && !cs.Deleted && cs.Dir:
			if !hasLocal && allowDown {
				if err := os.MkdirAll(lp, 0755); err == nil {
					next.Files[n] = StateFile{Dir: true, Version: cs.Version}
					down++
					s.log("вниз папка: " + n)
				} else if hasState {
					next.Files[n] = ss
				}
			} else if hasLocal {
				next.Files[n] = StateFile{Dir: true, Version: cs.Version}
			} else if hasState {
				next.Files[n] = ss
			}
		case hasSrv && !cs.Dir:
			// на сервере на месте папки файл-артефакт — заново регистрируем папку
			if hasLocal && allowUp {
				if v, err := MkdirRemote(base, n, tok); err == nil {
					next.Files[n] = StateFile{Dir: true, Version: v}
					up++
					s.log("вверх папка (заново): " + n)
				} else if hasState {
					next.Files[n] = ss
				}
			} else if hasState {
				next.Files[n] = ss
			}
		default:
			if hasLocal {
				if hasState {
					next.Files[n] = ss
				} else if allowUp {
					if v, err := MkdirRemote(base, n, tok); err == nil {
						next.Files[n] = StateFile{Dir: true, Version: v}
						up++
						s.log("вверх папка: " + n)
					}
				}
			} else if hasState {
				if allowUp {
					if err := DeleteRemote(base, n, tok); err == nil {
						del++
						s.log("вверх удаление (папка): " + n)
						s.removeRuleForDeletedFolder(n)
					} else {
						next.Files[n] = ss
					}
				} else {
					// «только получать»: удаление папки не уходит на сервер
					if err := os.MkdirAll(lp, 0755); err == nil {
						down++
					}
					next.Files[n] = ss
				}
			}
		}
	}
	// новые локальные папки, которых ещё нет ни в состоянии, ни на сервере
	for _, n := range LocalDirs(dir, rules) {
		if dirNames[n] || StateCoversDir(next.Files, n) {
			continue
		}
		mode := rules.ModeFor(n)
		if mode != FolderBoth && mode != FolderUp {
			continue
		}
		if v, err := MkdirRemote(base, n, tok); err == nil {
			next.Files[n] = StateFile{Dir: true, Version: v}
			up++
			s.log("вверх папка: " + n)
		}
	}

	maxv := srvVer
	for _, f := range next.Files {
		if f.Version > maxv {
			maxv = f.Version
		}
	}
	next.ServerVersion = maxv
	SaveState(dir, next)
	s.mu.Lock()
	s.lastSync = time.Now().Format("15:04:05")
	s.count = len(next.Files)
	s.mu.Unlock()
	if verbose {
		s.log(fmt.Sprintf("синк: сервер %s, локальных %d, изменений %d, вверх %d, вниз %d, удалено %d, пропущено по режимам %d",
			base, len(local), len(changes), up, down, del, skipped))
	}
}

// ServerFolderNode — строка списка синхронизации: папка первого уровня со статусом.
type ServerFolderNode struct {
	Path     string `json:"path"`
	Files    int    `json:"files"`    // файлов на сервере (по всей папке)
	Bytes    int64  `json:"bytes"`    // суммарный размер на сервере
	LocFiles int    `json:"locFiles"` // файлов на этом ПК (по всей папке)
	LocBytes int64  `json:"locBytes"` // суммарный размер на этом ПК
	Local    bool   `json:"local"`    // копия есть на этом ПК
	Rule     string `json:"rule"`     // явно заданный режим ("" — не задан)
	OnlyRule bool   `json:"onlyRule"` // только правило: папки нет нигде
	New      bool   `json:"new"`      // ещё не синхронизировалась и режим не выбран
	Mode     string `json:"mode"`     // действующий режим
}

// ServerFolders отдаёт интерфейсу список папок первого уровня со статусами:
// папки на сервере и на этом ПК плюс сохранённые правила. Вложенные папки в
// список не попадают — они наследуют режим своей папки первого уровня.
// Полный манифест запрашивается по требованию (при открытии закладки).
func (s *Syncer) ServerFolders() ([]ServerFolderNode, error) {
	s.mu.Lock()
	dir := s.cfg.Dir
	tok := s.cfg.Token
	base := fmt.Sprintf("http://%s:%d", s.cfg.Host, s.cfg.Port)
	s.mu.Unlock()
	if dir == "" {
		return nil, fmt.Errorf("не задана папка синхронизации")
	}
	_, files, err := FetchManifest(base, tok)
	if err != nil {
		return nil, err
	}
	st := LoadState(dir)
	s.migrateRuleDefaults(st)
	s.mu.Lock()
	list := append([]FolderRule(nil), s.cfg.Rules...)
	free := s.cfg.RulesFree
	s.mu.Unlock()
	rules := ParseRules(list)
	rules.Free = free

	type topRow struct {
		name  string
		srv   bool
		files int
		bytes int64
		loc   bool
	}
	rows := map[string]*topRow{}
	get := func(raw string) *topRow {
		k := strings.ToLower(raw)
		r := rows[k]
		if r == nil {
			r = &topRow{name: raw}
			rows[k] = r
		}
		return r
	}
	for _, f := range files {
		if _, ok := SanitizeRel(f.Name); !ok {
			continue
		}
		parts := strings.Split(f.Name, "/")
		if len(parts) < 2 && !f.Dir {
			continue // файлы в корне в списке папок не показываем
		}
		r := get(parts[0])
		r.srv = true
		if !f.Dir {
			r.files++
			r.bytes += f.Size
		}
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			get(e.Name()).loc = true
		}
	}
	// правила для папок, которых сейчас нет ни на сервере, ни здесь
	ruleOnly := map[string]bool{}
	for _, r := range list {
		p, ok := NormalizeRulePath(r.Path)
		if !ok || strings.Contains(p, "/") {
			continue
		}
		k := strings.ToLower(p)
		if _, ok := rows[k]; !ok {
			rows[k] = &topRow{name: p}
			ruleOnly[k] = true
		}
	}
	// корневые папки, уже участвовавшие в синхронизации (по состоянию):
	// файлы внутри папок и записи самих папок
	stateTops := map[string]bool{}
	for k, sf := range st.Files {
		k = filepath.ToSlash(k)
		if i := strings.Index(k, "/"); i > 0 {
			stateTops[strings.ToLower(k[:i])] = true
		} else if sf.Dir {
			stateTops[strings.ToLower(k)] = true
		}
	}

	out := make([]ServerFolderNode, 0, len(rows))
	for k, r := range rows {
		node := ServerFolderNode{Path: r.name, Files: r.files, Bytes: r.bytes}
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(r.name))); err == nil && fi.IsDir() {
			node.Local = true
		} else if HasStatePrefix(st, r.name) {
			node.Local = true
		}
		if r.loc {
			node.LocFiles, node.LocBytes = DirStats(filepath.Join(dir, filepath.FromSlash(r.name)))
		}
		if m, ok := rules.Explicit(r.name); ok {
			node.Rule = m
		}
		node.Mode = rules.ModeFor(r.name)
		node.OnlyRule = ruleOnly[k] && !r.srv && !node.Local
		node.New = !rules.Covered(r.name) && !stateTops[k]
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ClearLocalCopy удаляет локальные файлы папки (данные остаются на сервере)
// и забывает её состояние, чтобы синхронизация не сочла файлы удалёнными на сервере.
func (s *Syncer) ClearLocalCopy(rel string) (int, error) {
	clean, ok := NormalizeRulePath(rel)
	if !ok {
		return 0, fmt.Errorf("недопустимый путь папки")
	}
	s.mu.Lock()
	dir := s.cfg.Dir
	s.mu.Unlock()
	if dir == "" {
		return 0, fmt.Errorf("не задана папка синхронизации")
	}
	root := filepath.Clean(filepath.Join(dir, filepath.FromSlash(clean)))
	if root == filepath.Clean(dir) || !strings.HasPrefix(root, filepath.Clean(dir)+string(filepath.Separator)) {
		return 0, fmt.Errorf("недопустимый путь папки")
	}
	n := 0
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			dirs = append(dirs, p)
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if os.Remove(p) == nil {
			n++
		}
		return nil
	})
	// убрать опустевшие каталоги (снизу вверх)
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	st := LoadState(dir)
	lp := strings.ToLower(clean)
	prefix := lp + "/"
	removed := 0
	for k := range st.Files {
		lk := strings.ToLower(filepath.ToSlash(k))
		if lk == lp || strings.HasPrefix(lk, prefix) {
			delete(st.Files, k)
			removed++
		}
	}
	if removed > 0 {
		SaveState(dir, st)
	}
	cur := root
	for cur != filepath.Clean(dir) && strings.HasPrefix(cur, filepath.Clean(dir)+string(filepath.Separator)) {
		if err := os.Remove(cur); err != nil {
			break
		}
		cur = filepath.Dir(cur)
	}
	s.log(fmt.Sprintf("убрана локальная копия папки «%s»: файлов %d", clean, n))
	return n, nil
}

// DeleteServerFolder удаляет папку на сервере (все её файлы), убирает локальную
// копию и правила для неё. Данные исчезают и с других устройств — кнопка
// «Удалить с сервера» на закладке «Синхронизация».
func (s *Syncer) DeleteServerFolder(rel string) (int, error) {
	clean, ok := NormalizeRulePath(rel)
	if !ok {
		return 0, fmt.Errorf("недопустимый путь папки")
	}
	s.mu.Lock()
	tok := s.cfg.Token
	base := fmt.Sprintf("http://%s:%d", s.cfg.Host, s.cfg.Port)
	s.mu.Unlock()
	if tok == "" {
		return 0, fmt.Errorf("не задан токен доступа")
	}
	_, files, err := FetchManifest(base, tok)
	if err != nil {
		return 0, err
	}
	lp := strings.ToLower(clean)
	prefix := lp + "/"
	n := 0
	for _, f := range files {
		lf := strings.ToLower(f.Name)
		if lf != lp && !strings.HasPrefix(lf, prefix) {
			continue
		}
		if err := DeleteRemote(base, f.Name, tok); err != nil {
			s.log(fmt.Sprintf("удаление папки «%s» прервано на файле %s: %v", clean, f.Name, err))
			return n, err
		}
		n++
	}
	if _, err := s.ClearLocalCopy(clean); err != nil {
		s.log("удаление папки «" + clean + "»: локальная копия не убрана: " + err.Error())
	}
	s.mu.Lock()
	out := s.cfg.Rules[:0]
	removedRules := 0
	for _, r := range s.cfg.Rules {
		rl := strings.ToLower(r.Path)
		if rl == lp || strings.HasPrefix(rl, lp+"/") {
			removedRules++
			continue
		}
		out = append(out, r)
	}
	s.cfg.Rules = out
	s.mu.Unlock()
	if removedRules > 0 {
		s.saveNow()
	}
	s.log(fmt.Sprintf("папка «%s» удалена с сервера: файлов %d, правил убрано %d", clean, n, removedRules))
	s.MarkManifestResync()
	return n, nil
}

// TestConnection проверяет связь и токен, возвращает текст для интерфейса.
func TestConnection(host string, port int, token string) string {
	cl := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/whoami", host, port), nil)
	if err != nil {
		return "Ошибка: " + err.Error()
	}
	AuthHeader(req, token)
	resp, err := cl.Do(req)
	if err != nil {
		return "Ошибка: нет связи с " + host + ":" + fmt.Sprintf("%d", port)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var r struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return "OK: связь есть, пользователь - " + r.Name
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "Ошибка: неверный или отозванный токен"
	}
	return fmt.Sprintf("Ошибка: статус %d", resp.StatusCode)
}
