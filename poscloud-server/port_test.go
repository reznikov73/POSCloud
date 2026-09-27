//go:build windows

package main

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// freePort занимает свободный порт и возвращает его номер.
func freePort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось занять порт: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// Владелец порта определяется, и собственный процесс распознаётся как свой.
func TestPortHolderFindsSelf(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	h := a.PortHolder(port)
	if h.PID != windows.GetCurrentProcessId() {
		t.Fatalf("PID=%d, ожидался текущий процесс %d", h.PID, windows.GetCurrentProcessId())
	}
	if !h.IsSelf {
		t.Fatal("собственный процесс не распознан как свой")
	}
	if h.Name == "" {
		t.Log("имя процесса получить не удалось (не критично)")
	}
}

// Освобождённый порт считается свободным.
func TestPortHolderFreePort(t *testing.T) {
	ln, port := freePort(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	if h := a.PortHolder(port); h.PID != 0 {
		t.Fatalf("порт считается занятым: %+v", h)
	}
}

// ReleasePort обязан отказаться завершать собственный процесс.
func TestReleasePortRefusesSelf(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	st := a.ReleasePort(port, windows.GetCurrentProcessId())
	if !strings.Contains(st.Error, "это же приложение") {
		t.Fatalf("ожидался отказ с пояснением, получено: %q", st.Error)
	}
}

// ReleasePort не должен завершать процесс без точного PID.
func TestReleasePortRefusesZeroPID(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	if st := a.ReleasePort(port, 0); st.Error == "" {
		t.Fatal("ожидался отказ при нулевом PID")
	}
}

// ReleasePort не должен завершать процесс, если PID не совпадает с владельцем порта.
func TestReleasePortRefusesWrongPID(t *testing.T) {
	ln, port := freePort(t)
	defer ln.Close()

	a := NewApp()
	// 4 — системный процесс, заведомо не наш слушатель; завершать его нельзя.
	if st := a.ReleasePort(port, 4); st.Error == "" {
		t.Fatal("ожидался отказ при несовпадающем PID")
	}
}

// Для свободного порта освобождать нечего — ошибок быть не должно.
func TestReleasePortFreePortIsNoop(t *testing.T) {
	ln, port := freePort(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	if st := a.ReleasePort(port, 0); st.Error != "" {
		t.Fatalf("для свободного порта ошибок быть не должно: %q", st.Error)
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
	script := fmt.Sprintf(
		"$l=[System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback,%d); $l.Start(); Start-Sleep -Seconds 120", port)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
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
		t.Fatal("сторонний слушатель не найден в таблице портов")
	}
	if holder.PID == windows.GetCurrentProcessId() {
		t.Fatal("найден собственный процесс вместо стороннего")
	}
	if int(holder.PID) != cmd.Process.Pid {
		t.Fatalf("найден PID %d, ожидался PID слушателя %d", holder.PID, cmd.Process.Pid)
	}
	if holder.IsSelf {
		t.Fatal("сторонний процесс распознан как собственный")
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
