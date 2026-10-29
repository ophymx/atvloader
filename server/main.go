// atvloader-server hosts a private catalog of Android TV APKs with a REST API
// for the TV app and a web admin UI for uploading and curating apps.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"io"

	"github.com/ophymx/atvloader/server/internal/api"
	"github.com/ophymx/atvloader/server/internal/store"
	"github.com/ophymx/atvloader/server/self"
	"github.com/ophymx/atvloader/server/web"
)

// importLoader files the ATV Loader APK (embedded, or from -self-apk) as a
// protected app so TVs can update the loader from the store itself.
func importLoader(st *store.Store, path string) string {
	var r io.ReadCloser
	var src string
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			slog.Error("open loader apk", "path", path, "err", err)
			return ""
		}
		r, src = f, path
	} else if f, name, ok := self.APK(); ok {
		r, src = f, "embedded "+name
	} else {
		slog.Warn("no ATV Loader APK bundled: build with server/self/atvloader.apk present or pass -self-apk so TVs can update the loader from the store")
		return ""
	}
	defer r.Close()
	res, err := st.ImportAPK(r, store.ImportOptions{
		Protect: true, SkipSameSHA: true, Name: "ATV Loader",
		Description: "This app. Install updates here to get new features on this TV.",
	})
	if err != nil {
		slog.Error("import loader apk", "src", src, "err", err)
		return ""
	}
	slog.Info("loader apk", "src", src, "package", res.App.Package, "versionCode", res.Version.VersionCode, "versionName", res.Version.VersionName, "new", !res.Unchanged)
	return res.App.Package
}

var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := flag.String("addr", env("ATVLOADER_ADDR", ":8080"), "listen address")
	dataDir := flag.String("data", env("ATVLOADER_DATA", "./data"), "data directory for APKs, icons and the index")
	trustedHeader := flag.String("trusted-header", env("ATVLOADER_TRUSTED_HEADER", "X-Forwarded-User"), "request header set by the reverse proxy carrying the admin's identity")
	requireHeader := flag.Bool("require-trusted-header", env("ATVLOADER_REQUIRE_TRUSTED_HEADER", "") == "true", "reject admin requests that lack the identity header")
	name := flag.String("name", env("ATVLOADER_NAME", "Homelab apps"), "store name shown on the TV")
	selfAPK := flag.String("self-apk", env("ATVLOADER_SELF_APK", ""), "path to the ATV Loader APK to publish (default: the one embedded at build time)")
	maxMB, _ := strconv.ParseInt(env("ATVLOADER_MAX_UPLOAD_MB", "2048"), 10, 64)
	flag.Parse()

	st, err := store.Open(*dataDir)
	if err != nil {
		slog.Error("open store", "dir", *dataDir, "err", err)
		os.Exit(1)
	}
	if !*requireHeader {
		slog.Warn("admin UI accepts requests without an identity header; put it behind oauth2-proxy and set ATVLOADER_REQUIRE_TRUSTED_HEADER=true")
	}

	loaderPkg := importLoader(st, *selfAPK)

	srv := api.New(api.Config{
		Store: st, TrustedHeader: *trustedHeader, RequireTrustedHeader: *requireHeader,
		MaxUploadMB: maxMB, LoaderPackage: loaderPkg, ServerName: *name, Version: version, AdminUI: web.Admin(), Landing: web.Landing(),
	})
	h := &http.Server{
		Addr: *addr, Handler: srv,
		ReadHeaderTimeout: 10 * time.Second,
		// No overall read/write timeout: uploads and downloads can be large.
	}
	slog.Info("atvloader listening", "addr", *addr, "data", *dataDir, "apps", len(st.List()), "version", version)
	if err := h.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
