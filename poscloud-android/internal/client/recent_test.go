package client

import (
	"os"
	"path/filepath"
	"testing"

	eng "poscloud-engine"
)

// TestControllerRecentEvents — события от движка копятся, dirty-флаг
// срабатывает один раз, история переживает перезапуск контроллера.
func TestControllerRecentEvents(t *testing.T) {
	root := t.TempDir()
	c := NewController(root)

	if len(c.RecentEvents()) != 0 {
		t.Fatal("новая установка должна начинаться с пустой лентой")
	}
	if c.EventsDirtyAndReset() {
		t.Fatal("пустая лента не должна быть «грязной»")
	}

	c.recordEvents([]eng.ChangeEvent{
		{Name: "a.txt", Op: "up", Time: 100},
		{Name: "b.txt", Op: "down", Time: 101},
	})
	got := c.RecentEvents()
	if len(got) != 2 || got[0].Name != "a.txt" || got[1].Op != "down" {
		t.Fatalf("события: %+v", got)
	}
	if !c.EventsDirtyAndReset() {
		t.Fatal("после новых событий лента должна быть «грязной»")
	}
	if c.EventsDirtyAndReset() {
		t.Fatal("dirty-флаг должен сбрасываться при опросе")
	}

	// перезапуск: история читается из events.json
	c2 := NewController(root)
	got2 := c2.RecentEvents()
	if len(got2) != 2 || got2[1].Name != "b.txt" {
		t.Fatalf("история не пережила перезапуск: %+v", got2)
	}

	c2.ClearRecentEvents()
	if len(c2.RecentEvents()) != 0 {
		t.Fatal("лента не очистилась")
	}
	if _, err := os.Stat(filepath.Join(root, "events.json")); err != nil {
		t.Fatalf("events.json должен перезаписаться пустым: %v", err)
	}
}

// TestControllerRecentEventsLimit — лента обрезается до лимита, старые
// события уходят.
func TestControllerRecentEventsLimit(t *testing.T) {
	c := NewController(t.TempDir())
	batch := make([]eng.ChangeEvent, recentEventsLimit+50)
	for i := range batch {
		batch[i] = eng.ChangeEvent{Name: string(rune('a'+i%26)) + ".txt", Op: "down", Time: int64(i)}
	}
	c.recordEvents(batch)
	got := c.RecentEvents()
	if len(got) != recentEventsLimit {
		t.Fatalf("ожидалось %d событий, получено %d", recentEventsLimit, len(got))
	}
	if got[0].Time != 50 { // первые 50 отрезаны
		t.Errorf("первое событие: %+v, хочу Time=50", got[0])
	}
}
