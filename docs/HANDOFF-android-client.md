# Хендоф: POSCloud Client для Android

Дата: 2026‑11‑23 (вечер, вторая сессия). Репозиторий `E:\Work\VSC\POSCloud` (origin https://github.com/reznikov73/POSCloud, ветка `main`, HEAD — смотреть `git log`, всё запушено).
Модуль Android‑клиента: `poscloud-android/` (чистый Go + Fyne 2.8.1, gomobile, без Java‑классов; общий движок `engine/` через `replace`).

## Что сделано

| Коммит | Суть | Статус |
|---|---|---|
| `0d9313e` | Разрешение на чтение фото через JNI → системные скриншоты попадают в `Download/POScloud/Снимки экрана` | **Проверено пользователем на телефоне — работает** |
| `ce20ebc` | Главный экран по макету B: переключатель «Синхронизация», кнопка «Синхронизировать сейчас», плитки 2×2; тема под палец | APK отдан, **отзыв пользователя о новом экране всё ещё не получен** |
| `a0ebe4f` | «Доводка до ума»: 16‑КБ выравнивание .so, закрытие курсоров MediaStore, атомарные настройки, versionCode из версии, склонения, поле отпечатка, переносимый релизный путь | **Проверено в BlueStacks Desktop (Android 16): ставится поверх, работает, logcat чистый** |

Детали `a0ebe4f`:

- **16‑КБ страницы** (Android 15+ на части устройств, эмулятор `gphone16k`): `build.sh` экспортирует `CGO_LDFLAGS="-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384"` перед `fyne package` (у `fyne package` нет passthrough для ldflags, только окружение). Проверено `llvm-readelf`: все LOAD `0x4000`, RELRO кратен 16 КБ. `build.sh` сам проверяет выравнивание и падает при регрессии. **На реальном 16‑КБ устройстве/эмуляторе не запускалось** (BlueStacks — страницы 4 КБ).
- **Курсоры MediaStore** закрываются (`Cursor.close()`); раньше в logcat каждые 30 с было `A resource failed to call Cursor.close`. После исправления — тишина (75 с наблюдения).
- Запрос скриншотов: `date_added DESC LIMIT 200` (иначе после 200 снимков новые не попадали в выборку); `relative_path` только с API 29, на Android 5–9 путь берётся из `_data` (на старых версиях не проверялось); `EnsureLocalCapacity(48)`.
- `client.json` и `screenshots.json` пишутся атомарно (tmp + rename + fsync) под `fileMu`, ошибки — в журнал; побитый `client.json` сохраняется как `client.json.bad` и не затирается; `ImportScreenshots` — один одновременно (`shotMu.TryLock`).
- `versionCode` выводится из версии: `1.0.29 → 10029` (был всегда 1).
- UI: «1 файл / 2 файла / 5 файлов»; отпечаток сертификата — многострочное поле с переносом (раньше обрезался справа); `mkdir DCIM/POScloud` перед съёмкой больше не прерывает камеру (на Android 10+ каталог создаёт MediaStore).
- `build.sh`: тесты `./internal/...` + UI‑смок `./internal/ui` (раньше `.` — «no test files»); релизный путь перенесён на Git Bash: `python3 || python`, временные файлы через `mktemp -d` с путями‑аргументами, `java -jar lib/apksigner.jar` (в build‑tools 34/36 под Windows нет sh‑обёртки apksigner), build‑tools ≥ 35 → `zipalign -P 16 -f 4`. **Релизный путь проверен end‑to‑end с одноразовым тестовым keystore** (v1+v2+v3, `zipalign -c -P 16` OK), тестовые файлы удалены.
- `.gitattributes`: `*.sh text eol=lf` — иначе повторный checkout с `autocrlf=true` делал бы `build.sh` CRLF и ломал sh.

APK: `poscloud-android/POSCloud-Client-1.0.29-android-arm64-debug.apk` (22:41, 26.7 МБ). Подпись — встроенный debug‑ключ fyne (CN=gomobile, SHA‑256 `7406e14b…`), та же, что стоит на телефоне и в BlueStacks → ставится поверх (проверено `apksigner verify --print-certs`). Копия для телефона: `E:\Cloud\POSCloud\POSCloud\POSCloud-Client-1.0.29-android.apk` → на телефоне `/Download/POScloud/POSCloud/`. Старый APK от 21:39 остался в `poscloud-android/POSCloud-Client-1.0.29-android.apk` (не отслеживается, можно удалить).

Макеты A–D лежат в `docs/android-ui-mockups.html` / `.png` (не закоммичены — решение за пользователем), реализованный вариант B — `docs/android-status-b.png`.

## Как собирать и проверять

Окружение (всё на E:, на C: не ставить — там мало места):
- NDK r27c `E:\toolchain\android-ndk-r27c`, JDK 21 `E:\toolchain\jdk\jdk-21.0.13+9`, SDK `C:\Users\rez\AppData\Local\Android\Sdk` (build-tools 34.0.0 / 35.0.0 / 36.1.0). Переменные `ANDROID_HOME`, `ANDROID_NDK_HOME`, `JAVA_HOME` заданы в пользовательском профиле.
- В exec для любых go‑команд явно задавать `GOCACHE=%USERPROFILE%\AppData\Local\go-build`, `GOPATH=%USERPROFILE%\go`, `LOCALAPPDATA` (в сессии `%LocalAppData%` не определён).

```powershell
$env:ANDROID_HOME='C:\Users\rez\AppData\Local\Android\Sdk'; $env:ANDROID_NDK_HOME='E:\toolchain\android-ndk-r27c'
$env:JAVA_HOME='E:\toolchain\jdk\jdk-21.0.13+9'; $env:PATH="$env:JAVA_HOME\bin;C:\Users\rez\go\bin;$env:PATH"
$env:LOCALAPPDATA='C:\Users\rez\AppData\Local'; $env:GOCACHE="$env:USERPROFILE\AppData\Local\go-build"; $env:GOPATH="$env:USERPROFILE\go"
cd poscloud-android
go vet ./internal/... ; go test ./internal/...          # только internal — корневой пакет на Windows не собирается (cgo)
$env:POSCLOUD_SHOTS='E:\toolchain\shots'; go test -run TestScreenshotRender ./internal/ui/   # рендер вкладок в PNG
& 'C:\Program Files\Git\bin\bash.exe' -c './build.sh'          # тесты + APK (~2 мин) + проверка 16-КБ выравнивания
# релиз: $env:KEYSTORE=…; $env:KS_PASS=…; $env:KEY_ALIAS=…; $env:KEY_PASS=…; нужен python
```
Результат debug‑пути: `POSCloud-Client-<версия>-android-arm64-debug.apk` (отладочная подпись fyne, v1‑only, APK не выровнен — `Can't mmap dex … Falling back to extracting` в logcat, безвредно). Релиз: `POSCloud-Client-<версия>-android-arm64.apk` (v1+v2+v3, zipalign -P 16).

Проверка APK без телефона — BlueStacks Desktop (Android 16, SDK 36, экран 900×1600, страницы 4 КБ, arm64 через native bridge): подключение по ADB — пошагово в `docs/HANDOFF-BLUESTACKS-POLAR.md` (пути, порт, сервер 5038, грабли screencap); установка APK — в `docs/ИНСТРУКЦИЯ-BLUESTACKS-DESKTOP.md`; навык `bluestacks-desktop` (установлен в `C:\Users\rez\.openclaw-autoclaw\skills\bluestacks-desktop\`). Кратко: ADB только `C:\Program Files\BlueStacks_polar\HD-Adb.exe`, `ANDROID_ADB_SERVER_PORT=5038`, сервер стартовать фоновой командой, устройство `127.0.0.1:6610`. Вкладки внизу: x ≈ 117 / 339 / 560 / 782, y ≈ 1550. `screencap` иногда ловит чужое окно рабочего стола BlueStacks при анимациях — переснять через 2–3 с после `monkey`. `uiautomator` бесполезен (Fyne рисует сам). Логи ПК‑клиента: `E:\Program Files\POSCloud\client.log`.

## Грабли (уже наступлено)

- **Подпись debug‑APK.** Ключ fyne — НЕ уникален для машины: это константа `debugCert` в `fyne.io/tools@v1.7.4/cmd/fyne/internal/mobile/build_androidapp.go` (CN=gomobile), одинаковый для всех сборок fyne 1.7.4. «Пакет недействителен» при установке поверх — значит, стоявшая версия была подписана иначе (другая версия fyne/другой ключ); тогда только удаление (настройки/токен теряются). Сверять перед доставкой: `apksigner verify --print-certs` старого и нового APK.
- **CRLF.** В рабочем дереве CRLF (`core.autocrlf=true`), инструменты `edit`/`apply_patch` требуют `\r\n` в old/new text (кроме `build.sh` и `.gitattributes` — там LF). **Не запускать `gofmt -w .`**. Проверка форматирования: прочитать файл, заменить CRLF→LF, подать в `gofmt -d` через пайп.
- **PowerShell** не понимает heredoc: текст коммита — во временный файл (UTF‑8 без BOM) и `git commit -F`.
- **Fyne 2.8**: нет `widget.Switch`/`Toggle` (свой `pillSwitch` в `internal/ui/toggle.go`); у `widget.Label` нет `TextSize` — использовать `SizeName`.
- **Flaky ui‑тестов**: `Build()` не должен порождать фоновые горутины с `fyne.Do`; `refreshFolders` в `Build` вызывается только при `ctrl.HasCredentials()`.
- **FindClass из горутин** (`org/golang/app/GoNativeActivity`) — работает, потому что класс уже загружен (ART сначала ищет в таблице классов); ревизия «не найдёт класс» — ложная тревога, подтверждено на телефоне и в BlueStacks.
- TargetSdk 29, манифест генерирует `fyne package` (READ/WRITE_EXTERNAL_STORAGE + INTERNET, debuggable=true, без `extractNativeLibs` → библиотеки распаковываются, поэтому zip‑выравнивание .so для debug‑пути не нужно).

## Открытые вопросы / следующие шаги

1. **Отзыв по макету B** — всё ещё нужен: переключатель, плитки, высота кнопок на телефоне. Остальные вкладки («Связь», «Папки», «Журнал») только унаследовали тему.
2. **Релизный ключ**: релизный путь `build.sh` готов и проверен; нужен keystore на E: вне репозитория (`keytool -genkeypair -keystore … -alias poscloud -keyalg RSA -keysize 2048 -validity 10000`) и решение пользователя о пароле/хранении. Смена подписи потребует удалить debug‑версию с телефона.
3. **targetSdk 29 → 34** (рекомендация аудита манифеста; 35/36 пока рано — edge‑to‑edge и predictive back ломают Fyne): свой `AndroidManifest.xml` (READ_MEDIA_IMAGES, READ_EXTERNAL_STORAGE `maxSdkVersion=32`, без WRITE, `debuggable=false`, `configChanges` + `screenSize|smallestScreenSize|screenLayout`), в JNI на API ≥ 33 просить `READ_MEDIA_IMAGES` и обрабатывать частичный доступ (`READ_MEDIA_VISUAL_USER_SELECTED` → настройки); при targetSdk ≥ 30 debug‑подпись v1‑only уже не ставится — debug‑путь придётся подписывать через apksigner (ключ fyne — PEM в исходниках tools). Отдельная задача.
4. **16‑КБ**: проверить APK на эмуляторе `gphone16k` (AVD из образа 16 КБ) — линкер исправлен, но на 16‑КБ устройстве не запускалось.
5. Android 5–9 (`_data`‑ветка запроса) — не на чем проверить.
6. Фоновая работа без foreground service — по‑прежнему ограничение первой версии (синхронизация живёт, пока приложение открыто или в «недавних»).
7. Счётчики «Отправлено/Получено» — за сеанс; при желании сохранять в настройках.
8. Мусор: `.finance-tool-capability.json`, `.jw-proxy-…`, `.legal-proxy-…`, `.tyc-proxy-…` в корне — не наши, не трогать. `docs/android-ui-mockups.*` — закоммитить или удалить по решению пользователя.

## Ключевые файлы

- `poscloud-android/build.sh` — сборка: тесты, 16‑КБ флаг, versionCode, проверка выравнивания, debug/релиз подпись.
- `poscloud-android/internal/camera/camera_android.go` — JNI: камера, MediaStore, `list_screenshots` (курсор закрывается, сортировка по дате, ветка `_data` для API < 29), `PhotoPermissionGranted`, `RequestPhotoPermission`, `OpenAppSettings`; `camera_other.go` — заглушки.
- `poscloud-android/internal/client/client.go` — контроллер: `persist`/`saveScreenshotState` (атомарно, `writeFileAtomic`), `ImportScreenshots` (`shotMu.TryLock`, раз в 30 с из `ui.Refresh`), `RequestPhotoAccess` (`perm_asks` в `screenshots.json`), `TransferStore.Totals()`.
- `poscloud-android/internal/ui/ui.go` — вкладки, `Refresh`, `setSwitch`/`startSync`, `fingerprint()`, `pluralFiles`; `toggle.go` — переключатель; `theme.go` — размеры и цвета.
- `poscloud-android/main.go` — автозапуск, авто‑запрос разрешения при первом запуске.
- `docs/ИНСТРУКЦИЯ-BLUESTACKS-DESKTOP.md` — пошаговая проверка APK в BlueStacks Desktop через ADB.
