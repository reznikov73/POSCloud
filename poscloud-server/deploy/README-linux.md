# POSCloud Server на Linux — запуск и остановка

Практическая инструкция: где лежат файлы, как запускать службу и как запускать
программу с окном. Проверено на Ubuntu 22.04.

---

## 1. Где что лежит

### Бинарники

| Файл | Путь | Что это |
|---|---|---|
| Программа | `/home/rez/bin/poscloud-server` | один бинарник, два режима |
| Настройки | `/home/rez/bin/server.json` | порт, каталог данных, TLS |
| Пароль админки | `/home/rez/bin/admin.json` | хеш, права 0600 |
| Журнал | `/home/rez/bin/server.log` | пишется в режиме окна |

### Данные

```
/home/rez/poscloud-data/          ← каталог хранения (468 МБ)
├── users.json                    ← пользователи и токены
├── admin.json                    ← копия учётных данных админки
├── tls/                          ← сертификат и ключ
│   ├── server.crt
│   └── server.key                ← права 0600
└── rezlinux/                     ← файлы пользователя
    ├── .manifest.json            ← версии и надгробия
    └── .blocks/                  ← блочное хранилище (222 МБ)
```

### Исходники

```
/home/rez/Work/POSCloud/POSCloud-Server/    ← корень репозитория
├── engine/                 общий движок синхронизации
├── poscloud-server/        сервер (то, что здесь запускается)
│   ├── deploy/
│   │   └── poscloud-server.service    ← юнит systemd
│   ├── assets/             иконки
│   └── frontend/dist/      веб-интерфейс
├── poscloud-client/        клиент (только Windows)
├── lite/                   лёгкий клиент (только Windows)
└── installer/              установщик Windows
```

Репозиторий: `https://github.com/reznikov73/POSCloud`, ветка `main`.

### Служебные файлы systemd

| Файл | Путь |
|---|---|
| Юнит | `/etc/systemd/system/poscloud-server.service` |
| Автозапуск | `/etc/systemd/system/multi-user.target.wants/poscloud-server.service` |

---

## 2. Режим службы (без окна) — обычный режим работы

Служба запускается при загрузке ОС, работает без графической сессии и
перезапускается при падении. Управление — через веб-админку в браузере.

### Состояние

```bash
sudo systemctl status poscloud-server
```

Короткая проверка:

```bash
sudo systemctl is-active poscloud-server    # active — работает
sudo systemctl is-enabled poscloud-server   # enabled — в автозагрузке
```

Проверка, что сервер отвечает:

```bash
curl -sk https://127.0.0.1:8090/health
# {"service":"poscloud-server","status":"ok"}
```

### Остановить

```bash
sudo systemctl stop poscloud-server
```

### Запустить

```bash
sudo systemctl start poscloud-server
```

### Перезапустить (после смены настроек)

```bash
sudo systemctl restart poscloud-server
```

### Выключить автозапуск при загрузке

```bash
sudo systemctl disable poscloud-server
```

Если служба уже запущена, `stop` нужен отдельно — `disable` не останавливает.

### Включить автозапуск обратно

```bash
sudo systemctl enable poscloud-server
```

### Журнал

```bash
sudo journalctl -u poscloud-server -f          # в реальном времени
sudo journalctl -u poscloud-server -n 50       # последние 50 строк
sudo journalctl -u poscloud-server --since today
```

---

## 3. Режим с окном (GUI) — для настройки

Окно нужно, чтобы посмотреть состояние, выбрать каталог мышью, открыть папку
в файловом менеджере. Для повседневной работы не требуется.

**Важно:** служба и окно используют один и тот же замок (`flock`). Пока служба
работает, второй экземпляр молча завершится — защита от двойного запуска.

### Порядок: остановить службу, потом запустить окно

```bash
sudo systemctl stop poscloud-server
/home/rez/bin/poscloud-server
```

Окно откроется в текущей графической сессии. Закрытие окна **не завершает**
программу — она уходит в трей.

### Вернуться к службе

```bash
# сначала закрыть окно полностью (выход через трей), затем:
sudo systemctl start poscloud-server
```

Если окно осталось висеть, служба не займёт порт:

```bash
pgrep -af poscloud-server          # найти процесс
kill <PID>                          # завершить (SIGTERM)
kill -9 <PID>                       # если не реагирует
```

### Запуск окна с другими параметрами

```bash
/home/rez/bin/poscloud-server -port 8091 -data /path/to/data
```

Полезные флаги:

