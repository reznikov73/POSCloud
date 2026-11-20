//go:build linux

package main

import (
	"os"
	"syscall"
	"testing"
)

// Второй вызов acquireSingleInstance в том же процессе должен получить отказ:
// flock на уже открытом дескрипторе не даёт взять блокировку повторно.
func TestAcquireSingleInstanceTwice(t *testing.T) {
	// Замок может быть занят работающей программой: тогда проверить нечего.
	if singleInstanceFile == nil {
		if p, _ := singleInstancePath(); p != "" {
			if f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0644); err == nil {
				locked := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil
				_ = f.Close()
				if locked {
					t.Skip("замок держит работающий экземпляр программы")
				}
			}
		}
	}

	// Освобождаем состояние, чтобы тест не зависел от порядка запуска.
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
