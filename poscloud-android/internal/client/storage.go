package client

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	eng "poscloud-engine"
)

// ---------- экран «Хранилище»: занято здесь и на сервере ----------

// StorageFolder — строка разбивки хранилища: папка первого уровня или файлы
// в корне папки синхронизации (Root).
type StorageFolder struct {
	Path     string `json:"path"`
	Files    int    `json:"files"` // на сервере
	Bytes    int64  `json:"bytes"`
	LocFiles int    `json:"locFiles"` // на этом устройстве
	LocBytes int64  `json:"locBytes"`
	Root     bool   `json:"root,omitempty"`
}

// Total — суммарный размер папки на сервере и здесь (для сортировки).
func (f StorageFolder) Total() int64 { return f.Bytes + f.LocBytes }

// StorageStats — цифры карточки «Хранилище»: занято здесь, на сервере и
// разбивка по папкам первого уровня. Квота сюда не входит: сервер её не
// отдаёт, а править сервер ради этого экрана не нужно.
type StorageStats struct {
	LocalFiles  int
	LocalBytes  int64
	ServerFiles int
	ServerBytes int64
	ServerErr   error           // сервер недоступен: локальные цифры всё равно есть
	Folders     []StorageFolder // по убыванию размера; файлы в корне — последней строкой
}

// ErrNoServer — подключение не настроено, цифр с сервера не будет.
var ErrNoServer = fmt.Errorf("нет подключения к серверу")

// StorageStats считает занятое место: здесь — обходом папки синхронизации,
// на сервере — по манифесту (удалённые и папки не считаются). Обход и запрос
// выполняются синхронно — из интерфейса вызывать в горутине.
func (c *Controller) StorageStats() StorageStats {
	cfg := c.Config()
	var out StorageStats
	tops := &storageTops{rows: map[string]*StorageFolder{}}
	if cfg.Dir == "" {
		out.ServerErr = ErrNoServer
		return out
	}
	out.LocalFiles, out.LocalBytes = tops.walkLocal(cfg.Dir)
	if strings.TrimSpace(cfg.Token) == "" {
		out.ServerErr = ErrNoServer
		out.Folders = tops.sorted()
		return out
	}
	_, files, err := eng.FetchManifest(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS), cfg.Token)
	if err != nil {
		out.ServerErr = err
		out.Folders = tops.sorted()
		return out
	}
	for _, f := range files {
		if _, ok := eng.SanitizeRel(f.Name); !ok || f.Dir || f.Deleted {
			continue
		}
		row := tops.get(f.Name)
		row.Files++
		row.Bytes += f.Size
		out.ServerFiles++
		out.ServerBytes += f.Size
	}
	out.Folders = tops.sorted()
	return out
}

// storageTops — накопитель цифр по папкам первого уровня. Ключ — имя папки
// в нижнем регистре: на сервере имена могут отличаться регистром от локальных.
type storageTops struct {
	rows map[string]*StorageFolder
}

// get возвращает строку папки для относительного имени файла (через «/»).
func (t *storageTops) get(name string) *StorageFolder {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		if r, ok := t.rows[""]; ok {
			return r
		}
		r := &StorageFolder{Root: true}
		t.rows[""] = r
		return r
	}
	k := strings.ToLower(parts[0])
	if r, ok := t.rows[k]; ok {
		return r
	}
	r := &StorageFolder{Path: parts[0]}
	t.rows[k] = r
	return r
}

// walkLocal считает файлы и байты в папке синхронизации с разбивкой по папкам
// первого уровня. Скрытые файлы и каталоги пропускаются — как в движке.
func (t *storageTops) walkLocal(dir string) (int, int64) {
	var n int
	var size int64
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
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		n++
		size += info.Size()
		row := t.get(filepath.ToSlash(rel))
		row.LocFiles++
		row.LocBytes += info.Size()
		return nil
	})
	return n, size
}

// sorted отдаёт строки по убыванию суммарного размера; файлы в корне — всегда
// последней строкой, чтобы настоящие папки читались первыми.
func (t *storageTops) sorted() []StorageFolder {
	out := make([]StorageFolder, 0, len(t.rows))
	for _, r := range t.rows {
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Root != out[j].Root {
			return out[j].Root
		}
		if out[i].Total() != out[j].Total() {
			return out[i].Total() > out[j].Total()
		}
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out
}
