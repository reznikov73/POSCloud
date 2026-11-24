package engine

import (
	"fmt"
	"strings"
	"time"
)

// ---------- тихие часы ----------
//
// Окно, в котором цикл синхронизации не стартует сам (например 23:00–07:00,
// чтобы ночью не тратить трафик и батарею). Ручная синхронизация («сейчас»)
// работает и в тихие часы. Окно задаётся локальным временем устройства и
// может переходить через полночь (From 23:00, To 07:00). From == To означает
// «окно выключено» — так пустые настройки из старых версий ведут себя как
// раньше. Хранится строками "HH:MM", чтобы настройки читались в client.json
// и переносились между устройствами без разбора часовых поясов.

// QuietActive сообщает, действует ли тихое окно в указанный момент.
// Окно выключено, если строки не заданы или совпадают.
func QuietActive(from, to string, now time.Time) bool {
	fm, ok := parseClock(from)
	if !ok {
		return false
	}
	tm, ok := parseClock(to)
	if !ok {
		return false
	}
	if fm == tm {
		return false
	}
	// минуты от начала суток: сравниваем их, а не time.Time, чтобы окно
	// не зависело от даты.
	cur := now.Hour()*60 + now.Minute()
	if fm < tm {
		// окно внутри одних суток: 09:00–13:00
		return cur >= fm && cur < tm
	}
	// окно переходит через полночь: 23:00–07:00
	return cur >= fm || cur < tm
}

// QuietWindowTitle возвращает человекочитаемое описание окна (для журнала).
func QuietWindowTitle(from, to string) string {
	f, fok := parseClock(from)
	t, tok := parseClock(to)
	if !fok || !tok || f == t {
		return ""
	}
	return fmt.Sprintf("%s–%s", from, to)
}

// parseClock разбирает "HH:MM" в минуты от начала суток.
func parseClock(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// NormalizeClock приводит "HH:MM" к каноническому виду (без пробелов);
// некорректное значение возвращает пустым.
func NormalizeClock(s string) string {
	if _, ok := parseClock(s); !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// quietSkipLogged хранит момент последней записи «тихие часы» в журнал,
// чтобы не писать её каждый цикл (цикл может идти каждые 3 секунды).
func (s *Syncer) quietGate(now time.Time) bool {
	s.mu.Lock()
	from, to := s.cfg.QuietFrom, s.cfg.QuietTo
	last := s.quietLoggedAt
	s.mu.Unlock()
	if !QuietActive(from, to, now) {
		if !last.IsZero() {
			// окно закончилось — пишем один раз, что синхронизация возобновилась
			s.mu.Lock()
			s.quietLoggedAt = time.Time{}
			s.mu.Unlock()
			s.log("тихие часы закончились: синхронизация возобновлена")
		}
		return false
	}
	if now.Sub(last) >= time.Hour {
		s.mu.Lock()
		s.quietLoggedAt = now
		s.mu.Unlock()
		s.log("тихие часы " + QuietWindowTitle(from, to) + ": автоматическая синхронизация приостановлена")
	}
	return true
}

// ---------- лимит размера скачивания ----------
//
// MaxDownMB — файлы больше этого размера не скачиваются автоматически;
// в журнале пишется причина. 0 — ограничения нет (поведение прежних версий).
// Отправка на сервер не ограничивается: пользователь сам положил файл в папку.

// FormatMB возвращает размер файла в виде «12 МБ» / «1,4 ГБ» для журнала.
func FormatMB(mb int64) string {
	switch {
	case mb >= 1024:
		return fmt.Sprintf("%.1f ГБ", float64(mb)/1024)
	default:
		return fmt.Sprintf("%d МБ", mb)
	}
}

// sizeLimited сообщает, превышает ли размер файла лимит скачивания.
func sizeLimited(maxMB, size int64) bool {
	return maxMB > 0 && size > maxMB*1024*1024
}

// skipLimitedNote пишет в журнал, что файл пропущен из-за лимита, по одному
// разу на файл (пока размер или лимит не изменится) — иначе каждые 3 секунды
// цикла журнал заполнялся бы одной и той же строкой.
func (s *Syncer) skipLimitedNote(name string, size, maxMB int64) {
	key := name + "|лимит"
	s.mu.Lock()
	if s.failed == nil {
		s.failed = map[string]string{}
	}
	msg := fmt.Sprintf("%s (%s) больше лимита %s: не скачан, будет скачан при ручной синхронизации или после повышения лимита", name, FormatMB((size+1024*1024-1)/(1024*1024)), FormatMB(maxMB))
	if s.failed[key] == msg {
		s.mu.Unlock()
		return
	}
	s.failed[key] = msg
	s.mu.Unlock()
	s.log("пропущено: " + msg)
}
