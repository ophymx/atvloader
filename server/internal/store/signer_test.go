package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Fingerprints as printed by `apksigner verify --print-certs`.
func TestAPKSigner(t *testing.T) {
	cases := []struct {
		path, sha256 string
	}{
		{"../../self/atvloader.apk", "bb5cb3d3cb4ec006ee26692d4aa2a72d79fa67c25b5d0d0216727c2e75fcea9b"},
		{os.Getenv("BXC_APK"), "256fa8b7f9d1920b3e3688ff0157176593cfb329488168b82c26fcf74ec125da"},
	}
	ran := false
	for _, c := range cases {
		if c.path == "" {
			continue
		}
		if _, err := os.Stat(c.path); err != nil {
			t.Logf("skip %s: %v", c.path, err)
			continue
		}
		ran = true
		s, err := apkSigner(c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if s == nil {
			t.Fatalf("%s: no signer found", c.path)
		}
		if s.SHA256 != c.sha256 {
			t.Errorf("%s: fingerprint %s, want %s", filepath.Base(c.path), s.SHA256, c.sha256)
		}
		t.Logf("%s: %s %q", filepath.Base(c.path), s.SHA256, s.Name)
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
