//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- кто занимает порт и как его освободить ----------
//
// Сервер слушает порт сам, но порт может занять и посторонний процесс.
// Кнопка «Освободить порт» сначала показывает, кто держит порт, и только после
// подтверждения пользователя завершает именно этот процесс — по его PID.

// PortHolderInfo — процесс, слушающий TCP-порт.
type PortHolderInfo struct {
	PID    uint32 `json:"pid"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsSelf bool   `json:"isSelf"`
}

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	afINET                   = 2 // AF_INET
	tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER

	processTerminate        = 0x0001 // PROCESS_TERMINATE
	processQueryLimitedInfo = 0x1000 // PROCESS_QUERY_LIMITED_INFORMATION
)

// mibTcpRowOwnerPID — строка таблицы TCP (MIB_TCPROW_OWNER_PID).
// Важно: полей шесть — после локального порта идут адрес и порт удалённой стороны.
type mibTcpRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// listenerPID возвращает PID процесса, слушающего TCP-порт; 0 — порт свободен.
func listenerPID(port int) uint32 {
	var size uint32
	// Первый вызов с нулевым буфером возвращает нужный размер таблицы.
	r, _, _ := procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, afINET, tcpTableOwnerPIDListener, 0)
	if r != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size == 0 {
		return 0
	}
	buf := make([]byte, size)
	r, _, _ = procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, afINET, tcpTableOwnerPIDListener, 0)
	if r != 0 {
		return 0
	}
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := int(unsafe.Sizeof(mibTcpRowOwnerPID{}))
	for i := 0; i < int(count); i++ {
		off := 4 + i*rowSize
		if off+rowSize > len(buf) {
			break
		}
		row := (*mibTcpRowOwnerPID)(unsafe.Pointer(&buf[off]))
		// Порт лежит в младших 16 битах в сетевом порядке байт.
		p := int((row.LocalPort>>8)&0xff) | int(row.LocalPort&0xff)<<8
		if p == port {
			return row.OwningPID
		}
	}
	return 0
}

// processDisplayName возвращает имя и полный путь процесса (пустые строки, если недоступно).
func processDisplayName(pid uint32) (string, string) {
	h, err := windows.OpenProcess(processQueryLimitedInfo, false, pid)
	if err != nil {
		return "", ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", ""
	}
	full := windows.UTF16ToString(buf[:size])
	return filepath.Base(full), full
}

// PortHolder сообщает, какой процесс слушает порт. Нулевой PID — порт свободен.
func (a *App) PortHolder(port int) PortHolderInfo {
	if port <= 0 {
		port = 8090
	}
	pid := listenerPID(port)
	if pid == 0 {
		return PortHolderInfo{}
	}
	name, path := processDisplayName(pid)
	return PortHolderInfo{PID: pid, Name: name, Path: path, IsSelf: pid == windows.GetCurrentProcessId()}
}

// ReleasePort освобождает порт. Если наш сервер запущен — просто останавливает его.
// Если порт занят посторонним процессом — завершает его, но только когда переданный
// PID совпадает с текущим владельцем порта: это защита от завершения не того процесса.
func (a *App) ReleasePort(port int, pid uint32) ServerState {
	if port <= 0 {
		port = 8090
	}

	a.mu.Lock()
	running := a.srvRun
	a.mu.Unlock()
	if running {
		return a.StopServer()
	}

	holder := a.PortHolder(port)
	if holder.PID == 0 {
		a.setServerError("")
		a.log(fmt.Sprintf("порт %d свободен", port))
		return a.GetServerState()
	}
	if holder.IsSelf {
		a.setServerError("порт слушает это же приложение — сначала остановите сервер")
		return a.GetServerState()
	}
	if pid == 0 || pid != holder.PID {
		a.setServerError(fmt.Sprintf("порт %d занимает другой процесс (PID %d) — обновите данные и повторите", port, holder.PID))
		return a.GetServerState()
	}

	h, err := windows.OpenProcess(processTerminate, false, pid)
	if err != nil {
		a.setServerError("не удалось открыть процесс: " + err.Error())
		a.log(fmt.Sprintf("не удалось открыть процесс %d (%s): %v", pid, holder.Name, err))
		return a.GetServerState()
	}
	defer windows.CloseHandle(h)

	if err := windows.TerminateProcess(h, 1); err != nil {
		a.setServerError("не удалось завершить процесс: " + err.Error())
		a.log(fmt.Sprintf("не удалось завершить процесс %d (%s): %v", pid, holder.Name, err))
		return a.GetServerState()
	}

	a.log(fmt.Sprintf("завершён процесс %d (%s), занимавший порт %d", pid, holder.Name, port))
	a.setServerError("")
	return a.GetServerState()
}

func (a *App) setServerError(msg string) {
	a.mu.Lock()
	a.srvErr = msg
	a.mu.Unlock()
}
