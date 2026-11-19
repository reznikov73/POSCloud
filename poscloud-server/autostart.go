package main

import "strings"

// ---------- общая часть автозапуска ----------
//
// Механизм включения разный (реестр в Windows, .desktop-файл в Linux),
// но команда запуска и её разбор одинаковые — поэтому они здесь, без build-тегов.

// autostartCommand собирает команду автозапуска: при входе в систему программа
// стартует скрыто в трее, сервер поднимается сразу.
func autostartCommand(exe string) string {
	return `"` + exe + `" -autostart -hidden`
}

// desktopContent собирает .desktop-файл автозапуска (freedesktop autostart spec).
// TryExec не содержит кавычек и флагов — это путь, по которому среда проверяет,
// что программа ещё существует.
func desktopContent(exe string) string {
	return `[Desktop Entry]
Type=Application
Name=POSCloud Server
Comment=Синхронизация папок между устройствами
Exec=` + autostartCommand(exe) + `
TryExec=` + exe + `
Terminal=false
X-GNOME-Autostart-enabled=true
`
}

// desktopExec возвращает значение строки Exec= из .desktop-файла.
// Пустая строка означает, что строки нет.
func desktopExec(data string) string {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Exec=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
		}
	}
	return ""
}

// autostartNeedsUpdate сообщает, что существующая запись автозапуска указывает
// на этот exe, но ещё не содержит -hidden (запись прежней версии).
// Пустой exe означает «путь неизвестен»: такую запись не трогаем.
func autostartNeedsUpdate(value, exe string) bool {
	if value == "" || exe == "" {
		return false
	}
	if !strings.Contains(strings.ToLower(value), strings.ToLower(exe)) {
		return false
	}
	return !strings.Contains(strings.ToLower(value), "-hidden")
}
