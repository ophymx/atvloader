# Contributing

Issues and pull requests are welcome. For anything bigger than a bug fix,
open an issue first so we can agree on the shape before you spend time on
it.

## Layout

- `server/` Go, standard library HTTP, no framework. `internal/store` owns
  the data (`store.json` plus files), `internal/api` the HTTP surface,
  `web/` the embedded admin UI (plain HTML/CSS/JS, no build step).
- `tv/` Kotlin + Compose for TV. `data/` talks to the server, `install/`
  wraps PackageInstaller, `ui/` is the screens and view model.
- `tools/version.sh` derives the version from git tags for both halves.

## Working on it

```sh
make test            # Go tests + TV unit tests
make apk             # TV app (needs the Android SDK; tv/local.properties → sdk.dir)
make run             # server on :8080 with ./data, admin at /admin/
make snapshot        # what CI builds: .deb, tarballs, images, APK
```

Without the SDK you can still work on the server: `go -C server run .`
serves the catalog and the admin UI; the loader APK is simply absent.

There is no emulator in CI. Changes to the TV app should be tried on a
device (`adb install -r tv/app/build/outputs/apk/release/app-release.apk`)
and the PR should say what you checked.

## Style

- Go: `gofmt`, `go vet`. Errors are wrapped with context and the sentinel
  errors in `store` map to HTTP statuses in `api.storeErr`.
- Kotlin: the default Android Studio formatter.
- Keep the admin UI dependency-free. It is one HTML file, one stylesheet
  and one script on purpose.
- Commit messages: a short imperative subject, then why in the body.
  The changelog in each release is generated from them.

## Releases

Maintainers cut releases by pushing a `v*` tag; see "Releases" in the
README. The APK signing key is not in the repository and never will be.
