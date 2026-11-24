package engine

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// conflictStampLayout — формат метки времени в имени конфликтной копии
// (см. ConflictName в state.go).
const conflictStampLayout = "2006-01-02_15-04-05"

// conflictMark — маркер конфликтной копии перед меткой времени.
const conflictMark = " (конфликт "

// ConflictOriginalName убирает из имени суффикс конфликтной копии
// « (конфликт YYYY-MM-DD_HH-MM-SS)» и возвращает имя оригинала. Формат
// проверяется строго: файл пользователя со словом «конфликт» в имени
// («Протокол конфликта.docx») копией не считается. ok=false — имя не
// конфликтное.
func ConflictOriginalName(name string) (string, bool) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	i := strings.LastIndex(base, conflictMark)
	if i < 0 || !strings.HasSuffix(base, ")") {
		return "", false
	}
	stamp := base[i+len(conflictMark) : len(base)-1]
	if _, err := time.Parse(conflictStampLayout, stamp); err != nil {
		return "", false
	}
	return base[:i] + ext, true
}

// ConflictPair — конфликтная копия в папке синхронизации и файл, с которым
// она конфликтует. Пара показывается интерфейсу, чтобы человек выбрал
// версию (см. ResolveConflictKeepLocal и ResolveConflictKeepServer).
type ConflictPair struct {
	Conflict string // имя конфликтной копии (относительный путь)
	Original string // имя оригинала (имя без суффикса)
	Size     int64  // размер копии
	MTime    int64  // время изменения копии (Unix, локальное)
	Uploaded bool   // копия выгружена на сервер (по локальному состоянию)
}

// Conflicts возвращает конфликтные копии в папке синхронизации. Это локальный
// обход папки, сеть не используется: в списке оказываются файлы с суффиксом
// « (конфликт …)», который движок добавляет при расхождении версий. Конфликтная
// копия — обычный файл (никаких пометок, кроме имени), поэтому пока человек не
// выбрал версию, она синхронизируется как все файлы и копится на устройствах.
func (s *Syncer) Conflicts() []ConflictPair {
	s.mu.Lock()
	dir := s.cfg.Dir
	s.mu.Unlock()
	if dir == "" {
		return nil
	}
	st := LoadState(dir)
	var out []ConflictPair
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		orig, ok := ConflictOriginalName(key)
		if !ok {
			return nil
		}
		pair := ConflictPair{Conflict: key, Original: orig, Uploaded: st.Files[key].Version > 0}
		if info, ierr := d.Info(); ierr == nil {
			pair.Size = info.Size()
			pair.MTime = info.ModTime().Unix()
		}
		out = append(out, pair)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Conflict < out[j].Conflict })
	return out
}

