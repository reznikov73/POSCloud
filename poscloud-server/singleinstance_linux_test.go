//go:build linux

package main

import "testing"

// Второй вызов acquireSingleInstance в том же процессе должен получить отказ:
// flock на уже открытом дескрипторе не даёт взять блокировку повторно.
func TestAcquireSingleInstanceTwice(t *testing.T) {
	if p, err := singleInstancePath(); err == nil {
		_ = p
	}
	// Первый экземпляр уже мог занять замок в этом процессе — освобождаем
	// состояние, чтобы тест не зависел от порядка запуска.
	if singleInstanceFile != nil {
		_ = singleInstanceFile.Close()
		singleInstanceFile = nil
	}

	if !acquireSingleInstance("test") {
		t.Fatal("первый вызов должен вернуть true (экземпляр первый)")
	}
	if acquireSingleInstance("test") {
		t.Fatal("второй вызов должен вернуть false (программа уже запущена)")
	}
}

// Путь замка должен быть абсолютным и лежать вне /tmp: предсказуемый путь
// в общем каталоге позволил бы другому пользователю подменить файл.
func TestSingleInstancePathIsOutsideTmp(t *testing.T) {
	p, err := singleInstancePath()
	if err != nil {
		t.Fatalf("не удалось получить путь замка: %v", err)
	}
	if p == "" {
		t.Fatal("путь замка пуст")
	}
	if p[0] != '/' {
		t.Fatalf("путь замка должен быть абсолютным: %s", p)
	}
	if len(p) > 5 && p[:5] == "/tmp/" {
		t.Fatalf("файл замка не должен лежать в /tmp: %s", p)
	}
}

// activateExistingWindow в Linux — no-op и не должна падать.
func TestActivateExistingWindowIsNoop(t *testing.T) {
	activateExistingWindow("POSCloud Server — окно-которого-нет")
}
