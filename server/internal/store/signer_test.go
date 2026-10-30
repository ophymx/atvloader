package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The loader APK is signed with whatever key the build had: the release key
// locally, the debug key in CI. Its fingerprint must be well formed and its
// subject one of the two. Better xCloud (BXC_APK, local only) is pinned to
// the fingerprint `apksigner verify --print-certs` prints.
func TestAPKSigner(t *testing.T) {
	ran := false
	if _, err := os.Stat("../../self/atvloader.apk"); err == nil {
		ran = true
		s, err := apkSigner("../../self/atvloader.apk")
		if err != nil || s == nil {
			t.Fatalf("loader apk: signer=%v err=%v", s, err)
		}
		if len(s.SHA256) != 64 || strings.Trim(s.SHA256, "0123456789abcdef") != "" {
			t.Errorf("loader apk: fingerprint %q is not 32 bytes of lowercase hex", s.SHA256)
		}
		if !strings.Contains(s.Name, "CN=ATV Loader") && !strings.Contains(s.Name, "CN=Android Debug") {
			t.Errorf("loader apk: unexpected signer %q", s.Name)
		}
		t.Logf("atvloader.apk: %s %q", s.SHA256, s.Name)
	}
	if p := os.Getenv("BXC_APK"); p != "" {
		ran = true
		s, err := apkSigner(p)
		if err != nil || s == nil {
			t.Fatalf("%s: signer=%v err=%v", p, s, err)
		}
		const want = "256fa8b7f9d1920b3e3688ff0157176593cfb329488168b82c26fcf74ec125da"
		if s.SHA256 != want || s.Name != "CN=redphx" {
			t.Errorf("%s: got %s %q, want %s CN=redphx", filepath.Base(p), s.SHA256, s.Name, want)
		}
	}
	if !ran {
		t.Skip("no APKs available")
	}
}

// Labels, as aapt2 dump badging prints them. Better xCloud's label is a
// string that aliases another string, which androidbinary alone cannot read.
func TestAPKLabel(t *testing.T) {
	cases := []struct{ path, label string }{
		{"../../self/atvloader.apk", "ATV Loader"},
		{os.Getenv("BXC_APK"), "Better xCloud"},
	}
	ran := false
	for _, c := range cases {
		if c.path == "" {
			continue
		}
		if _, err := os.Stat(c.path); err != nil {
			continue
		}
		ran = true
		m, err := parseAPK(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if m.Label != c.label {
			t.Errorf("%s: label %q, want %q", filepath.Base(c.path), m.Label, c.label)
		}
	}
	if !ran {
		t.Skip("no APKs available")
	}
}
