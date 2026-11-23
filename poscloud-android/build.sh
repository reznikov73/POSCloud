#!/bin/sh
# Сборка APK POSCloud Client для Android (arm64).
#
# Нужны: Go 1.25+, JDK 11+, Android SDK (platform-tools, platforms;android-34,
# build-tools;34.0.0; для релизной подписи с zipalign -P 16 — build-tools 35+),
# Android NDK ($ANDROID_NDK_HOME) и утилита fyne:
#   go install fyne.io/tools/cmd/fyne@latest
#
# Релизная подпись (необязательно): переменные окружения
#   KEYSTORE=<путь к .keystore> KS_PASS=<пароль> KEY_ALIAS=<алиас> KEY_PASS=<пароль>
# Без них APK остаётся с отладочной подписью fyne (для установки достаточно).
#
# Что сборка делает сверх fyne package:
#   * .so линкуется с 16-КБ выравниванием сегментов (иначе на Android 15+ с
#     16-КБ страницами памяти приложение падает при запуске: dlopen «program
#     alignment (4096) cannot be smaller than system page size (16384)»);
#   * versionCode выводится из версии (1.0.29 -> 10029): обновление всегда
#     старше установленной версии и ставится поверх неё;
#   * проверяет выравнивание LOAD-сегментов .so (llvm-readelf из NDK).
set -e
cd "$(dirname "$0")"

APPID=io.github.reznikov73.poscloud.client
NAME="POSCloud Client"
VERSION=${APP_VERSION:-$(cat ../installer/version.txt 2>/dev/null || echo 1.0.0)}
VERSION=$(printf '%s' "$VERSION" | tr -d '\r\n ')
BUILD=${APP_BUILD:-$(printf '%s' "$VERSION" | awk -F. '{ printf "%d", $1 * 10000 + $2 * 100 + $3 }')}
LIBNAME=libPOSCloud_Client.so # fyne именует библиотеку по имени приложения

command -v fyne >/dev/null 2>&1 || {
        echo "fyne не найден: go install fyne.io/tools/cmd/fyne@latest" >&2
        exit 1
}
[ -n "$ANDROID_HOME" ] || { echo "задайте ANDROID_HOME (каталог Android SDK)" >&2; exit 1; }
[ -n "$ANDROID_NDK_HOME" ] || { echo "задайте ANDROID_NDK_HOME (каталог NDK)" >&2; exit 1; }

# иконка: в репозитории только текстовые файлы, PNG восстанавливается генератором
[ -f Icon.png ] || go run ./tools/genicon

# тесты логики (UI-скриншоты отключены — не задана переменная POSCLOUD_SHOTS)
if [ -z "$SKIP_TESTS" ]; then
        go test ./internal/...
        go test -run TestBuildUISmoke ./internal/ui
fi

# 16-КБ страницы памяти. У fyne package нет параметра для ldflags: флаг линкеру
# NDK доходит только через окружение (CGO_LDFLAGS -> cgo -> cmd/link -> clang).
# NDK r28+ выравнивает так по умолчанию, r27 и младше — только так.
export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384"

# fyne package --release собирает .so без отладочной информации (примерно на
# 20% меньше); подпись и выравнивание APK — ниже
rm -f POSCloud_Client.apk
fyne package --release --target android/arm64 \
        --appID "$APPID" \
        --name "$NAME" \
        --app-version "$VERSION" --app-build "$BUILD" \
        --icon Icon.png