| Флаг | Что делает |
|---|---|
| `-service` | без окна и трея (так запускает systemd) |
| `-port N` | порт вместо значения из `server.json` |
| `-data PATH` | каталог данных вместо значения из `server.json` |
| `-hidden` | старт скрыто в трее |
| `-autostart=false` | не поднимать сервер сразу при старте |

---

## 4. Веб-админка

Открывается в браузере с любого устройства:

```
https://193.7.203.222:8090/
```

Логина нет — только пароль. Текущий пароль задан в `admin.json` (по умолчанию
`poscloud`, менять через «Обслуживание → Пароль веб-админки»).

Админка работает и в режиме службы, и в режиме окна — это один и тот же
HTTP-сервер.

---

## 5. Пересборка после изменения исходников

Сборка требует GTK/WebKit и cgo. Теги обязательны: без `production` бинарник
соберётся, но при запуске напишет `Wails applications will not build without
the correct build tags` и завершится.

### Штатный путь (если стоят dev-пакеты)

```bash
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev

cd /home/rez/Work/POSCloud/POSCloud-Server/poscloud-server
go build -tags "production gtk3 webkit2_40" -o /home/rez/bin/poscloud-server

sudo systemctl restart poscloud-server
```

### Путь без root (локальный префикс)

На этой машине dev-пакеты не установлены системно, сборка идёт из
распакованного префикса в `~/sdk`:

```bash
cd /home/rez/Work/POSCloud/POSCloud-Server/poscloud-server
R=$HOME/sdk/root
export PATH="$R/usr/bin:$HOME/sdk/go/bin:$PATH"
export LD_LIBRARY_PATH="$R/usr/lib/x86_64-linux-gnu:$HOME/sdk/liblink:/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu"
export PKG_CONFIG_PATH="$R/usr/lib/x86_64-linux-gnu/pkgconfig:$R/usr/lib/pkgconfig:$R/usr/share/pkgconfig"
export PKG_CONFIG_SYSROOT_DIR="$R"
export CGO_CFLAGS="-I$R/usr/include -I$R/usr/include/x86_64-linux-gnu"
export CGO_LDFLAGS="-L$HOME/sdk/liblink -B$R/usr/lib/x86_64-linux-gnu -L$R/usr/lib/x86_64-linux-gnu"
export CGO_ENABLED=1

go build -tags "production gtk3 webkit2_40" -o /tmp/poscloud-server .
```

Затем скопировать и перезапустить службу:

```bash
cp /tmp/poscloud-server /home/rez/bin/poscloud-server
chmod +x /home/rez/bin/poscloud-server
sudo systemctl restart poscloud-server
```

### Проверка сборки без GUI

```bash
cd /home/rez/Work/POSCloud/POSCloud-Server/poscloud-server
go vet ./...                                  # статический анализ
go test ./...                                 # тесты
GOOS=windows GOARCH=amd64 go build ./...      # Windows не сломан
```

---

## 6. Типичные проблемы

### «Программа не запускается» — ничего не происходит

Скорее всего работает служба и держит замок. Проверьте:

```bash
sudo systemctl is-active poscloud-server
```

Если `active` — остановите службу, потом запускайте окно.

### Порт занят

```bash
sudo ss -tlnp | grep 8090        # кто слушает
sudo systemctl status poscloud-server
```

### Клиенты не подключаются

1. Проверить, что сервер отвечает: `curl -sk https://127.0.0.1:8090/health`
2. Сверить отпечаток сертификата в клиенте с серверным:

```bash
openssl x509 -in /home/rez/poscloud-data/tls/server.crt -noout -fingerprint -sha256
```

3. Посмотреть журнал: `sudo journalctl -u poscloud-server -n 50`

### Веб-админка не открывается

```bash
curl -sk -o /dev/null -w "%{http_code}\n" https://127.0.0.1:8090/
# 301 или 200 — работает; 000 — сервер не слушает
```

### Забыт пароль админки

```bash
sudo systemctl stop poscloud-server
rm /home/rez/bin/admin.json
sudo systemctl start poscloud-server
# пароль вернётся к 'poscloud'
```

Файл создастся заново при первом входе. Все данные синхронизации это не
затрагивает — `admin.json` относится только к веб-админке.

---

## 7. Что где меняется

| Что нужно | Где менять | Нужен перезапуск |
|---|---|---|
| Порт, каталог данных, TLS | `server.json` или через веб-админку | да |
| Пользователи, токены | веб-админка или окно | нет |
| Пароль админки | «Обслуживание» в веб-админке | нет |
| Права службы, лимиты | `/etc/systemd/system/poscloud-server.service` | да (`daemon-reload` + `restart`) |

После правки юнита:

```bash
sudo systemctl daemon-reload
sudo systemctl restart poscloud-server
```
