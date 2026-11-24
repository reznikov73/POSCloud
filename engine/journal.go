package engine

// ChangeEvent — одно изменение, применённое синхронизацией: файл пришёл с
// сервера, ушёл на сервер или удалён (вслед за сервером или с этого
// устройства). События собираются за цикл SyncOnce и отдаются приёмнику
// одной пачкой: владелец движка показывает из них ленту «Недавние».
type ChangeEvent struct {
	Name string `json:"name"` // относительный путь файла ("a/b.txt")
	Op   string `json:"op"`   // "up" — отправлен, "down" — получен, "delete" — удалён
	Time int64  `json:"time"` // Unix-время применения
}

// SetChangeFunc задаёт приёмник событий об изменениях (nil — события не
// отдаются). Владелец вызывает до запуска синхронизации; приёмник вызывается
// в конце цикла SyncOnce, пачкой, поэтому частая синхронизация не создаёт
// частых вызовов. Папки (создание/удаление пустых каталогов) в события
// не попадают — лента про файлы.
func (s *Syncer) SetChangeFunc(fn func([]ChangeEvent)) {
	s.mu.Lock()
	s.eventFn = fn
	s.mu.Unlock()
}

// deliverChangeEvents отдаёт собранную за цикл пачку приёмнику.
func (s *Syncer) deliverChangeEvents(events []ChangeEvent) {
	if len(events) == 0 {
		return
	}
	s.mu.Lock()
	fn := s.eventFn
	s.mu.Unlock()
	if fn != nil {
		fn(events)
	}
}