# check_align проверяет, что все LOAD-сегменты .so выровнены по 16 КБ (0x4000).
# Без unzip или llvm-readelf из NDK проверка пропускается со сообщением.
check_align() {
        command -v unzip >/dev/null 2>&1 || { echo "выравнивание .so не проверено: нет unzip"; return 0; }
        readelf=""
        for r in "$ANDROID_NDK_HOME"/toolchains/llvm/prebuilt/*/bin/llvm-readelf*; do
                [ -x "$r" ] && readelf="$r" && break
        done
        [ -n "$readelf" ] || { echo "выравнивание .so не проверено: нет llvm-readelf в NDK"; return 0; }
        tmpso=$(mktemp)
        unzip -p "$1" "lib/arm64-v8a/$LIBNAME" > "$tmpso"
        if "$readelf" -lW "$tmpso" | awk '$1 == "LOAD" { if ($NF != "0x4000") bad = 1 } END { exit bad }'; then
                echo "выравнивание $LIBNAME: LOAD по 16 КБ (OK)"
        else
                rm -f "$tmpso"
                echo "ОШИБКА: LOAD-сегменты $LIBNAME выровнены не по 16 КБ — APK упадёт на 16-КБ устройствах" >&2
                exit 1
        fi
        rm -f "$tmpso"
}

if [ -z "$KEYSTORE" ]; then
        mv POSCloud_Client.apk "POSCloud-Client-$VERSION-android-arm64-debug.apk"
        check_align "POSCloud-Client-$VERSION-android-arm64-debug.apk"
        echo "Готово: POSCloud-Client-$VERSION-android-arm64-debug.apk (отладочная подпись fyne, versionCode $BUILD)."
        echo "Для релизной подписи задайте KEYSTORE, KS_PASS, KEY_ALIAS, KEY_PASS."
        exit 0
fi

# Релизная подпись: убрать отладочную подпись fyne, переписать APK (resources.arsc
# несжатым — требование targetSdk 30+), выровнять (.so — по 16 КБ, zipalign -P 16,
# нужны build-tools 35+) и подписать своим ключом. Временные файлы — в своём
# каталоге (mktemp -d), пути передаются аргументами: Git Bash превращает их в
# Windows-пути для нативных exe, а путь, зашитый в код Python, стал бы C:\tmp.
PY=$(command -v python3 2>/dev/null || command -v python 2>/dev/null)
[ -n "$PY" ] || { echo "python не найден: нужен для перепаковки APK" >&2; exit 1; }
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

"$PY" - POSCloud_Client.apk "$WORK/repack.apk" <<'PYEOF'
import sys, zipfile
src = zipfile.ZipFile(sys.argv[1])
out = zipfile.ZipFile(sys.argv[2], 'w')
for n in src.namelist():
    if n.startswith('META-INF/'):
        continue  # старые подписи выбрасываем
    data = src.read(n)
    if n == 'resources.arsc':
        zi = zipfile.ZipInfo(n)
        zi.compress_type = zipfile.ZIP_STORED
        out.writestr(zi, data)
    else:
        out.writestr(n, data)
out.close()
print("перепаковано: resources.arsc -> STORED, META-INF выброшен")
PYEOF

BT=""
for d in $(ls -d "$ANDROID_HOME"/build-tools/* 2>/dev/null | sort -rV); do
        case "$(basename "$d")" in
        [3-9][5-9].* | [4-9][0-9].*)
                if { [ -x "$d/zipalign" ] || [ -x "$d/zipalign.exe" ]; } && [ -f "$d/lib/apksigner.jar" ]; then
                        BT="$d"
                        break
                fi
                ;;
        esac
done
if [ -n "$BT" ]; then
        ALIGN="-P 16 -f 4" # .so внутри APK — на границе 16 КБ (нужно при extractNativeLibs=false)
else
        BT="$ANDROID_HOME/build-tools/34.0.0"
        ALIGN="-f 4"
        echo "внимание: build-tools 35+ не найдены, zipalign без -P 16" >&2
fi
[ -f "$BT/lib/apksigner.jar" ] || { echo "apksigner.jar не найден в $BT/lib" >&2; exit 1; }
# apksigner вызываем через java -jar: в build-tools под Windows sh-обёртки
# apksigner может не быть (только .bat), а java получает от Git Bash уже
# Windows-пути. JAVA_HOME с обратными слэшами переводим cygpath'ом.
JAVA=java
if [ -n "$JAVA_HOME" ]; then
        case "$JAVA_HOME" in
        *\\*) JAVA="$(cygpath -u "$JAVA_HOME" 2>/dev/null || echo "$JAVA_HOME")/bin/java" ;;
        *) JAVA="$JAVA_HOME/bin/java" ;;
        esac
fi
"$BT/zipalign" $ALIGN "$WORK/repack.apk" "$WORK/aligned.apk"

"$JAVA" -jar "$BT/lib/apksigner.jar" sign \
        --ks "$KEYSTORE" --ks-key-alias "${KEY_ALIAS:-poscloud}" \
        --ks-pass "pass:$KS_PASS" --key-pass "pass:${KEY_PASS:-$KS_PASS}" \
        --out "POSCloud-Client-$VERSION-android-arm64.apk" "$WORK/aligned.apk"

"$JAVA" -jar "$BT/lib/apksigner.jar" verify "POSCloud-Client-$VERSION-android-arm64.apk" >/dev/null \
        && echo "подпись OK"
check_align "POSCloud-Client-$VERSION-android-arm64.apk"
echo "Готово: POSCloud-Client-$VERSION-android-arm64.apk (versionCode $BUILD)"