// ResolveConflictKeepLocal разрешает конфликт в пользу локальной версии:
// содержимое конфликтной копии становится файлом-оригиналом, копия удаляется
// и локально, и на сервере, а оригинал выгружается на сервер немедленно.
//
// Немедленная выгрузка обязательна для случая «удаление против правки»: файл
// без записи состояния рядом с надгробием следующий цикл снова признал бы
// конфликтом. Если выгрузить не удалось (нет связи), файл возвращается на
// место копии и возвращается ошибка — конфликт можно разрешить позже.
func (s *Syncer) ResolveConflictKeepLocal(conflict string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	name, ok := SanitizeRel(conflict)
	if !ok {
		return fmt.Errorf("недопустимое имя файла")
	}
	orig, ok := ConflictOriginalName(name)
	if !ok {
		return fmt.Errorf("файл не является конфликтной копией")
	}
	s.mu.Lock()
	dir, tok := s.cfg.Dir, s.cfg.Token
	base := BaseURL(s.cfg.Host, s.cfg.Port, s.cfg.TLS)
	s.mu.Unlock()
	if dir == "" {
		return fmt.Errorf("не задана папка синхронизации")
	}
	if tok == "" {
		return fmt.Errorf("не задан токен доступа")
	}
	cfPath := LocalPath(dir, name)
	origPath := LocalPath(dir, orig)
	if _, err := os.Stat(cfPath); err != nil {
		return fmt.Errorf("конфликтная копия не найдена: %w", err)
	}
	st := LoadState(dir)
	// Серверную версию оригинала прячем в скрытый временный файл: если выгрузка
	// не удастся, возвращаем всё как было — конфликт остаётся в списке и его
	// можно разрешить при связи. Имя начинается с точки, поэтому синхронизация
	// и список конфликтов его не видят.
	backup := ""
	if _, err := os.Stat(origPath); err == nil {
		backup = filepath.Join(filepath.Dir(origPath), "."+filepath.Base(origPath)+".conflict-tmp")
		if err := moveFile(origPath, backup); err != nil {
			return fmt.Errorf("не удалось прибрать оригинал: %w", err)
		}
	}
	if err := moveFile(cfPath, origPath); err != nil {
		if backup != "" {
			_ = moveFile(backup, origPath)
		}
		return fmt.Errorf("не удалось поставить копию на место файла: %w", err)
	}
	h, v, mt, err := s.upload(base, dir, orig, tok)
	if err != nil {
		// Возвращаем обе версии на место: без записи состояния файл снова ушёл
		// бы в конфликт на следующем цикле, лучше оставить конфликт в списке
		// и разрешить его при связи.
		_ = moveFile(origPath, cfPath)
		if backup != "" {
			_ = moveFile(backup, origPath)
		}
		return fmt.Errorf("не удалось выгрузить файл на сервер: %w", err)
	}
	if backup != "" {
		_ = os.Remove(backup)
	}
	st.Files[orig] = StateFile{Hash: h, Size: FileSize(origPath), MTime: mt, Version: v}
	if st.Files[name].Version > 0 {
		if err := DeleteRemote(base, name, tok); err != nil {
			// Запись состояния копии оставляем: следующий цикл увидит
			// «локально удалён, сервер не менял» и сам удалит копию на сервере.
			s.log("конфликт " + name + ": копия ещё на сервере — будет удалена при следующем цикле (" + err.Error() + ")")
		} else {
			delete(st.Files, name)
		}
	} else {
		delete(st.Files, name)
	}
	SaveState(dir, st)
	s.log("конфликт разрешён (моя версия): " + name)
	return nil
}

// ResolveConflictKeepServer разрешает конфликт в пользу серверной версии:
// конфликтная копия удаляется локально и на сервере, оригинал не трогаем —
// при «правке против правки» под его именем уже лежит серверная версия. Для
// случая «удаление против правки» это означает «принять удаление»:
// содержимого на сервере больше нет (историю версий хранит сервер).
func (s *Syncer) ResolveConflictKeepServer(conflict string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	name, ok := SanitizeRel(conflict)
	if !ok {
		return fmt.Errorf("недопустимое имя файла")
	}
	if _, ok := ConflictOriginalName(name); !ok {
		return fmt.Errorf("файл не является конфликтной копией")
	}
	s.mu.Lock()
	dir, tok := s.cfg.Dir, s.cfg.Token
	base := BaseURL(s.cfg.Host, s.cfg.Port, s.cfg.TLS)
	s.mu.Unlock()
	if dir == "" {
		return fmt.Errorf("не задана папка синхронизации")
	}
	st := LoadState(dir)
	if st.Files[name].Version > 0 {
		if tok == "" {
			return fmt.Errorf("не задан токен доступа")
		}
		if err := DeleteRemote(base, name, tok); err != nil {
			// Запись состояния оставляем: следующий цикл удалит копию на
			// сервере сам («локально удалён, сервер не менял»).
			s.log("конфликт " + name + ": копия ещё на сервере — будет удалена при следующем цикле (" + err.Error() + ")")
		} else {
			delete(st.Files, name)
		}
	} else {
		delete(st.Files, name)
	}
	if err := os.Remove(LocalPath(dir, name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	PruneEmptyDirs(dir, LocalPath(dir, name))
	SaveState(dir, st)
	s.log("конфликт разрешён (серверная версия): " + name)
	return nil
}
