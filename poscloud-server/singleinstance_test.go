//go:build windows

package main

import "testing"

// Примитив «один экземпляр»: второй запрос того же имени должен быть отклонён.
// Второй CreateMutex с тем же именем в пределах процесса тоже возвращает
// ERROR_ALREADY_EXISTS, поэтому проверка детерминированная.
func TestSingleInstanceDetectsSecondAcquire(t *testing.T) {
	name := `Local\POSCloud-Server-Test-SingleInstance`
	if !acquireSingleInstance(name) {
		t.Fatal("первый вызов должен вернуть true (экземпляр первый)")
	}
	if acquireSingleInstance(name) {
		t.Fatal("второй вызов должен вернуть false (программа уже запущена)")
	}
}

// Поиск несуществующего окна не должен падать и должен завершаться по таймауту.
func TestActivateMissingWindowReturns(t *testing.T) {
	activateExistingWindow("POSCloud Server — окно-которого-нет")
}
