// Package api exposes the catalog over HTTP: a public read-only API that the
// TV app consumes and an authenticated admin API behind the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ophymx/atvloader/server/internal/store"
)

type Config struct {
	Store *store.Store
	// Admin requests are authenticated by a reverse proxy (nginx + oauth2-proxy).
	// TrustedHeader names the header carrying the signed-in user; when
	// RequireTrustedHeader is set, admin requests without it are rejected.
	TrustedHeader        string
	RequireTrustedHeader bool
	MaxUploadMB          int64
	LoaderPackage        string // package name of the bundled TV app, if any
	ServerName           string
	Version              string
	AdminUI              fs.FS
	Landing              []byte // html/template source for the public front page
}

type Server struct {
	cfg     Config
	mux     *http.ServeMux
	landing *template.Template
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	m := s.mux

	// Public front page + loader download, so a TV browser can bootstrap the app.
	if cfg.Landing != nil {
		s.landing = template.Must(template.New("landing").Parse(string(cfg.Landing)))
		m.HandleFunc("GET /{$}", s.landingPage)
	}
	m.HandleFunc("GET /atvloader.apk", s.loaderDownload)

	// Unauthenticated: server discovery and the activation-code flow.
	m.HandleFunc("GET /api/v1/info", s.info)
	m.HandleFunc("POST /api/v1/device/code", s.deviceCode)
	m.HandleFunc("POST /api/v1/device/token", s.deviceToken)

	// Catalog: needs a device bearer token.
	m.Handle("GET /api/v1/apps", s.device(s.listApps))
	m.Handle("GET /api/v1/apps/{pkg}", s.device(s.getApp))
	m.Handle("GET /api/v1/apps/{pkg}/icon", s.device(s.icon))
	m.Handle("GET /api/v1/apps/{pkg}/versions/{code}/download", s.device(s.download))

	// Admin: behind the reverse proxy's login.
	m.Handle("GET /api/v1/admin/me", s.auth(s.me))
	m.Handle("GET /api/v1/admin/devices", s.auth(s.listDevices))
	m.Handle("POST /api/v1/admin/devices/approve", s.auth(s.approveDevice))
	m.Handle("POST /api/v1/admin/devices/deny", s.auth(s.denyDevice))
	m.Handle("PATCH /api/v1/admin/devices/{id}", s.auth(s.renameDevice))
	m.Handle("DELETE /api/v1/admin/devices/{id}", s.auth(s.revokeDevice))
	m.Handle("GET /api/v1/admin/apps", s.auth(s.adminListApps))
	m.Handle("GET /api/v1/admin/apps/{pkg}/icon", s.auth(s.icon))
	m.Handle("GET /api/v1/admin/apps/{pkg}/versions/{code}/download", s.auth(s.download))
	m.Handle("POST /api/v1/admin/apps", s.auth(s.upload))
	m.Handle("PATCH /api/v1/admin/apps/{pkg}", s.auth(s.patchApp))
	m.Handle("DELETE /api/v1/admin/apps/{pkg}", s.auth(s.deleteApp))
	m.Handle("POST /api/v1/admin/apps/{pkg}/icon", s.auth(s.setIcon))
	m.Handle("PATCH /api/v1/admin/apps/{pkg}/versions/{code}", s.auth(s.patchVersion))
	m.Handle("DELETE /api/v1/admin/apps/{pkg}/versions/{code}", s.auth(s.deleteVersion))
	m.Handle("GET /api/v1/admin/categories", s.auth(s.getCategories))
	m.Handle("PUT /api/v1/admin/categories", s.auth(s.putCategories))

	if cfg.AdminUI != nil {
		m.Handle("GET /admin/", s.auth(http.StripPrefix("/admin/", http.FileServerFS(cfg.AdminUI)).ServeHTTP))
		m.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
		})
		if s.landing == nil {
			m.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/admin/", http.StatusFound)
			})
		}
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := &statusWriter{ResponseWriter: w, status: 200}
	s.mux.ServeHTTP(rw, r)
	slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", time.Since(start).Milliseconds(), "from", r.RemoteAddr)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }

// --- auth ---------------------------------------------------------------

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
)

// adminUser extracts the identity set by the reverse proxy.
func (s *Server) adminUser(r *http.Request) string {
	for _, h := range []string{s.cfg.TrustedHeader, "X-Forwarded-User", "X-Forwarded-Email", "X-Forwarded-Preferred-Username", "X-Auth-Request-User", "X-Auth-Request-Email"} {
		if h == "" {
			continue
		}
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	return ""
}

// auth guards admin routes. Authentication itself happens upstream.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := s.adminUser(r)
		if user == "" && s.cfg.RequireTrustedHeader {
			writeErr(w, http.StatusUnauthorized, "no identity header from the reverse proxy")
			return
		}
		if user == "" {
			user = "anonymous"
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxUser, user)))
	})
}

