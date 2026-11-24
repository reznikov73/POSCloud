package engine

import (
	"strings"
	"testing"
	"time"
)

// Тихие часы: окно локального времени, в котором автоматический цикл не
// стартует. Проверяем границы, переход через полночь и выключенное окно.

func TestQuietActive(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		now      string // "15:04"
		want     bool
	}{
		{"окно внутри суток, внутри", "09:00", "13:00", "10:30", true},
		{"окно внутри суток, до", "09:00", "13:00", "08:59", false},
		{"окно внутри суток, после", "09:00", "13:00", "13:00", false},
		{"начало окна", "09:00", "13:00", "09:00", true},
		{"конец окна (не включается)", "09:00", "13:00", "12:59", true},
		{"через полночь, вечер", "23:00", "07:00", "23:30", true},
		{"через полночь, ночь", "23:00", "07:00", "03:00", true},
		{"через полночь, утро после окна", "23:00", "07:00", "07:00", false},
		{"через полночь, день до окна", "23:00", "07:00", "12:00", false},
		{"полные сутки не окно (равные)", "00:00", "00:00", "12:00", false},
		{"пустые строки", "", "", "12:00", false},
		{"одна пустая", "23:00", "", "23:30", false},
		{"мусор", "25:00", "07:00", "23:30", false},
		{"без ведущего нуля", "9:00", "13:00", "10:00", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now, err := time.Parse("15:04", c.now)
			if err != nil {
				t.Fatalf("время: %v", err)
			}
			if got := QuietActive(c.from, c.to, now); got != c.want {
				t.Errorf("QuietActive(%q, %q, %s) = %v, хотим %v", c.from, c.to, c.now, got, c.want)
			}
		})
	}
}

func TestNormalizeClock(t *testing.T) {
	if got := NormalizeClock(" 23:05 "); got != "23:05" {
		t.Errorf("NormalizeClock обрезает пробелы: %q", got)
	}
	if got := NormalizeClock("24:00"); got != "" {
		t.Errorf("24:00 некорректно, хотим пусто: %q", got)
	}
	if got := NormalizeClock("7:30"); got != "" {
		t.Errorf("формат строго HH:MM: %q", got)
	}
}

func TestQuietWindowTitle(t *testing.T) {
	if got := QuietWindowTitle("23:00", "07:00"); got != "23:00–07:00" {
		t.Errorf("окно: %q", got)
	}
	if got := QuietWindowTitle("09:00", "09:00"); got != "" {
		t.Errorf("выключенное окно — пусто: %q", got)
	}
}

// quietGate: цикл не идёт в окно, возобновляется после и пишет в журнал
// по одному разу в час, а не при каждом трёхсекундном тике.
func TestQuietGate(t *testing.T) {
	var logs []string
	s := NewSyncer(Config{QuietFrom: "23:00", QuietTo: "07:00"}, func(m string) { logs = append(logs, m) }, nil)
	night := time.Date(2026, 11, 24, 2, 0, 0, 0, time.Local)
	morning := time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)

	if !s.quietGate(night) {
		t.Error("ночью цикл должен стоять")
	}
	if !s.quietGate(night.Add(10 * time.Second)) {
		t.Error("ночью цикл должен стоять")
	}
	n := 0
	for _, l := range logs {
		if strings.Contains(l, "тихие часы") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("запись про тихие часы должна быть одна, получили %d: %v", n, logs)
	}

	if s.quietGate(morning) {
		t.Error("утром цикл должен идти")
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "возобновлена") {
			found = true
		}
	}
	if !found {
		t.Errorf("нет записи о возобновлении: %v", logs)
	}
	if s.quietGate(morning.Add(time.Second)) {
		t.Error("утром цикл должен идти")
	}
	if len(logs) != 2 {
		t.Errorf("лишние записи в журнале: %v", logs)
	}
}

// Лимит скачивания: файл больше MaxDownMB не качается, заметка в журнале
// пишется один раз, а не при каждом цикле.
func TestDownloadLimitSkipsLargeFiles(t *testing.T) {
	var logs []string
	s := NewSyncer(Config{MaxDownMB: 5}, func(m string) { logs = append(logs, m) }, nil)

	ok, err := s.downloadLR(6*1024*1024, "http://127.0.0.1:1", t.TempDir(), "big.bin", 0, "token")
	if err != nil {
		t.Fatalf("пропуск по лимиту — не ошибка: %v", err)
	}
	if ok {
		t.Error("файл больше лимита не должен качаться")
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "больше лимита") {
		t.Fatalf("хотим одну заметку о пропуске: %v", logs)
	}

	// второй вызов — та же заметка, журнал не растёт
	_, _ = s.downloadLR(6*1024*1024, "http://127.0.0.1:1", t.TempDir(), "big.bin", 0, "token")
	if len(logs) != 1 {
		t.Errorf("заметка должна писаться один раз: %v", logs)
	}

	// лимит снят — заметка не мешает качать (проверяем только гейт:
	// сервера нет, поэтому ожидаем ошибку передачи, но не пропуск)
	s.Update(Config{MaxDownMB: 0})
	ok, err = s.downloadLR(6*1024*1024, "http://127.0.0.1:1", t.TempDir(), "big.bin", 0, "token")
	if ok || err == nil {
		t.Errorf("без лимита передача должна идти к серверу (и упасть без него): ok=%v err=%v", ok, err)
	}
}

func TestSizeLimited(t *testing.T) {
	if !sizeLimited(1, 1024*1024+1) {
		t.Error("чуть больше мегабайта — лимит 1 МБ должен сработать")
	}
	if sizeLimited(1, 1024*1024) {
		t.Error("ровно лимит — не превышение")
	}
	if sizeLimited(0, 1<<40) {
		t.Error("0 — ограничения нет")
	}
}

func TestFormatMB(t *testing.T) {
	if got := FormatMB(5); got != "5 МБ" {
		t.Errorf("FormatMB(5) = %q", got)
	}
	if got := FormatMB(2048); got != "2.0 ГБ" {
		t.Errorf("FormatMB(2048) = %q", got)
	}
}

// Update нормализует тихие часы и не пускает отрицательный лимит.
func TestUpdateNormalizesSchedule(t *testing.T) {
	s := NewSyncer(Config{}, nil, nil)
	s.Update(Config{QuietFrom: " 23:00 ", QuietTo: "07:00", MaxDownMB: -3})
	cfg := s.Config()
	if cfg.QuietFrom != "23:00" {
		t.Errorf("QuietFrom не нормализован: %q", cfg.QuietFrom)
	}
	if cfg.MaxDownMB != 0 {
		t.Errorf("отрицательный лимит должен стать 0: %d", cfg.MaxDownMB)
	}
}
