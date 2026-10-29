package store

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestParseAPKIcons parses every APK under testdata (none are committed;
// drop some in to exercise icon extraction) and, when ICON_OUT is set,
// writes each icon there for eyeballing.
func TestParseAPKIcons(t *testing.T) {
	apks, _ := filepath.Glob("testdata/*.apk")
	if extra := os.Getenv("ICON_APKS"); extra != "" {
		apks = append(apks, filepath.SplitList(extra)...)
	}
	if len(apks) == 0 {
		t.Skip("no APKs to test; set ICON_APKS=a.apk:b.apk")
	}
	for _, p := range apks {
		meta, err := parseAPK(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if meta.Icon == nil {
			t.Errorf("%s (%s): no icon", p, meta.Package)
			continue
		}
		b := meta.Icon.Bounds()
		t.Logf("%s: %s %s icon %dx%d", filepath.Base(p), meta.Package, meta.VersionName, b.Dx(), b.Dy())
		if out := os.Getenv("ICON_OUT"); out != "" {
			f, err := os.Create(filepath.Join(out, meta.Package+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, meta.Icon); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
