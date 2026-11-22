#!/bin/bash
# POSCloud Server — управление службой.
#
# Запуск без аргументов открывает меню. С аргументом выполняет одно действие:
#   status | start | stop | restart | log | gui | web | info
#
# Служба systemd запускает сервер без окна и трея. Режим "gui" останавливает
# службу и открывает окно: они делят замок, два экземпляра не уживутся.

set -u

UNIT="poscloud-server"
BIN="/home/rez/bin/poscloud-server"
WEB="https://127.0.0.1:8090/"

# Цвета только если вывод в терминал.
if [ -t 1 ]; then
	C_OK=$'\033[32m'; C_ERR=$'\033[31m'; C_WARN=$'\033[33m'
	C_DIM=$'\033[90m'; C_B=$'\033[1m'; C_0=$'\033[0m'
else
	C_OK=; C_ERR=; C_WARN=; C_DIM=; C_B=; C_0=
fi

ok()   { printf '%s%s%s\n' "$C_OK" "$1" "$C_0"; }
err()  { printf '%s%s%s\n' "$C_ERR" "$1" "$C_0"; }
warn() { printf '%s%s%s\n' "$C_WARN" "$1" "$C_0"; }
dim()  { printf '%s%s%s\n' "$C_DIM" "$1" "$C_0"; }

# need_root проверяет, что systemctl доступен без пароля.
# Правило лежит в /etc/sudoers.d/poscloud-server и ограничено этой службой.
need_root() {
	# is-active возвращает 3 для inactive — это ответ, а не отказ в доступе.
	# Отказ sudo выглядит иначе: код 1 и текст про пароль.
	case "$(sudo -n systemctl is-active "$UNIT" 2>/dev/null)" in
		active|inactive|failed|activating|deactivating) return 0;;
	esac
	if sudo -n -l systemctl >/dev/null 2>&1; then
		return 0
	fi
	err "Нет доступа к systemctl без пароля."
	dim "Проверьте /etc/sudoers.d/poscloud-server (права 440, правила для $UNIT)."
	dim "Либо запустите с паролем: sudo systemctl $1 $UNIT"
	return 1
}

state() {
	# Вывод is-active — это и есть состояние. Пустой вывод значит,
	# что команда не выполнилась (нет прав), а не «остановлена».
	local out
	out="$(sudo -n systemctl is-active "$UNIT" 2>/dev/null)"
	case "$out" in
		active|activating)       echo active;;
		inactive|failed|deactivating) echo inactive;;
		*)                       echo unknown;;
	esac
}

enabled() {
	sudo -n systemctl is-enabled "$UNIT" 2>/dev/null || echo unknown
}

# gui_running ищет окно или процесс не-служебного запуска.
gui_running() {
	# Процесс с -service — это служба; всё остальное — режим окна.
	pgrep -af "$BIN" 2>/dev/null | grep -v -- "-service" | grep -v grep >/dev/null
}

port_busy() {
	ss -tln 2>/dev/null | grep -q ':8090 '
}

health() {
	curl -sk -m 3 "https://127.0.0.1:8090/health" 2>/dev/null | grep -q '"status":"ok"'
}

action_status() {
	printf '%sСлужба%s\n' "$C_B" "$C_0"
	local st; st="$(state)"
	case "$st" in
		active)   ok "  состояние:  active (работает)";;
		inactive) warn "  состояние:  inactive (остановлена)";;
		*)        err  "  состояние:  неизвестно (нет доступа к systemctl)";;
	esac
	printf '  автозапуск: %s\n' "$(enabled)"

	printf '%sПроверки%s\n' "$C_B" "$C_0"
	if port_busy; then ok "  порт 8090:  занят"; else warn "  порт 8090:  свободен"; fi
	if health; then ok "  /health:    отвечает"; else err "  /health:    не отвечает"; fi

	if gui_running; then
		warn "  окно:       запущено (процесс без -service)"
	else
		dim  "  окно:       не запущено"
	fi

	local pid
	pid="$(sudo -n systemctl show "$UNIT" -p MainPID --value 2>/dev/null)"
	if [ -n "${pid:-}" ] && [ "$pid" != "0" ]; then
		printf '  PID:        %s\n' "$pid"
		local mem
		mem="$(ps -o rss= -p "$pid" 2>/dev/null | awk '{printf "%.0f", $1/1024}')"
		[ -n "${mem:-}" ] && printf '  память:     %s МБ\n' "$mem"
	fi
}

action_start() {
	if [ "$(state)" = "active" ]; then
		warn "Служба уже работает."
		return 0
	fi
	if gui_running; then
		err "Открыто окно программы. Сначала закройте его — они делят замок."
		return 1
	fi
	if sudo -n systemctl start "$UNIT"; then
		sleep 2
		if health; then
			ok "Служба запущена. https://127.0.0.1:8090/"
		else
			warn "Служба запущена, но /health не отвечает. Смотрите журнал: $0 log"
		fi
	else
		err "Не удалось запустить службу."
		return 1
	fi
}