// device guards catalog routes with the bearer token issued during pairing.
func (s *Server) device(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
		dev, ok := s.cfg.Store.AuthenticateToken(tok)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="atvloader"`)
			writeErr(w, http.StatusUnauthorized, "this TV is not linked; pair it from the admin page")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxDevice, dev)))
	})
}

// --- JSON shapes ----------------------------------------------------------

type versionJSON struct {
	VersionCode int64     `json:"versionCode"`
	VersionName string    `json:"versionName"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	MinSDK      int32     `json:"minSdk"`
	TargetSDK   int32     `json:"targetSdk"`
	AddedAt     time.Time `json:"addedAt"`
	Changelog   string    `json:"changelog"`
	DownloadURL string    `json:"downloadUrl"`
}

type appJSON struct {
	Package     string         `json:"package"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Category    string         `json:"category"`
	Featured    bool           `json:"featured"`
	Hidden      bool           `json:"hidden"`
	Protected   bool           `json:"protected"`
	IconURL     string         `json:"iconUrl"`
	AddedAt     time.Time      `json:"addedAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	Latest      *versionJSON   `json:"latest"`
	Versions    []*versionJSON `json:"versions,omitempty"`
}

func toVersion(a *store.App, v *store.Version, admin bool) *versionJSON {
	base := "/api/v1/apps/"
	if admin {
		base = "/api/v1/admin/apps/"
	}
	return &versionJSON{
		VersionCode: v.VersionCode, VersionName: v.VersionName, Size: v.Size, SHA256: v.SHA256,
		MinSDK: v.MinSDK, TargetSDK: v.TargetSDK, AddedAt: v.AddedAt, Changelog: v.Changelog,
		DownloadURL: fmt.Sprintf("%s%s/versions/%d/download", base, a.Package, v.VersionCode),
	}
}

func toApp(a *store.App, full bool, admin bool) *appJSON {
	j := &appJSON{
		Package: a.Package, Name: a.Name, Description: a.Description, Category: a.Category,
		Featured: a.Featured, Hidden: a.Hidden, Protected: a.Protected, AddedAt: a.AddedAt, UpdatedAt: a.UpdatedAt,
	}
	if a.Icon != "" {
		base := "/api/v1/apps/"
		if admin {
			base = "/api/v1/admin/apps/"
		}
		j.IconURL = base + a.Package + "/icon?v=" + strconv.FormatInt(a.UpdatedAt.Unix(), 10)
	}
	if l := a.Latest(); l != nil {
		j.Latest = toVersion(a, l, admin)
	}
	if full {
		j.Versions = make([]*versionJSON, 0, len(a.Versions))
		for _, v := range a.Versions {
			j.Versions = append(j.Versions, toVersion(a, v, admin))
		}
	}
	return j
}

// --- public handlers --------------------------------------------------------

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	apps := s.cfg.Store.List()
	visible := 0
	for _, a := range apps {
		if !a.Hidden {
			visible++
		}
	}
	out := map[string]any{
		"service": "atvloader",
		"name":    s.cfg.ServerName,
		"version": s.cfg.Version,
		"apps":    visible,
		"auth":    "device",
	}
	if s.cfg.LoaderPackage != "" {
		if a, err := s.cfg.Store.Get(s.cfg.LoaderPackage); err == nil && a.Latest() != nil {
			out["loader"] = map[string]any{"package": a.Package, "versionCode": a.Latest().VersionCode, "versionName": a.Latest().VersionName}
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	var out []*appJSON
	for _, a := range s.cfg.Store.List() {
		if a.Hidden || a.Latest() == nil {
			continue
		}
		out = append(out, toApp(a, false, false))
	}
	if out == nil {
		out = []*appJSON{}
	}
	writeJSON(w, 200, map[string]any{"apps": out, "categories": s.cfg.Store.Categories()})
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	a, err := s.cfg.Store.Get(r.PathValue("pkg"))
	if err != nil || a.Hidden {
		writeErr(w, 404, "app not found")
		return
	}
	writeJSON(w, 200, toApp(a, true, false))
}

func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	a, err := s.cfg.Store.Get(r.PathValue("pkg"))
	if err != nil || a.Icon == "" {
		writeErr(w, 404, "no icon")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, s.cfg.Store.Abs(a.Icon))
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	a, err := s.cfg.Store.Get(r.PathValue("pkg"))
	if err != nil {
		writeErr(w, 404, "app not found")
		return
	}
	code, _ := strconv.ParseInt(r.PathValue("code"), 10, 64)
	for _, v := range a.Versions {
		if v.VersionCode == code {
			w.Header().Set("Content-Type", "application/vnd.android.package-archive")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, path.Base(v.File)))
			w.Header().Set("X-SHA256", v.SHA256)
			http.ServeFile(w, r, s.cfg.Store.Abs(v.File))
			return
		}
	}
	writeErr(w, 404, "version not found")
}

