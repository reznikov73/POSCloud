package main

import "fmt"

// ---------- кто занимает порт и как его освободить ----------
//
// Сервер слушает порт сам, но порт может занять и посторонний процесс.
// Кнопка «Освободить порт» сначала показывает, кто держит порт, и только после
// подтверждения пользователя завершает именно этот процесс — по его PID.
//
// Определение владельца порта и завершение процесса платформенные:
// port_windows.go (таблица TCP через iphlpapi) и port_linux.go (/proc).
// Проверки безопасности общие — они не должны расходиться между платформами.

// PortHolderInfo — процесс, слушающий TCP-порт.
type PortHolderInfo struct {
	PID    uint32 `json:"pid"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsSelf bool   `json:"isSelf"`
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
	return PortHolderInfo{PID: pid, Name: name, Path: path, IsSelf: pid == selfPID()}
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

	if err := terminateProcess(pid); err != nil {
		a.setServerError(err.Error())
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