action_stop() {
	if [ "$(state)" != "active" ]; then
		warn "Служба не работает."
		return 0
	fi
	if sudo -n systemctl stop "$UNIT"; then
		ok "Служба остановлена. Клиенты не смогут синхронизироваться."
	else
		err "Не удалось остановить службу."
		return 1
	fi
}

action_restart() {
	if sudo -n systemctl restart "$UNIT"; then
		sleep 2
		if health; then
			ok "Служба перезапущена."
		else
			warn "Перезапущена, но /health не отвечает. Журнал: $0 log"
		fi
	else
		err "Не удалось перезапустить службу."
		return 1
	fi
}

action_log() {
	dim "Журнал службы (Ctrl+C — выход)"
	sudo -n journalctl -u "$UNIT" -f -n 40
}

action_gui() {
	if [ ! -x "$BIN" ]; then
		err "Не найден бинарник: $BIN"
		return 1
	fi
	if [ "$(state)" = "active" ]; then
		dim "Останавливаю службу, чтобы освободить замок…"
		sudo -n systemctl stop "$UNIT" || { err "Не удалось остановить службу."; return 1; }
	fi
	if gui_running; then
		warn "Окно уже запущено."
		return 0
	fi
	if [ -z "${DISPLAY:-}" ]; then
		err "Нет DISPLAY: окно можно открыть только в графической сессии."
		dim "Если вы по SSH — включите X11-форвардинг (ssh -X)."
		return 1
	fi
	dim "Запускаю окно…"
	nohup "$BIN" >/tmp/poscloud-gui.log 2>&1 &
	sleep 3
	if gui_running; then
		ok "Окно запущено. Закрытие окна прячет программу в трей."
		dim "Вернуться к службе: $0 service"
	else
		err "Окно не запустилось. Лог: /tmp/poscloud-gui.log"
		tail -5 /tmp/poscloud-gui.log 2>/dev/null | sed 's/^/  /'
		return 1
	fi
}

action_service() {
	if gui_running; then
		dim "Закрываю окно…"
		pkill -f "$BIN" 2>/dev/null
		for _ in 1 2 3 4 5; do
			gui_running || break
			sleep 1
		done
		pkill -9 -f "$BIN" 2>/dev/null
		sleep 1
	fi
	action_start
}

action_web() {
	if health; then
		dim "Открываю $WEB"
		xdg-open "$WEB" >/dev/null 2>&1 &
		ok "Браузер запущен."
	else
		err "Сервер не отвечает. Сначала запустите: $0 start"
		return 1
	fi
}

action_info() {
	cat <<INFO
POSCloud Server — где что лежит

  Программа    $BIN
  Настройки    /home/rez/bin/server.json
  Пароль       /home/rez/bin/admin.json
  Журнал       /home/rez/bin/server.log (режим окна)
  Данные       /home/rez/poscloud-data/
  Исходники    /home/rez/Work/POSCloud/POSCloud-Server/
  Юнит         /etc/systemd/system/poscloud-server.service
  Админка      https://193.7.203.222:8090/
  Документация /home/rez/Work/POSCloud/POSCloud-Server/poscloud-server/deploy/README-linux.md
INFO
}

menu() {
	while true; do
		printf '\n%s POSCloud Server%s\n' "$C_B" "$C_0"
		action_status
		printf '\n'
		printf '  1) Запустить службу\n'
		printf '  2) Остановить службу\n'
		printf '  3) Перезапустить службу\n'
		printf '  4) Открыть окно программы (служба будет остановлена)\n'
		printf '  5) Вернуться к службе (закрыть окно, запустить службу)\n'
		printf '  6) Открыть веб-админку\n'
		printf '  7) Журнал службы\n'
		printf '  8) Где что лежит\n'
		printf '  0) Выход\n'
		printf '\nВыбор: '
		read -r choice || return 0
		case "$choice" in
			1) action_start;;
			2) action_stop;;
			3) action_restart;;
			4) action_gui;;
			5) action_service;;
			6) action_web;;
			7) action_log; return 0;;
			8) action_info;;
			0|q|Q) return 0;;
			*) warn "Нет такого пункта.";;
		esac
	done
}

case "${1:-}" in
	status)  action_status;;
	start)   action_start;;
	stop)    action_stop;;
	restart) action_restart;;
	log)     action_log;;
	gui)     action_gui;;
	service) action_service;;
	web)     action_web;;
	info)    action_info;;
	"")      menu;;
	*)
		err "Неизвестная команда: $1"
		printf 'Использование: %s [status|start|stop|restart|log|gui|service|web|info]\n' "$(basename "$0")"
		exit 1
		;;
esac