// --- admin handlers ----------------------------------------------------------

func (s *Server) adminListApps(w http.ResponseWriter, r *http.Request) {
	apps := s.cfg.Store.List()
	out := make([]*appJSON, 0, len(apps))
	for _, a := range apps {
		out = append(out, toApp(a, true, true))
	}
	writeJSON(w, 200, map[string]any{"apps": out, "categories": s.cfg.Store.Categories()})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadMB<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, 400, "expected multipart/form-data with an 'apk' file: "+err.Error())
		return
	}
	var results []*store.ImportResult
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if part.FormName() != "apk" && part.FormName() != "apks" {
			part.Close()
			continue
		}
		res, err := s.cfg.Store.ImportAPK(part)
		part.Close()
		if err != nil {
			writeErr(w, 422, fmt.Sprintf("%s: %v", part.FileName(), err))
			return
		}
		slog.Info("imported apk", "package", res.App.Package, "versionCode", res.Version.VersionCode, "created", res.Created, "replaced", res.Replaced)
		results = append(results, res)
	}
	if len(results) == 0 {
		writeErr(w, 400, "no 'apk' file in upload")
		return
	}
	out := make([]map[string]any, 0, len(results))
	for _, res := range results {
		out = append(out, map[string]any{
			"app": toApp(res.App, true, true), "versionCode": res.Version.VersionCode,
			"created": res.Created, "replaced": res.Replaced,
		})
	}
	writeJSON(w, 201, map[string]any{"imported": out})
}

func (s *Server) patchApp(w http.ResponseWriter, r *http.Request) {
	var p store.AppPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	a, err := s.cfg.Store.UpdateApp(r.PathValue("pkg"), p)
	if s.storeErr(w, err) {
		return
	}
	writeJSON(w, 200, toApp(a, true, true))
}

func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	if s.storeErr(w, s.cfg.Store.DeleteApp(r.PathValue("pkg"))) {
		return
	}
	w.WriteHeader(204)
}

func (s *Server) setIcon(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	f, _, err := r.FormFile("icon")
	if err != nil {
		writeErr(w, 400, "expected multipart field 'icon' (PNG)")
		return
	}
	defer f.Close()
	a, err := s.cfg.Store.SetIcon(r.PathValue("pkg"), f)
	if s.storeErr(w, err) {
		return
	}
	writeJSON(w, 200, toApp(a, true, true))
}

func (s *Server) patchVersion(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Changelog string `json:"changelog"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	code, _ := strconv.ParseInt(r.PathValue("code"), 10, 64)
	a, err := s.cfg.Store.UpdateVersion(r.PathValue("pkg"), code, p.Changelog)
	if s.storeErr(w, err) {
		return
	}
	writeJSON(w, 200, toApp(a, true, true))
}

func (s *Server) deleteVersion(w http.ResponseWriter, r *http.Request) {
	code, _ := strconv.ParseInt(r.PathValue("code"), 10, 64)
	if s.storeErr(w, s.cfg.Store.DeleteVersion(r.PathValue("pkg"), code)) {
		return
	}
	w.WriteHeader(204)
}

func (s *Server) getCategories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"categories": s.cfg.Store.Categories()})
}

func (s *Server) putCategories(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Categories []string `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if err := s.cfg.Store.SetCategories(p.Categories); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"categories": s.cfg.Store.Categories()})
}

// --- front page -----------------------------------------------------------

func (s *Server) loaderApp() (*store.App, *store.Version) {
	if s.cfg.LoaderPackage == "" {
		return nil, nil
	}
	a, err := s.cfg.Store.Get(s.cfg.LoaderPackage)
	if err != nil || a.Latest() == nil {
		return nil, nil
	}
	return a, a.Latest()
}

