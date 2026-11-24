# Хендоф: POSCloud Client для Android

Дата: 2026‑11‑23 (вечер, вторая сессия). Репозиторий `E:\Work\VSC\POSCloud` (origin https://github.com/reznikov73/POSCloud, ветка `main`, HEAD — смотреть `git log`, всё запушено).

> **Актуально на 24.11.2026: версия 1.0.30.** Что изменилось в третьей сессии
> (переключатель, возврат удалённых снимков, эмулятор) — в конце файла,
> раздел «Сессия 3».
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

## Сессия 3 (23.11.2026, ночь): переключатель, возврат снимков, эмулятор

Версия **1.0.30** (versionCode 10030). APK:
`poscloud-android/POSCloud-Client-1.0.30-android-arm64-debug.apk` (подпись — тот же
debug‑ключ fyne, ставится поверх 1.0.29, настройки сохраняются). Копия для телефона:
`E:\Cloud\POSCloud\POSCloud\POSCloud-Client-1.0.30-android.apk`.

### 1. Переключатель «Синхронизация»

Причина: `BorderLayout` в карточке отдаёт правому полю всю высоту карточки
(~110 dp), а отрисовщик растягивал таблетку на это место — ползунок превращался
в белый круг диаметром во всю карточку.

- `internal/ui/toggle.go` — таблетка постоянного размера 56×32 dp, ползунок 24 dp;
  рисуется по центру отведённого места (при нехватке — пропорционально уменьшается,
  но не растягивается), лёгкая тень и обводка ползунка, зелёная дорожка включённого
  состояния, серо‑голубая выключенного;
- `internal/ui/ui.go` — переключатель завёрнут в `GridWrapLayout` своего размера,
  раскладка карточки его больше не тянет;
- тесты: `TestPillSwitch` (размер, касание, отсутствие лишнего `OnChanged`),
  `TestRenderToggle` (рендер обоих положений в `POSCLOUD_SHOTS/toggle.png`).

### 2. Удалённые на ПК снимки больше не возвращаются

Симптом: удаляешь снимки на компьютере, чтобы освободить место, — они приезжают
с телефона снова, каждый раз с новым именем «(конфликт …)».

Механика (видна по `E:\Program Files\POSCloud\client.log` 22–23.11):

1. телефон раз в 30 с копирует снимки из MediaStore в «Снимки экрана» папки синхронизации;
2. на ПК файл удаляют → сервер ставит надгробие (tombstone);
3. телефон удаляет копию, но импорт снова копирует снимок (после переустановки
   приложения `screenshots.json` пуст), и движок видит локальный файл без записи
   состояния рядом с надгробием;
4. движок считал это конфликтом «удаление против правки» → переименовывал файл
   в «(конфликт …)» и выгружал → файл приезжал на ПК. На каждый цикл — новая копия.

Исправлено:

- `internal/client`: в `screenshots.json` появился список `gone` — снимки, которые уже
  переносили и которые из папки исчезли (вручную или синхронизацией), больше не
  копируются (`forgetMissing` перед каждым импортом). Список ограничен 4000 имён
  (старые отбрасываются), `copied` больше не подрезается;
- `engine/syncer.go`: если сервер прислал надгробие, а локальный файл по содержимому
  совпадает с удалённой версией (`cs.Hash`), файл просто удаляется — без конфликтной
  копии и без выгрузки. Конфликт остаётся только для реально изменённого файла
  (`engine/syncer_tombstone_test.go`, два теста).

Итог: удаление на ПК доезжает до телефона (копия в папке синхронизации удаляется)
и не возвращается. **Оригиналы в галерее телефона не удаляются** — Android 11+ не даёт
приложению удалять чужие файлы без диалога подтверждения; это отдельная задача
(`MediaStore.createDeleteRequest`).

Копии «(конфликт …)», накопившиеся до 1.0.30, нужно один раз удалить вручную —
новые не появляются.

### 3. BlueStacks: «связь есть», а файлов нет

Диагноз — в `docs/HANDOFF-BLUESTACKS-POLAR.md` (раздел «Сессия 3»): папка
`/storage/emulated/0/DCIM/POScloud` в BlueStacks (Android 16) была доступна
приложению только на чтение, движок не мог сохранить даже `.poscloud-state.json`,
каждый цикл начинался с нуля, а скачивание падало без записи в журнал.

Исправлено:

- движок проверяет запись в папку (пробный файл, не чаще раза в минуту) и пишет
  причину в журнал; `Status.DirErr` выводится на вкладке «Статус» («папка
  недоступна для записи»), а не молчит;
- ошибки передачи (вверх и вниз) попадают в журнал по одному разу на файл
  («не удалось получить <файл>: …») — раньше неудача пропадала молча;
- `Controller.Start`: если выбранная папка недоступна для записи, синхронизация
  переходит в папку приложения (`/data/user/0/<пакет>/files/fyne/sync`) и пишет об
  этом в журнал (`TestWritableSyncDir`).

Проверено в BlueStacks на 1.0.30: папка сама стала
`/data/user/0/io.github.reznikov73.poscloud.client/files/fyne/sync`, «Файлов 36»,
тестовый файл с сервера приехал (25 Б в «Получено»), после удаления на сервере — исчез.

Внешняя папка на Android 11+ заработает только со своим манифестом
(`requestLegacyExternalStorage` при targetSdk 29). `fyne package` без
adaptive‑иконки собирает манифест своим binres, который этого атрибута не знает
(«failed to find table ref by attr/requestLegacyExternalStorage»). Варианты на
будущее: adaptive‑иконка (`Icon-foreground.png` + `Icon-background.png` — тогда
сборка идёт через aapt2 и атрибут проходит) либо `MANAGE_EXTERNAL_STORAGE` с
переходом в настройки.

### Что проверить на телефоне (1.0.30)

1. Поставить APK поверх 1.0.29 (подпись та же — настройки сохранятся).
2. «Статус»: переключатель — ровная таблетка, срабатывает одним касанием.
3. Сделать снимок экрана → он приезжает в `E:\Cloud\POSCloud\Снимки экрана`;
   удалить его на ПК → с телефона не возвращается (копия в
   `/storage/emulated/0/Download/POScloud/Снимки экрана` удаляется).
4. «Журнал»: строки «снимок экрана удалён из папки — повторно не переносим».

### Состояние сборок

- `poscloud-client` / `poscloud-server` пересобраны (движок общий):
  `installer/out/POSCloud-Setup-1.0.30.exe`, версия в `installer/version.txt` — 1.0.30;
- Lite: `lite/out/POSCloud-Lite-x64.exe`, `POSCloud-Lite-x86.exe` (go1.20.14);
- Android: `POSCloud-Client-1.0.30-android-arm64-debug.apk`; в BlueStacks установлен.
- Старые APK 1.0.29 удалены из репозитория и из папки синхронизации `POSCloud/`.

### Сессия 4 (24.11.2026, ночь): почему на телефон ничего не скачивалось

Журнал с телефона (вкладка «Журнал», 1.0.30) наконец показал причину — до этого
ошибки не писались:

    не удалось получить POSCloud/poscloud-server-linux-amd64: rename
    /storage/emulated/0/Download/POScloud/POSCloud/.poscloud-dl-…
    /storage/emulated/0/Download/POScloud/POSCloud/poscloud-server-linux-amd64:
    operation not permitted

В общей памяти Android приложение с targetSdk 29 **может создавать и удалять
файлы, но не переименовывать** (FUSE отвечает «operation not permitted»).
Скачивание падало на последнем шаге: временный файл уже скачан, а встать на
место не может. Отсюда «вверх работает, вниз 0» на телефоне — всегда, просто
молча. Выгрузка шла, потому что съёмка и снимки пишутся через `os.Create`.

Исправление: `engine/movefile.go` — `moveFile` сначала переименовывает, а если
запрещено — копирует и удаляет исходник (как камера в `internal/ui`). Применён
во всех местах, где файл встаёт на место: `http.go` (обычное скачивание),
`chunks.go` (блочное), `state.go` (сохранение состояния), `syncer.go`
(конфликтные копии). Тесты: `TestMoveFile`, `TestMoveFileFallback`.

Версия **1.0.31**: APK — `poscloud-android/POSCloud-Client-1.0.31-android-arm64-debug.apk`,
для телефона — `E:\Cloud\POSCloud\POSCloud\POSCloud-Client-1.0.31-android.apk`
(ставится поверх 1.0.30, настройки сохраняются). Пересобраны клиент, сервер,
Lite x64/x86, установщик `POSCloud-Setup-1.0.31.exe`.

Проверить на телефоне после установки: в «Журнале» при синхронизации должны
пойти строки «вниз: …», а «Файлов» должно вырасти; ошибок «rename … operation
not permitted» больше быть не должно.

### Сессия 5 (24.11.2026, утро): журнал в папку синхронизации, тише записи, видимая папка

Вопросы пользователя: куда пишется журнал, можно ли его класть в папку
синхронизации (чтобы смотреть с компьютера) и как видеть файлы «как в проводнике».

Где журнал:

- полный — в данных приложения: `/data/user/0/io.github.reznikov73.poscloud.client/files/client.log`
  (без root не прочитать; на ПК у настольного клиента — `E:\Program Files\POSCloud\client.log`);
- копия — в **корне** папки синхронизации: `POSCloud-журнал-<устройство>-<номер установки>.log`,
  обновляется раз в 30 с (если журнал менялся). В корне — потому что файлы в корне
  синхронизируются всегда, а на папки действуют правила (у пользователя почти все
  выключены, и зеркало в подпапке не уезжало бы на ПК);
- номер установки (`installId` в `client.json`, 8 hex-символов) нужен, чтобы журналы
  телефона и эмулятора — оба с именем «Android» — были разными файлами;
- движок (`MirrorLogPrefix = "POSCloud-журнал-"`) не пишет в журнал выгрузку самого
  зеркала — иначе строка «вверх: …log» меняла бы журнал, и зеркало обновлялось бы
  бесконечно (проверено: в зеркале нуль таких строк);
- оба пути видны на вкладке «Журнал» (подпись под кнопкой «Очистить журнал»).

Тише журнал:

- итог синхронизации — одна короткая строка: `синк: вверх 0, вниз 0, удалено 0,
  пропущено 71, локальных 11` (было: «синк: сервер …, локальных …, изменений …,
  вверх …, вниз …, удалено …, пропущено по режимам …» на 2–3 строки);
- убрана отдельная запись «синхронизация вручную» (кнопка и так действие
  пользователя, а итог пишется сразу же).

Видимая папка (проводник):

- в диалоге выбора папки быстрые переходы: «Приложение», «На карте», «Память»,
  «Загрузки», «Снимки», «POSCloud». «POSCloud» ведёт в
  `/storage/emulated/0/Download/POScloud` (или `DCIM/POScloud`), где файлы видны в
  любом файловом менеджере телефона; быстрая кнопка «Фото» убрана — камера
  доступна через «Память»;
- внешняя папка теперь полностью рабочая и для скачивания: после `moveFile`
  (сессия 4) запись через временный файл не нужна — переименование при запрете
  заменяется копированием.

Источник истории на телефоне: пользователь переустановил приложение начисто — папка
осталась по умолчанию (`…/files/fyne/sync`, не видна в проводнике), токен подхватился
повторным подключением, всё скачанное (60,5 МБ из POSCloud) лежит там. Чтобы вернуть
видимую папку — «Связь» → «Выбрать папку…» → «POSCloud» → «Выбрать эту папку».

Версия **1.0.32**: APK `poscloud-android/POSCloud-Client-1.0.32-android-arm64-debug.apk`,
для телефона — `E:\Cloud\POSCloud\POSCloud\POSCloud-Client-1.0.32-android.apk`.
Пересобраны клиент, сервер, Lite x64/x86, установщик `POSCloud-Setup-1.0.32.exe`.
Проверено в BlueStacks: зеркало появилось у настольного клиента через минуту
(`вниз: POSCloud-журнал-Android-5423354b.log`).
