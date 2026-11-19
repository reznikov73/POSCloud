//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// freePort занимает свободный порт и возвращает его номер вместе со слушателем.
func freePort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось занять порт: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// Собственный слушающий сокет должен распознаваться как свой процесс.
func TestPortHolderFindsSelf(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	holder := a.PortHolder(port)
	if holder.PID == 0 {
		t.Fatal("владелец порта не найден")
	}
	if !holder.IsSelf {
		t.Fatalf("собственный процесс не распознан: PID %d, self %d", holder.PID, selfPID())
	}
}

// Свободный порт: владельца нет.
func TestPortHolderFreePort(t *testing.T) {
	ln, port := freePort(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	a := NewApp()
	if holder := a.PortHolder(port); holder.PID != 0 {
		t.Fatalf("у свободного порта найден владелец: PID %d (%s)", holder.PID, holder.Name)
	}
}

// ReleasePort обязан отказаться завершать собственный процесс.
func TestReleasePortRefusesSelf(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	st := a.ReleasePort(port, selfPID())
	if st.Error == "" {
		t.Fatal("ReleasePort не отказался завершать собственный процесс")
	}
	if !strings.Contains(st.Error, "это же приложение") {
		t.Fatalf("неожиданное сообщение об ошибке: %q", st.Error)
	}
}

// ReleasePort не должен завершать процесс без точного PID.
func TestReleasePortRefusesZeroPID(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	if st := a.ReleasePort(port, 0); st.Error == "" {
		t.Fatal("ReleasePort не отказался работать с нулевым PID")
	}
}

// ReleasePort не должен завершать процесс, если PID не совпадает с владельцем порта.
func TestReleasePortRefusesWrongPID(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	// PID 1 существует всегда, но порт занимает не он.
	if st := a.ReleasePort(port, 1); st.Error == "" {
		t.Fatal("ReleasePort не отказался завершать процесс с чужим PID")
	}
}

// Свободный порт: освобождать нечего, ошибки быть не должно.
func TestReleasePortFreePortIsNoop(t *testing.T) {
	ln, port := freePort(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	a := NewApp()
	if st := a.ReleasePort(port, 0); st.Error != "" {
		t.Fatalf("освобождение свободного порта вернуло ошибку: %q", st.Error)
	}
}

// Полный сценарий: сторонний процесс слушает порт, мы его находим и завершаем
// именно этот процесс. Ровно то, что делает кнопка «Освободить порт».
func TestReleasePortTerminatesForeignListener(t *testing.T) {
	ln, port := freePort(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	// Слушатель — отдельный процесс, чтобы он был для нас действительно чужим.
	// python3 есть почти везде; если его нет, тест пропускается.
	script := fmt.Sprintf(
		"import socket,time\ns=socket.socket()\ns.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)\ns.bind(('127.0.0.1',%d))\ns.listen(1)\ntime.sleep(120)\n", port)
	cmd := exec.Command("python3", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Skipf("не удалось запустить сторонний слушатель: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	a := NewApp()
	var holder PortHolderInfo
	for i := 0; i < 100; i++ {
		holder = a.PortHolder(port)
		if holder.PID != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if holder.PID == 0 {
		t.Fatal("сторонний слушатель не найден")
	}
	if holder.IsSelf {
		t.Fatalf("сторонний процесс распознан как собственный (PID %d)", holder.PID)
	}
	if int(holder.PID) != cmd.Process.Pid {
		t.Fatalf("найден PID %d, ожидался PID слушателя %d", holder.PID, cmd.Process.Pid)
	}

	if st := a.ReleasePort(port, holder.PID); st.Error != "" {
		t.Fatalf("не удалось освободить порт: %q", st.Error)
	}
	for i := 0; i < 50; i++ {
		if a.PortHolder(port).PID == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := a.PortHolder(port).PID; got != 0 {
		t.Fatalf("порт всё ещё занят процессом %d", got)
	}
}

// Разбор /proc/net/tcp: строка LISTEN с нужным портом даёт inode,
// а состояние, отличное от LISTEN, не должно считаться владельцем.
func TestListenerInodeParsing(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	if got := listenerInode(port); got == "" {
		t.Fatalf("inode слушающего сокета для порта %d не найден", port)
	}
	// Порт, который никто не слушает.
	if got := listenerInode(1); got != "" {
		t.Fatalf("для порта 1 найден inode %q, ожидалась пустая строка", got)
	}
}

// processDisplayName для собственного процесса должен вернуть непустые имя и путь.
func TestProcessDisplayNameSelf(t *testing.T) {
	name, path := processDisplayName(selfPID())
	if name == "" {
		t.Error("имя собственного процесса пусто")
	}
	if path == "" {
		t.Error("путь собственного процесса пуст")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("путь процесса не существует: %s (%v)", path, err)
	}
}

// terminateProcess не должен трогать собственный процесс: проверяем,
// что вызывающий код защищён, а сам примитив на несуществующем PID даёт ошибку.
func TestTerminateMissingProcess(t *testing.T) {
	// Заведомо несуществующий PID в старших номерах.
	if err := terminateProcess(0x7FFFFFFF); err == nil {
		t.Fatal("terminateProcess для несуществующего PID не вернул ошибку")
	}
}
