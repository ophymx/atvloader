package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ophymx/atvloader/server/internal/store"
)

const loaderAPK = "../../self/atvloader.apk"

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func newTestServer(t *testing.T, require bool) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{Store: st, TrustedHeader: "X-Forwarded-User", RequireTrustedHeader: require, MaxUploadMB: 64, ServerName: "test", Version: "test"})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, st
}

type req struct {
	method, path, body string
	headers            map[string]string
}

func do(t *testing.T, ts *httptest.Server, r req) (int, map[string]any) {
	t.Helper()
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	hr, _ := http.NewRequest(r.method, ts.URL+r.path, body)
	if r.body != "" {
		hr.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func admin(h ...string) map[string]string {
	m := map[string]string{"X-Forwarded-User": "abic"}
	for i := 0; i+1 < len(h); i += 2 {
		m[h[i]] = h[i+1]
	}
	return m
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func TestHealthz(t *testing.T) {
	ts, _ := newTestServer(t, true)
	code, body := do(t, ts, req{method: "GET", path: "/healthz"})
	if code != 200 || body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("got %d %v", code, body)
	}
}

func TestPairingFlow(t *testing.T) {
	ts, _ := newTestServer(t, false)

	// Catalog needs a device token.
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/apps"}); code != 401 {
		t.Fatalf("unauthenticated catalog: %d", code)
	}

	code, dc := do(t, ts, req{method: "POST", path: "/api/v1/device/code", body: `{"model":"Test TV","product":"test","sdk":34}`})
	if code != 200 || dc["userCode"] == "" || dc["deviceCode"] == "" {
		t.Fatalf("device code: %d %v", code, dc)
	}
	poll := func() map[string]any {
		_, b := do(t, ts, req{method: "POST", path: "/api/v1/device/token", body: `{"deviceCode":"` + dc["deviceCode"].(string) + `"}`})
		return b
	}
	if b := poll(); b["status"] != "pending" {
		t.Fatalf("before approval: %v", b)
	}

	// The code shows up for the admin, who approves it by user code.
	code, dl := do(t, ts, req{method: "GET", path: "/api/v1/admin/devices", headers: admin()})
	if code != 200 || len(dl["pending"].([]any)) != 1 {
		t.Fatalf("pending list: %d %v", code, dl)
	}
	code, _ = do(t, ts, req{method: "POST", path: "/api/v1/admin/devices/approve", headers: admin(), body: `{"userCode":"` + dc["userCode"].(string) + `","name":"Living room"}`})
	if code != 201 {
		t.Fatalf("approve: %d", code)
	}
	b := poll()
	tok, _ := b["token"].(string)
	if b["status"] != "approved" || tok == "" || b["deviceName"] != "Living room" {
		t.Fatalf("after approval: %v", b)
	}
	// A device code is single use.
	if b := poll(); b["status"] == "approved" {
		t.Fatalf("device code reusable: %v", b)
	}

	code, apps := do(t, ts, req{method: "GET", path: "/api/v1/apps", headers: bearer(tok)})
	if code != 200 || apps["apps"] == nil {
		t.Fatalf("catalog with device token: %d %v", code, apps)
	}
	// Unlinking invalidates the token.
	_, dl = do(t, ts, req{method: "GET", path: "/api/v1/admin/devices", headers: admin()})
	id := dl["devices"].([]any)[0].(map[string]any)["id"].(string)
	if code, _ := do(t, ts, req{method: "DELETE", path: "/api/v1/admin/devices/" + id, headers: admin()}); code != 204 {
		t.Fatalf("unlink: %d", code)
	}
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/apps", headers: bearer(tok)}); code != 401 {
		t.Fatalf("catalog after unlink: %d", code)
	}
}

