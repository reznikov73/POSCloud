package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Run запускает цикл синхронизации: первая полная сверка, обмен по таймеру,
// слежение за изменениями в папке и подписка на события сервера.
func (s *Syncer) Run() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.stop = stop
	s.running = true
	dir := s.cfg.Dir
	interval := s.cfg.Interval
	s.mu.Unlock()
	if interval <= 0 {
		interval = 3
	}
	_ = os.MkdirAll(dir, 0755)
	_ = os.RemoveAll(filepath.Join(dir, ".poscloud-cache"))

	s.safeGo("run", func() {
		s.log("старт синхронизации: " + dir)
		s.safeGo("watch", func() { s.watchLoop(dir, stop) })
		s.safeGo("events", func() { s.eventsLoop(stop) })
		// Первая сверка после запуска — полная: состояние догоняет сервер и
		// само восстанавливается, если локальные записи почему-то разошлись.
		s.MarkManifestResync()
		s.SyncOnce(true)
		tk := time.NewTicker(time.Duration(interval) * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				s.log("синхронизация остановлена")
				s.mu.Lock()
				s.running = false
				s.mu.Unlock()
				return
			case <-tk.C:
				s.SyncOnce(false)
			}
		}
	})
}

// Stop останавливает цикл синхронизации.
func (s *Syncer) Stop() {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.running = false
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// Running сообщает, запущен ли цикл синхронизации.
func (s *Syncer) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// watchLoop следит за изменениями в папке синхронизации и запускает цикл
// синхронизации с задержкой (debounce). Останавливается при закрытии stop.
func (s *Syncer) watchLoop(dir string, stop chan struct{}) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.log("слежение недоступно: " + err.Error() + " (работаем по таймеру)")
		return
	}
	if err := w.Add(dir); err != nil {
		s.log("не удалось следить за папкой: " + err.Error() + " (работаем по таймеру)")
		_ = w.Close()
		return
	}
	s.log("слежение за изменениями включено: " + dir)
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
		_ = w.Close()
	}()
	for {
		select {
		case <-stop:
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if strings.HasPrefix(filepath.Base(ev.Name), ".") {
				continue // служебные и временные файлы пропускаем
			}
			if rel, rerr := filepath.Rel(dir, ev.Name); rerr == nil && s.Rules().IsOff(filepath.ToSlash(rel)) {
				continue // папка отключена от синхронизации
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(500*time.Millisecond, func() { s.SyncOnce(false) })
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			s.log("ошибка слежения: " + err.Error())
		}
	}
}

// eventsLoop держит подключение к /events и запускает синхронизацию по событию.
func (s *Syncer) eventsLoop(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		s.mu.Lock()
		base := BaseURL(s.cfg.Host, s.cfg.Port, s.cfg.TLS)
		tok := s.cfg.Token
		s.mu.Unlock()
		s.runEventsOnce(base, tok, stop)
		select {
		case <-stop:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// runEventsOnce читает поток /events до обрыва и запускает синхронизацию на каждое событие.
func (s *Syncer) runEventsOnce(base, token string, stop chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.safeGo("events-cancel", func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	})
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/events", nil)
	if err != nil {
		return
	}
	AuthHeader(req, token)
	cl := &http.Client{}
	resp, err := cl.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Version int64 `json:"version"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev) == nil {
			s.SyncOnce(false)
		}
	}
}