func (s *Server) landingPage(w http.ResponseWriter, r *http.Request) {
	type loader struct {
		VersionName, SizeHuman, ShortSHA string
	}
	data := struct {
		Name, Version, Address string
		Loader                 *loader
	}{Name: s.cfg.ServerName, Version: s.cfg.Version, Address: externalAddress(r)}
	if _, v := s.loaderApp(); v != nil {
		data.Loader = &loader{VersionName: v.VersionName, SizeHuman: humanSize(v.Size), ShortSHA: v.SHA256[:12]}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := s.landing.Execute(w, data); err != nil {
		slog.Error("landing", "err", err)
	}
}

// loaderDownload serves the newest bundled ATV Loader APK at a stable URL.
func (s *Server) loaderDownload(w http.ResponseWriter, r *http.Request) {
	a, v := s.loaderApp()
	if v == nil {
		writeErr(w, 404, "this server has no ATV Loader APK bundled")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="atvloader.apk"`)
	w.Header().Set("X-SHA256", v.SHA256)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.cfg.Store.Abs(v.File))
	_ = a
}

// externalAddress reconstructs the URL a TV should type into the app.
func externalAddress(r *http.Request) string {
	scheme := "http"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func humanSize(b int64) string {
	switch {
	case b < 1<<20:
		return fmt.Sprintf("%d KB", b>>10)
	case b < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	}
}

// --- device pairing -----------------------------------------------------------

func (s *Server) deviceCode(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Model   string `json:"model"`
		Product string `json:"product"`
		SDK     int    `json:"sdk"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p)
	c := s.cfg.Store.NewDeviceCode(p.Model, p.Product, p.SDK)
	slog.Info("pairing code issued", "userCode", c.UserCode, "model", c.Model, "from", r.RemoteAddr)
	writeJSON(w, 200, map[string]any{
		"deviceCode": c.DeviceCode,
		"userCode":   c.UserCode,
		"expiresIn":  int(time.Until(c.ExpiresAt).Seconds()),
		"interval":   store.PollInterval,
	})
}

func (s *Server) deviceToken(w http.ResponseWriter, r *http.Request) {
	var p struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p); err != nil {
		writeErr(w, 400, "bad json")
		return
	}
	status, token, dev, err := s.cfg.Store.PollDeviceCode(p.DeviceCode)
	if err != nil {
		writeJSON(w, 200, map[string]any{"status": "expired"})
		return
	}
	out := map[string]any{"status": status}
	if status == "approved" {
		out["token"] = token
		out["deviceName"] = dev.Name
		slog.Info("device linked", "device", dev.ID, "name", dev.Name)
	}
	writeJSON(w, 200, out)
}

type deviceJSON struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeen   time.Time `json:"lastSeen"`
	ApprovedBy string    `json:"approvedBy"`
}

func toDevice(d *store.Device) deviceJSON {
	return deviceJSON{ID: d.ID, Name: d.Name, Model: d.Model, CreatedAt: d.CreatedAt, LastSeen: d.LastSeen, ApprovedBy: d.ApprovedBy}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"user": r.Context().Value(ctxUser), "store": s.cfg.ServerName, "version": s.cfg.Version})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	devs := s.cfg.Store.Devices()
	out := make([]deviceJSON, 0, len(devs))
	for _, d := range devs {
		out = append(out, toDevice(d))
	}
	pending := s.cfg.Store.PendingCodes()
	if pending == nil {
		pending = []*store.PendingCode{}
	}
	writeJSON(w, 200, map[string]any{"devices": out, "pending": pending})
}

func (s *Server) approveDevice(w http.ResponseWriter, r *http.Request) {
	var p struct {
		UserCode string `json:"userCode"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "bad json")
		return
	}
	user, _ := r.Context().Value(ctxUser).(string)
	dev, err := s.cfg.Store.ApproveCode(p.UserCode, p.Name, user)
	switch {
	case errors.Is(err, store.ErrCodeUnknown):
		writeErr(w, 404, "no TV is waiting with that code (codes expire after 15 minutes)")
		return
	case err != nil:
		writeErr(w, 409, err.Error())
		return
	}
	slog.Info("device approved", "device", dev.ID, "name", dev.Name, "by", user)
	writeJSON(w, 201, toDevice(dev))
}

func (s *Server) denyDevice(w http.ResponseWriter, r *http.Request) {
	var p struct {
		UserCode string `json:"userCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "bad json")
		return
	}
	if err := s.cfg.Store.DenyCode(p.UserCode); err != nil {
		writeErr(w, 404, "unknown code")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "bad json")
		return
	}
	d, err := s.cfg.Store.RenameDevice(r.PathValue("id"), p.Name)
	if s.storeErr(w, err) {
		return
	}
	writeJSON(w, 200, toDevice(d))
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	if s.storeErr(w, s.cfg.Store.RevokeDevice(r.PathValue("id"))) {
		return
	}
	w.WriteHeader(204)
}

// --- helpers -------------------------------------------------------------------

func (s *Server) storeErr(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "not found")
	case errors.Is(err, store.ErrProtected):
		writeErr(w, 409, err.Error())
	default:
		slog.Error("store", "err", err)
		writeErr(w, 500, err.Error())
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg, "status": status})
}
