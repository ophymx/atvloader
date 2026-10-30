package store

import (
	"errors"
	"testing"
)

// A drawable that references itself or forms a cycle used to recurse until
// the goroutine stack overflowed, a fatal error that crashes the whole
// process. The render chain now stops at maxDrawableDepth. The guard sits
// at the top of both recursive entry points, before any resource access, so
// it can be checked without a real APK.
func TestRenderDepthGuard(t *testing.T) {
	r := &apkRes{}
	if _, err := r.renderNode(&xmlNode{Name: "layer-list"}, 64, maxDrawableDepth+1); !errors.Is(err, errDrawableTooDeep) {
		t.Fatalf("renderNode past max depth: got %v, want errDrawableTooDeep", err)
	}
	if _, err := r.renderDrawableFile("a.xml", 64, maxDrawableDepth+1); !errors.Is(err, errDrawableTooDeep) {
		t.Fatalf("renderDrawableFile past max depth: got %v, want errDrawableTooDeep", err)
	}
}

func TestValidPackage(t *testing.T) {
	good := []string{
		"com.ophymx.atvloader",
		"a.b",
		"com.example.app_name",
		"org.smarttube.stable",
		"com.b2.c3",
	}
	bad := []string{
		"",
		"nodot",
		"../../../../pwn/aaaa",
		"com..example",
		".com.example",
		"com.example.",
		"1com.example",
		"com.1example",
		"com.exa mple",
		"com/example",
	}
	for _, p := range good {
		if !validPackage.MatchString(p) {
			t.Errorf("valid package rejected: %q", p)
		}
	}
	for _, p := range bad {
		if validPackage.MatchString(p) {
			t.Errorf("invalid package accepted: %q", p)
		}
	}
}