func TestDenyCode(t *testing.T) {
	ts, _ := newTestServer(t, false)
	_, dc := do(t, ts, req{method: "POST", path: "/api/v1/device/code", body: `{}`})
	if code, _ := do(t, ts, req{method: "POST", path: "/api/v1/admin/devices/deny", headers: admin(), body: `{"userCode":"` + dc["userCode"].(string) + `"}`}); code/100 != 2 {
		t.Fatalf("deny: %d", code)
	}
	_, b := do(t, ts, req{method: "POST", path: "/api/v1/device/token", body: `{"deviceCode":"` + dc["deviceCode"].(string) + `"}`})
	if b["status"] != "denied" {
		t.Fatalf("after deny: %v", b)
	}
}

func TestRequireTrustedHeader(t *testing.T) {
	ts, _ := newTestServer(t, true)
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps"}); code != 401 {
		t.Fatalf("admin without header: %d", code)
	}
	code, me := do(t, ts, req{method: "GET", path: "/api/v1/admin/me", headers: admin()})
	if code != 200 || me["user"] != "abic" || me["kind"] != "admin" {
		t.Fatalf("admin with header: %d %v", code, me)
	}
	// Alternate proxy header names are accepted too.
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: map[string]string{"X-Forwarded-Email": "a@b.c"}}); code != 200 {
		t.Fatalf("admin with email header: %d", code)
	}
	// A device token is not an identity for the admin API.
	_, dc := do(t, ts, req{method: "POST", path: "/api/v1/device/code", body: `{}`})
	do(t, ts, req{method: "POST", path: "/api/v1/admin/devices/approve", headers: admin(), body: `{"userCode":"` + dc["userCode"].(string) + `"}`})
	_, b := do(t, ts, req{method: "POST", path: "/api/v1/device/token", body: `{"deviceCode":"` + dc["deviceCode"].(string) + `"}`})
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: bearer(b["token"].(string))}); code != 401 {
		t.Fatalf("device token on admin API: %d", code)
	}
	// Device endpoints never need it.
	if code, _ := do(t, ts, req{method: "POST", path: "/api/v1/device/code", body: `{}`}); code != 200 {
		t.Fatalf("device code with header required: %d", code)
	}
}

