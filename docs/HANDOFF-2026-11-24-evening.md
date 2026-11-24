# Хендоф: POSCloud, состояние на 24.11.2026 (вечер, сессии 2–4)

Для агента без контекста. Репозиторий `E:\Work\VSC\POSCloud` (origin
https://github.com/reznikov73/POSCloud, ветка `main`). Документ продолжает
`docs/HANDOFF-2026-11-24.md` (утренние сессии): общая карта проекта — там,
здесь — что сделано после, **окружение этой машины** и что делать дальше.
Утренний хендоф описывает другую машину (пользователь `rez`, тулчейн на
`E:\toolchain`); на этой машине эти пути не работают — см. раздел 3.

## 1. Состояние

- Версия **1.0.41** (versionCode 10041), `main` = `origin/main` = `0c88f93`,
  теги `v1.0.39`, `v1.0.40`, `v1.0.41` на origin.
- APK: `poscloud-android/POSCloud-Client-1.0.41-android-arm64-debug.apk`
  (отладочная подпись fyne); копия для телефона — в папке синхронизации
  `E:\Cloud\POSCloud\POSCloud\` (там же 1.0.39 и 1.0.40).
- Десктоп-клиент и сервер **не пересобирались** с 28.09 (см. п. 5).

## 2. Что сделано 24.11 после обеда (Android, три релиза)

План развития — `docs/ANDROID-PLAN-2026-11-24.md`; статусы вписаны в сами
пункты плана. Всё — категория A (чистый Go), проверено тестами и рендером,
**на телефоне не проверялось** (на этой машине нет эмулятора).

**1.0.39 — разрешение конфликтов (шаг 2 плана).**
`engine/conflicts.go`: `ConflictOriginalName` (строгий разбор суффикса
« (конфликт YYYY-MM-DD_HH-MM-SS)»), `Syncer.Conflicts()`, `ResolveConflictKeepLocal`
/ `KeepServer`. Карточка «Конфликты» на «Файлах» (`internal/ui/conflicts.go`).
Ловушки: без `DeleteRemote` копия возвращается скачиванием; при keep-local
нужна немедленная выгрузка (иначе петля с надгробием); офлайн — серверная
версия прячется в `.<имя>.conflict-tmp` и всё откатывается.

**1.0.40 — лента «Недавние» + поиск (шаг 3 плана, частично).**
SSE `/events` шлёт только `{"version":N}` — имён там нет, поэтому лента
построена на событиях движка: `engine/journal.go` (`ChangeEvent`,
`SetChangeFunc`, пачка в конце `SyncOnce`; заметки `noteEvent` у счётчиков
up/down/del в `syncer.go`). Клиент хранит `events.json` (до 500 записей).
Шестая вкладка «Лента» (`internal/ui/recent.go`). Поиск по имени в «Файлах».

**1.0.41 — волна 1.5, стабилизация интерфейса по внешнему ревью.**
Ревью (диалог из 9 предложений + разбор по коду) и макеты —
`docs/android-ui-mockups-v2.html`, что взято/не взято — раздел «Волна 1.5»
в плане. Сделано: поиск вне UI-потока (debounce, горутина, кеш, поколение),
карточка «Передачи» всегда на месте, `setTextIfChanged`/`showIf` в тикере,
`pillSwitch.SetCheckedSilently`, `fieldRow` через `caption()`, полноэкранный
диалог папки с вертикальными быстрыми переходами, свёрнутая «Ручная настройка»
с адресом сервера в заголовке.

## 3. Окружение сборки (эта машина, пользователь Administrator)

В `exec` обязательно (иначе `go` падает: «build cache is not defined»):

```powershell
$env:LOCALAPPDATA="$env:USERPROFILE\AppData\Local"
$env:GOCACHE="$env:USERPROFILE\AppData\Local\go-build"
$env:GOPATH="$env:USERPROFILE\go"
```

| Что | Где |
|---|---|
| Go 1.27.1 | в PATH (`E:\Program Files\Go\bin`) |
| fyne, gomobile, gobind | `D:\Android\bin` (в `%USERPROFILE%\go\bin` их нет) |
| NDK r27 | `D:\Android\ndk\android-ndk-r27` |
| Android SDK | `C:\Users\Administrator\AppData\Local\Android\Sdk` (build-tools 34/35/35.0.1/37, platforms android-34/35) |
| JDK 17 | `C:\Program Files\Java\jdk-17.0.2` (есть ещё 24 — не проверялся) |
| Git bash | `C:\Program Files\Git\bin\bash.exe` |
| Эмулятор | **нет** (BlueStacks здесь не установлен) |

Сборка APK (проверено, ~4–5 минут; **прямым вызовом** с таймаутом 600 с —
фоновая сборка через `exec background` тихо умирала):

```powershell
$env:ANDROID_HOME='C:/Users/Administrator/AppData/Local/Android/Sdk'
$env:ANDROID_NDK_HOME='D:/Android/ndk/android-ndk-r27'
$env:JAVA_HOME='C:/Program Files/Java/jdk-17.0.2'
$env:PATH="D:\Android\bin;$env:JAVA_HOME\bin;$env:PATH"
cd E:\Work\VSC\POSCloud\poscloud-android
& 'C:\Program Files\Git\bin\bash.exe' -c 'sh ./build.sh 2>&1'
```

`build.sh` сам подставляет версию из `installer/version.txt` (поднимите
перед сборкой), гоняет тесты и проверяет 16-КБ выравнивание. APK —
`POSCloud-Client-<версия>-android-arm64-debug.apk`; копию положить в
`E:\Cloud\POSCloud\POSCloud\` — телефон найдёт её через автопроверку обновления.

Пуш (параметры credential обязательны, иначе висит/пусто):

```powershell
$env:GIT_TERMINAL_PROMPT='0'
git -c credential.username=reznikov73 -c credential.interactive=false push origin main
git tag -a v1.0.42 -m '…'; git -c credential.username=reznikov73 -c credential.interactive=false push origin v1.0.42
```

`git ls-remote` без `-c credential.*` возвращает пусто — не делать выводов
«тегов на origin нет» по нему.

## 4. Проверка

```powershell
cd engine;               go vet ./...; go test ./...          # движок
cd ..\poscloud-android;  go vet ./internal/...; go test ./internal/...
go test -run TestBuildUISmoke ./internal/ui
POSCLOUD_SHOTS=<папка> go test -run 'TestScreenshotRender|TestRender' .   # скриншоты вкладок (без POSCLOUD_SHOTS — skip)
```

- `go build ./...` в `poscloud-android` **падает** для корневого пакета
  (`main`) на Windows-хосте: нет cgo/GL. Это норма — APK собирается
  кросс-компиляцией через NDK. Внутренние пакеты `go build ./internal/...` — ок.
- `go test -race` недоступен (нет cgo). Гонки закрывают мьютексы:
  кеш поиска — `filesPage.searchMu`, события ленты — `Controller.mu`.

## 5. Открытые вопросы и следующие шаги

1. **Проверка на телефоне 1.0.39–1.0.41** — все три релиза не видели
   устройства: конфликты, лента, поиск, свёрнутая настройка, полноэкранный
   диалог папки. Приоритет — диалог папки (fullscreen через
   `win.Canvas().Size()`) и переключатель/тишину тикера.
2. **Дерево «Файлов» в обычном режиме** (без поиска) читает `ReadDir`/`Stat`
   в колбэках `widget.Tree` на UI-потоке — остаток пункта 9c ревью. Нужно
   асинхронное дерево с кешем; на больших папках (FUSE, тысячи снимков)
   возможен фриз при раскрытии.
3. **Экран «Хранилище»** — хвост пункта 6 плана: занято локально, всего на
   сервере, квота (на сервере есть `quota.go`). Не начат.
4. **Шаг 4 плана**: «держать на телефоне» и автоочистка (в движке уже есть
   `ClearLocalCopy` — папочный, нужен файловый уровень). **Шаг 5**: PIN.
   Дальше — волна 2 (JNI): Wi-Fi only, уведомления, foreground service.
5. **Десктоп не пересобран** с добавочным API движка (`conflicts.go`,
   `journal.go`): поведению десктопа не влияет (он новые функции не зовёт,
   `engine_compat.go` обёрток не добавлял), но при следующей сборке клиента
   и Lite они войдут. Правило проекта «движок → клиент + Lite + установщик»
   для Android-релизов 1.0.36–1.0.41 не выполнялось осознанно.
6. **Не взятое из ревью** (осознанно): плоские списки вместо карточек —
   нужен единый `listRow` для всех вкладок; «Полная сверка» в overflow-меню;
   i18n; одна строка статуса вместо плиток.
7. **16-КБ страницы**: линкер и APK выровнены, на реальном 16-КБ устройстве
   не запускалось (как и утром).
8. **Локальные файлы агента** (`AGENTS.md`, `SOUL.md`, `USER.md`, `memory/`)
   не в репозитории: `AGENTS.md`, `.zwork/`, `.agents/`, `.autoclaw/` были в
   `.gitignore`, `memory/` добавлен сегодня. Не пушить.

## 6. Ключевые файлы (новое за день)

| Файл | Зачем |
|---|---|
| `engine/conflicts.go`, `conflicts_test.go` | список конфликтных пар, keep-local / keep-server |
| `engine/journal.go`, `journal_test.go` | `ChangeEvent`, `SetChangeFunc`; заметки в `syncer.go` (`noteEvent`) |
| `poscloud-android/internal/client/client.go` | `Conflicts`, `ResolveConflict`, события ленты (`events.json`, `RecentEvents`, `EventsDirtyAndReset`) |
| `poscloud-android/internal/ui/conflicts.go` | карточка «Конфликты» на «Файлах» |
| `poscloud-android/internal/ui/recent.go` | вкладка «Лента» |
| `poscloud-android/internal/ui/files.go` | дерево, поиск (debounce/горутина/кеш), карточка конфликтов |
| `poscloud-android/internal/ui/ui.go` | вкладки, тикер (`setTextIfChanged`, `showIf`), свёрнутая ручная настройка (`updateManualTitle`) |
| `poscloud-android/internal/ui/folderdialog.go` | полноэкранный диалог папки, вертикальные быстрые переходы |
| `poscloud-android/internal/ui/toggle.go` | `SetCheckedSilently` |
| `poscloud-android/internal/ui/stability_test.go`, `recent_test.go`, `conflicts_test.go` | тесты волны 1.5, ленты/поиска, конфликтов |
| `docs/ANDROID-PLAN-2026-11-24.md` | план + статусы (шаги 1–3, «Волна 1.5») |
| `docs/android-ui-mockups-v2.html` | макеты из ревью (референс) |

## 7. Грабли дня (не повторять)

- **`Label.SetText` в fyne 2.8 не сравнивает текст** — всегда `Refresh()`.
  В тикере только через `setTextIfChanged` / `setButtonTextIfChanged`
  (для кнопок отдельный хелпер) и `showIf`.
- **Замыкание в Go не видит переменную, объявленную позже** в том же блоке —
  `var d *dialog.CustomDialog` до `rows`, которые на него ссылаются.
- **Текст поля и путь папки — только на UI-потоке**: в горутину передавать
  значениями (`scheduleSearch` снимает `q, root` и передаёт в `runSearch`).
- **`apply_patch`/`edit` чувствительны к отступам**: перед правкой сверять
  текст с диском (`read` с `offset`), иначе «match not found» или, что хуже,
  аккуратный на вид, но неверный патч. Опечатка в пути (`POSCloud`) даёт
  «parent folder unavailable».
- **PowerShell**: `.NET`-методы (`[IO.File]::…`) не следуют за `cd` — только
  абсолютные пути; перевод CRLF→LF перед `edit` для файлов, которые правите.
- **`Select-String` в цепочке пуша** может проглотить результат: после пуша
  проверять `git fetch; git log origin/main -1`.
