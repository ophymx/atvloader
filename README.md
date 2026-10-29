# atvloader

A private app store for Android TV, for a homelab.

- **server/** – Go binary. Hosts the APK catalog, a REST API the TV app talks to, and the web admin UI (embedded, no build step).
- **tv/** – Kotlin + Compose for TV app. Browses the catalog with the remote, installs/updates/uninstalls apps with the system package installer.
- **deploy/** – example nginx config for running the admin behind oauth2-proxy.
- **tools/genassets/** – renders the TV banner and launcher icon PNGs.

## How it fits together

```
 TV app  ──bearer token──▶  /api/v1/apps, /download, /icon   ┐
 TV app  ──no auth──────▶  /api/v1/info, /api/v1/device/*    ├─ atvloader-server ─ ./data
 Browser ─oauth2-proxy──▶  /admin/, /api/v1/admin/*          ┘
```

The admin side does **no authentication of its own**: it trusts the identity header that nginx + oauth2-proxy set (`X-Forwarded-User` by default). TVs authenticate with a bearer token they get through an activation-code flow:

1. The TV asks the server for a code and shows it (`ABC-DEF`).
2. You open the admin page → Devices, see the code in the waiting list (or type it), and approve it.
3. The TV, which has been polling, receives its token and loads the store.

Unlinking a TV from the Devices page invalidates its token; the TV drops back to the pairing screen.

### The loader updates itself from the store

The server carries the ATV Loader APK it was built with (`server/self/atvloader.apk`, embedded into the binary; `make apk` stages it, or point `-self-apk` / `ATVLOADER_SELF_APK` at a file). At startup it is imported into the catalog as a *protected* app: always listed, cannot be hidden or removed. When a TV sees a newer version of itself in the catalog it shows an "update now" banner above the shelves, and installs it through the normal package installer; the app restarts on the new build.

To ship a new TV build: bump `versionCode` in `tv/app/build.gradle.kts`, `make apk`, rebuild and restart the server (or `make docker`). Every linked TV offers the update on its next refresh.

## Build and run

```sh
make apk        # TV app → server/self/atvloader.apk (needs the Android SDK, see below)
make server     # → dist/atvloader-server with the APK embedded
make run        # serve ./data on :8080
# or
make apk && docker compose up -d --build
```

### Debian package

[GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`) builds the server for amd64 and arm64 and packages it as a `.deb` with a systemd unit, an `atvloader` system user and `/etc/atvloader/env` (loopback listen, `/var/lib/atvloader` data dir, identity header required). Stage the APK first; the build refuses to run without it.

```sh
make apk
make snapshot   # untagged: dist/atvloader_<ver>_{amd64,arm64}.deb + tarballs
make release    # on a v* tag: same, published as a Gitea release (needs GITEA_TOKEN)
```

Install with `dpkg -i`, then `systemctl start atvloader` and put nginx + oauth2-proxy in front (`/usr/share/doc/atvloader/nginx.conf.example`).

| Setting | Env var | Default |
| --- | --- | --- |
| Listen address | `ATVLOADER_ADDR` | `:8080` |
| Data directory (APKs, icons, `store.json`) | `ATVLOADER_DATA` | `./data` |
| Store name shown on the TV | `ATVLOADER_NAME` | `Homelab apps` |
| Header carrying the admin identity | `ATVLOADER_TRUSTED_HEADER` | `X-Forwarded-User` |
| Reject admin requests without that header | `ATVLOADER_REQUIRE_TRUSTED_HEADER` | `false` |
| Max upload size | `ATVLOADER_MAX_UPLOAD_MB` | `2048` |
| Loader APK to publish instead of the embedded one | `ATVLOADER_SELF_APK` | |

Admin UI: `http://host:8080/admin/`. Until nginx is in front, anyone who can reach the port can administer the store, so set `ATVLOADER_REQUIRE_TRUSTED_HEADER=true` once the proxy is wired up (see `deploy/nginx.conf`; the proxy must strip `X-Forwarded-User` on the non-admin routes, as the example does).

## REST API

Unauthenticated:

| | |
| --- | --- |
| `GET /` | public front page with the loader download, for TV browsers |
| `GET /atvloader.apk` | newest bundled ATV Loader APK |
| `GET /api/v1/info` | `{service:"atvloader", name, version, apps, auth:"device", loader:{package, versionCode, versionName}}` |
| `POST /api/v1/device/code` `{model, product, sdk}` | `{deviceCode, userCode, expiresIn, interval}` |
| `POST /api/v1/device/token` `{deviceCode}` | `{status:"pending"}` / `{status:"approved", token, deviceName}` / `{status:"denied"|"expired"}` |

With `Authorization: Bearer <token>`:

| | |
| --- | --- |
| `GET /api/v1/apps` | `{apps:[…], categories:[…]}` (visible apps, latest version each) |
| `GET /api/v1/apps/{pkg}` | full app with every version |
| `GET /api/v1/apps/{pkg}/icon` | PNG |
| `GET /api/v1/apps/{pkg}/versions/{versionCode}/download` | the APK (supports ranges, `X-SHA256` header) |

Admin (behind the proxy):

| | |
| --- | --- |
| `GET /api/v1/admin/me` | who the proxy says you are |
| `GET /api/v1/admin/apps` | every app including hidden ones |
| `POST /api/v1/admin/apps` multipart `apk=@file` | import; package, version, label and icon are read from the APK |
| `PATCH /api/v1/admin/apps/{pkg}` `{name, description, category, featured, hidden}` | edit |
| `DELETE /api/v1/admin/apps/{pkg}` | remove app and files |
| `POST /api/v1/admin/apps/{pkg}/icon` multipart `icon=@png` | replace icon |
| `PATCH /api/v1/admin/apps/{pkg}/versions/{code}` `{changelog}` | per-version note |
| `DELETE /api/v1/admin/apps/{pkg}/versions/{code}` | remove one APK |
| `GET/PUT /api/v1/admin/categories` `{categories:[…]}` | row order on the TV |
| `GET /api/v1/admin/devices` | linked TVs and codes waiting for approval |
| `POST /api/v1/admin/devices/approve` `{userCode, name}` | link a TV |
| `POST /api/v1/admin/devices/deny` `{userCode}` | |
| `PATCH /api/v1/admin/devices/{id}` `{name}` / `DELETE` | rename / unlink |

Data on disk is plain: `data/store.json`, `data/apks/<pkg>-<versionCode>.apk`, `data/icons/<pkg>.png`. Back up the directory and you have everything.

## Build the TV app

Needs JDK 17+ and the Android SDK (`tv/local.properties` → `sdk.dir`).

```sh
cd tv
./gradlew assembleRelease
# → app/build/outputs/apk/release/app-release.apk
```

Release signing reads `tv/keystore.properties` (`storeFile`, `storePassword`, `keyAlias`, `keyPassword`); without it the APK is signed with the debug key. **Keep the same key forever**: Android refuses to update an app signed with a different key, so losing the keystore means uninstalling ATV Loader from every TV. Create one with:

```sh
keytool -genkeypair -v -keystore tv/keystore/release.jks -alias atvloader \
  -keyalg RSA -keysize 2048 -validity 10000
```

## Install it on a TV

The server's front page (`http://host:8080/`) is public and has one big download button, so you can bootstrap from any TV browser: open the page, download, open the APK from the browser's downloads (allow the browser to install unknown apps when asked). The APK is also at the stable URL `/atvloader.apk`. `adb install` works too. On first launch:

1. Enter the server address, e.g. `http://192.168.1.10:8080` (plain HTTP is fine on the LAN; cleartext is enabled).
2. Approve the code on the admin page → Devices.
3. The first install prompts for "Install unknown apps"; the app sends you to the right settings page.

Once ATV Loader is itself uploaded to the store it can update itself from there like any other app.

## Layout of the TV screen

Rows, in order: *Updates available*, *Featured*, each category in the order set under "Edit categories", uncategorized apps, then *Installed*. An app's page shows the description, install/update/open/uninstall, and every uploaded version so an older one can be installed if a new build misbehaves.
