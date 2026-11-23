#!/bin/sh
# Сборка APK POSCloud Client для Android (arm64).
#
# Нужны: Go 1.25+, JDK 11+, Android SDK (platform-tools, platforms;android-34,
# build-tools;34.0.0), Android NDK ($ANDROID_NDK_HOME) и утилита fyne:
#   go install fyne.io/tools/cmd/fyne@latest
#
# Релизная подпись (необязательно): переменные окружения
#   KEYSTORE=<путь к .keystore> KS_PASS=<пароль> KEY_ALIAS=<алиас> KEY_PASS=<пароль>
# Без них APK остаётся с отладочной подписью fyne (для установки достаточно).
set -e
cd "$(dirname "$0")"

APPID=io.github.reznikov73.poscloud.client
NAME="POSCloud Client"
VERSION=${APP_VERSION:-$(cat ../installer/version.txt 2>/dev/null || echo 1.0.0)}
BUILD=${APP_BUILD:-1}

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
        go test -run TestBuildUISmoke .
fi

# fyne package --release собирает .so без отладочной информации (примерно на
# 20% меньше), затем APK перепаковывается и подписывается ниже
rm -f POSCloud_Client.apk
fyne package --release --target android/arm64 \
        --appID "$APPID" \
        --name "$NAME" \
        --app-version "$VERSION" --app-build "$BUILD" \
        --icon Icon.png

# перепаковка: убрать отладочную подпись fyne, resources.arsc — несжатым
# (требование targetSdk 30+), затем выравнивание и подпись
if [ -z "$KEYSTORE" ]; then
        mv POSCloud_Client.apk "POSCloud-Client-$VERSION-android-arm64-debug.apk"
        echo "Готово: POSCloud-Client-$VERSION-android-arm64-debug.apk (отладочная подпись fyne)."
        echo "Для релизной подписи задайте KEYSTORE, KS_PASS, KEY_ALIAS, KEY_PASS."
        exit 0
fi

python3 - POSCloud_Client.apk <<'PYEOF'
import sys, zipfile
src = zipfile.ZipFile(sys.argv[1])
out = zipfile.ZipFile('/tmp/poscloud-repack.apk', 'w')
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

BT="$ANDROID_HOME/build-tools/34.0.0"
"$BT/zipalign" -f 4 /tmp/poscloud-repack.apk /tmp/poscloud-aligned.apk

"$BT/apksigner" sign \
        --ks "$KEYSTORE" --ks-key-alias "${KEY_ALIAS:-poscloud}" \
        --ks-pass "pass:$KS_PASS" --key-pass "pass:${KEY_PASS:-$KS_PASS}" \
        --out "POSCloud-Client-$VERSION-android-arm64.apk" /tmp/poscloud-aligned.apk
rm -f /tmp/poscloud-repack.apk /tmp/poscloud-aligned.apk

"$BT/apksigner" verify "POSCloud-Client-$VERSION-android-arm64.apk" >/dev/null \
        && echo "подпись OK"
echo "Готово: POSCloud-Client-$VERSION-android-arm64.apk"
