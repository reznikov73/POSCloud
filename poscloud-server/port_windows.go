//go:build windows

package main

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- платформенная часть: таблица TCP и завершение процесса ----------
//
// Общая логика проверок и текст сообщений живут в port_common.go.
// Здесь только то, что зависит от Windows API.

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

// selfPID возвращает PID текущего процесса.
func selfPID() uint32 { return windows.GetCurrentProcessId() }

// terminateProcess завершает процесс по PID.
func terminateProcess(pid uint32) error {
	h, err := windows.OpenProcess(processTerminate, false, pid)
	if err != nil {
		return errors.New("не удалось открыть процесс: " + err.Error())
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return errors.New("не удалось завершить процесс: " + err.Error())
	}
	return nil
}