func upload(t *testing.T, ts *httptest.Server, headers map[string]string, apkPath string, fields map[string]string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("apk", "app.apk")
	f, err := os.Open(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(fw, f)
	f.Close()
	mw.Close()
	hr, _ := http.NewRequest("POST", ts.URL+"/api/v1/admin/apps", &buf)
	hr.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range headers {
		hr.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPITokens(t *testing.T) {
	if _, err := os.Stat(loaderAPK); err != nil {
		t.Skip("no loader APK built")
	}
	ts, _ := newTestServer(t, true)

	mint := func(body string) string {
		code, b := do(t, ts, req{method: "POST", path: "/api/v1/admin/tokens", headers: admin(), body: body})
		if code != 201 || b["secret"] == nil {
			t.Fatalf("mint %s: %d %v", body, code, b)
		}
		return b["secret"].(string)
	}
	// Unknown scopes are refused outright.
	if code, _ := do(t, ts, req{method: "POST", path: "/api/v1/admin/tokens", headers: admin(), body: `{"name":"x","scopes":["root"]}`}); code != 400 {
		t.Fatalf("bad scope: %d", code)
	}
	other := mint(`{"name":"other app","scopes":["publish","read"],"packages":["com.example.other"]}`)
	loader := mint(`{"name":"loader","scopes":["publish","read"],"packages":["com.ophymx.atvloader"]}`)
	readOnly := mint(`{"name":"ro","scopes":["read"]}`)

	if !strings.HasPrefix(other, store.TokenPrefix) {
		t.Fatalf("token prefix: %q", other)
	}
	// A token can act without the proxy header, even when it is required.
	code, me := do(t, ts, req{method: "GET", path: "/api/v1/admin/me", headers: bearer(readOnly)})
	if code != 200 || me["kind"] != "token" {
		t.Fatalf("me with token: %d %v", code, me)
	}
	// Tokens never manage tokens, and a forged identity header does not help.
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/tokens", headers: bearer(readOnly)}); code != 403 {
		t.Fatalf("token listing tokens: %d", code)
	}
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/tokens", headers: map[string]string{"Authorization": "Bearer " + readOnly, "X-Forwarded-User": "abic"}}); code != 403 {
		t.Fatalf("token + forged header: %d", code)
	}
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: bearer("atv_nope")}); code != 401 {
		t.Fatalf("bogus token: %d", code)
	}

	// Package-restricted publish: wrong package is refused after parsing, nothing stored.
	if code, b := upload(t, ts, bearer(other), loaderAPK, nil); code != 403 {
		t.Fatalf("upload foreign package: %d %v", code, b)
	}
	if code, l := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: admin()}); code != 200 || len(l["apps"].([]any)) != 0 {
		t.Fatalf("store after refused upload: %d %v", code, l)
	}
	if code, b := upload(t, ts, bearer(loader), loaderAPK, nil); code != 201 {
		t.Fatalf("upload own package: %d %v", code, b)
	}
	// read is filtered by package list; manage is not granted.
	if _, l := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: bearer(other)}); len(l["apps"].([]any)) != 0 {
		t.Fatalf("restricted read sees foreign apps: %v", l)
	}
	if _, l := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: bearer(readOnly)}); len(l["apps"].([]any)) != 1 {
		t.Fatalf("unrestricted read: %v", l)
	}
	if code, _ := do(t, ts, req{method: "PATCH", path: "/api/v1/admin/apps/com.ophymx.atvloader", headers: bearer(loader), body: `{"hidden":true}`}); code != 403 {
		t.Fatalf("manage without scope: %d", code)
	}
	if code, _ := upload(t, ts, bearer(readOnly), loaderAPK, nil); code != 403 {
		t.Fatalf("publish without scope: %d", code)
	}

	// Revocation takes effect immediately.
	_, tl := do(t, ts, req{method: "GET", path: "/api/v1/admin/tokens", headers: admin()})
	for _, x := range tl["tokens"].([]any) {
		tok := x.(map[string]any)
		if tok["name"] == "loader" {
			if code, _ := do(t, ts, req{method: "DELETE", path: "/api/v1/admin/tokens/" + tok["id"].(string), headers: admin()}); code != 204 {
				t.Fatalf("revoke: %d", code)
			}
		}
	}
	if code, _ := do(t, ts, req{method: "GET", path: "/api/v1/admin/apps", headers: bearer(loader)}); code != 401 {
		t.Fatalf("revoked token: %d", code)
	}
}

func TestDownloadHasHash(t *testing.T) {
	if _, err := os.Stat(loaderAPK); err != nil {
		t.Skip("no loader APK built")
	}
	ts, _ := newTestServer(t, false)
	if code, b := upload(t, ts, admin(), loaderAPK, nil); code != 201 {
		t.Fatalf("upload: %d %v", code, b)
	}
	_, dc := do(t, ts, req{method: "POST", path: "/api/v1/device/code", body: `{}`})
	do(t, ts, req{method: "POST", path: "/api/v1/admin/devices/approve", headers: admin(), body: `{"userCode":"` + dc["userCode"].(string) + `"}`})
	_, b := do(t, ts, req{method: "POST", path: "/api/v1/device/token", body: `{"deviceCode":"` + dc["deviceCode"].(string) + `"}`})
	tok := b["token"].(string)

	_, app := do(t, ts, req{method: "GET", path: "/api/v1/apps/com.ophymx.atvloader", headers: bearer(tok)})
	latest := app["latest"].(map[string]any)
	sum, _ := latest["sha256"].(string)
	if len(sum) != 64 || latest["signer"] == nil {
		t.Fatalf("latest lacks sha256/signer: %v", latest)
	}
	hr, _ := http.NewRequest("GET", ts.URL+latest["downloadUrl"].(string), nil)
	hr.Header.Set("Authorization", "Bearer "+tok)
	resp, err := ts.Client().Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("X-SHA256") != sum {
		t.Fatalf("download: %d X-SHA256=%q want %q", resp.StatusCode, resp.Header.Get("X-SHA256"), sum)
	}
}
