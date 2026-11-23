# POSCloud Client for Android

A separate version of the POSCloud client for Android — **entirely in Go**, without a single
line of Java/Kotlin. Built into an APK with `fyne package` (gomobile +
Android NDK under the hood). Synchronization is performed by the shared
engine [`poscloud-engine`](../engine) — the same code as the desktop client
and Lite, so the Android client is fully compatible with the POSCloud server:
protocol, block transfer, versions, tombstones, conflicts and folder rules.

## Features

- **camera capture**: the “Take photo” button opens the system camera, the shot is written into
  the sync folder (through MediaStore) and decoded on the spot — the app connects by itself;
- **screenshots**: system screenshots (Power + Volume Down) are copied every 30 seconds
  from `Pictures/Screenshots` / `DCIM/Screenshots` (read through MediaStore) into
  “Снимки экрана” inside the sync folder. Android shows another app's files to
  MediaStore only with the photo permission, so the app asks for it at first
  launch (the “Status” tab has the button when the permission is missing); after
  the dialog is declined twice the button opens the app settings;
- **two-way folder synchronization with the server**: polling at an interval,
  file watching (inotify works on internal storage) and a subscription to
  server changes (SSE `/events`) — just like the desktop client;
- **connection via code** `poscloud://connect?...` — a code created in the
  server window is exchanged for a permanent token with one tap;
- **connection via QR code**: the server displays the connection code as a QR image,
  the phone photographs it with the system camera, the shot is decoded
  right in the app (gozxing) and connects automatically — no need to
  manually type ~250 characters of code. Both kinds of QR are supported: “Connection
  code” (a one-time secret is exchanged for a token) and “Device
  token” (a ready-made token is saved as is, without re-issuing on the server);
- **manual setup**: address, port, token, interval, TLS with verification of
  the SHA-256 fingerprint of a self-signed certificate;
- **choosing the sync folder**: a folder dialog, the private app folder and the
  external app folder on shared storage (`Android/data/<package>/files/sync`),
  which is not wiped with the app data;
- **selective synchronization**: first-level folders with modes “both ways /
  send only / receive only / do not synchronize” and
  a “no rules” mode;
- **transfer progress** with a progress bar and a log;
- **compatible settings**: `client.json` follows the desktop
  client's schema (the engine part), the file can be transferred between devices.

## Structure

| Path | What it is |
|---|---|
| `main.go` | entry point: app setup, autorun, starts the interface from `internal/ui` |
| `internal/ui/` | Fyne interface: tabs “Status”, “Connection”, “Folders”, “Log”, brand theme |
| `internal/client/` | controller: settings, engine, log, progress, QR decoder — no UI (tested separately) |
| `tools/genicon/` | `Icon.png` generator: draws the icon from vector geometry with 4x supersampling — deterministic, the repository is text only |
| `build.sh` | local APK build: tests, package, signing |
| `../engine` | the shared sync engine (included via `replace` in `go.mod`) |

Build artifacts (`Icon.png`, `*.apk`, signing keys) never get into the
repository — see `.gitignore`; the APK is built locally with `build.sh`, CI checks the module's tests (job `android-unit`).

## How it works on Android

- app data (settings, log, default sync folder)
  live in the app's private directory (`/data/data/<package>/files`);
- the engine and SSE work unchanged; fsnotify on Android's internal storage
  is supported; on external storage (FUSE) the engine automatically falls back to a timer;
- synchronization runs while the app is open or is kept alive by the system
  in the background. Guaranteed background operation requires a foreground service — an
  intentional limitation of the first version (pure Go without Java wrappers);
- writing to arbitrary external directories (`/storage/emulated/0/...`) on
  Android 11+ is restricted by the system: you can specify a path, but without
  the “All files access” permission it will not work.

## Building the APK

Requires: Go 1.25+, Java JDK 11+, Android SDK (platform-tools, platforms;android-34,
build-tools;34.0.0), Android NDK and the `fyne` utility
(`go install fyne.io/tools/cmd/fyne@latest`).

```bash
# environment variables
export ANDROID_HOME=/path/to/android-sdk
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/27.0.12077973

# quick debug APK (fyne debug signature, installs right away)
sh ./build.sh

# release build with your own key (environment variables, not arguments)
KEYSTORE=poscloud-release.keystore KS_PASS=… KEY_ALIAS=poscloud KEY_PASS=… sh ./build.sh
```

The same manually (`build.sh` does all of this itself — including repacking and signing):

```bash
go run ./tools/genicon                    # restore Icon.png
fyne package --release --target android/arm64 \
  --appID io.github.reznikov73.poscloud.client \
  --name "POSCloud Client" --app-version 1.0.0 --app-build 1 \
  --icon Icon.png                         # --release: .so without debug info

# release signing: drop the debug META-INF, resources.arsc — uncompressed,
align and sign (targetSdk 30+ requires an uncompressed resources.arsc)
BT=$ANDROID_HOME/build-tools/34.0.0
$BT/zipalign -f 4 POSCloud_Client.apk aligned.apk
$BT/apksigner sign --ks poscloud-release.keystore --ks-key-alias poscloud \
  --ks-pass pass:<password> --key-pass pass:<password> \
  --out POSCloud-Client-1.0.0-android-arm64.apk aligned.apk
```

Architectures: `--target android/arm64` — all modern phones;
`--target android/arm` — 32-bit devices; `--target android` —
a universal APK with all ABIs (~4 times larger).

### Signing key

The key is not stored in the repository. For your own release builds create one
(once; without it you cannot update an installed app — signatures
must match):

```bash
keytool -genkeypair -v -keystore poscloud-release.keystore \
  -alias poscloud -keyalg RSA -keysize 2048 -validity 10000
```

`build.sh` signs the APK with an fyne debug signature by default — you can
install it, but it will not install on top of a release signature. For publishing to
the Play Store use your own key and an `.aab` via `bundletool`.

## Testing

```bash
go test ./internal/...          # controller logic: settings, log, progress
go test -run TestBuildUISmoke . # the interface on the Fyne test driver
POSCLOUD_SHOTS=/tmp/shots go test -run TestScreenshotRender .  # tab screenshots
```

CI runs `go test ./internal/...` and `go vet ./internal/...` for this module
(job `android-unit`).

A desktop Linux build for debugging requires system GL libraries
(`libgl1-mesa-dev libwayland-dev libxkbcommon-dev` and X11 dev); for Android
they are not needed.
