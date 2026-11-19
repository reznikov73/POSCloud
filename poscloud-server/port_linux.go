//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ---------- платформенная часть: /proc и сигналы ----------
//
// Общая логика проверок и текст сообщений живут в port_common.go.
// Здесь только то, что зависит от Linux: разбор /proc/net/tcp и /proc/<pid>.
//
// Владелец слушающего сокета ищется так:
//   1. в /proc/net/tcp и /proc/net/tcp6 находим строку с нужным локальным портом
//      в состоянии LISTEN (0A) и запоминаем её inode;
//   2. идём по /proc/<pid>/fd/* и ищем символическую ссылку socket:[<inode>];
//   3. владелец — тот PID, у которого нашлась ссылка.
//
// Чтение /proc/<pid>/fd чужого процесса требует прав: под обычным пользователем
// владелец определится только для своих процессов. Это осознанное ограничение.

// tcpListenState — код состояния LISTEN в /proc/net/tcp (0A в шестнадцатеричном виде).
const tcpListenState = "0A"

// listenerPID возвращает PID процесса, слушающего TCP-порт; 0 — порт свободен.
func listenerPID(port int) uint32 {
	inode := listenerInode(port)
	if inode == "" {
		return 0
	}
	return pidBySocketInode(inode)
}

// listenerInode находит inode слушающего сокета для порта.
// Пустая строка — порт никто не слушает.
func listenerInode(port int) string {
	want := fmt.Sprintf(":%04X", port)
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		// Первая строка — заголовок.
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			// fields[1] — local_address вида 0100007F:1F90 (адрес:порт).
			if !strings.HasSuffix(fields[1], want) {
				continue
			}
			if fields[3] != tcpListenState {
				continue
			}
			// fields[9] — inode сокета.
			if fields[9] != "0" {
				return fields[9]
			}
		}
	}
	return ""
}

// pidBySocketInode ищет процесс, у которого открыт сокет с указанным inode.
func pidBySocketInode(inode string) uint32 {
	want := "socket:[" + inode + "]"
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if target == want {
				return uint32(pid)
			}
		}
	}
	return 0
}

// processDisplayName возвращает имя и полный путь процесса (пустые строки, если недоступно).
func processDisplayName(pid uint32) (string, string) {
	base := filepath.Join("/proc", strconv.FormatUint(uint64(pid), 10))
	name := ""
	if b, err := os.ReadFile(filepath.Join(base, "comm")); err == nil {
		name = strings.TrimSpace(string(b))
	}
	path := ""
	if p, err := os.Readlink(filepath.Join(base, "exe")); err == nil {
		// Для удалённого файла ядро дописывает " (deleted)".
		path = strings.TrimSuffix(p, " (deleted)")
	}
	if name == "" && path != "" {
		name = filepath.Base(path)
	}
	return name, path
}

// selfPID возвращает PID текущего процесса.
func selfPID() uint32 { return uint32(os.Getpid()) }

// terminateProcess просит процесс завершиться (SIGTERM) и ждёт, пока он уйдёт.
// SIGKILL не используется: процесс должен успеть закрыть сокет и сохранить данные.
func terminateProcess(pid uint32) error {
	p, err := os.FindProcess(int(pid))
	if err != nil {
		return errors.New("не удалось открыть процесс: " + err.Error())
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return errors.New("не удалось завершить процесс: " + err.Error())
	}
	// Даём процессу время на корректное завершение: до 3 секунд.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("процесс %d не завершился по SIGTERM", pid)
}

// processAlive сообщает, существует ли процесс.
func processAlive(pid uint32) bool {
	base := filepath.Join("/proc", strconv.FormatUint(uint64(pid), 10))
	if _, err := os.Stat(base); err != nil {
		return false
	}
	// Процесс-зомби ещё виден в /proc, но уже не работает.
	if b, err := os.ReadFile(filepath.Join(base, "stat")); err == nil {
		// Формат: pid (comm) state ...; comm может содержать пробелы и скобки,
		// поэтому состояние ищем после последней закрывающей скобки.
		s := string(b)
		if i := strings.LastIndex(s, ")"); i >= 0 && i+2 < len(s) {
			if s[i+2] == 'Z' {
				return false
			}
		}
	}
	return true
}
