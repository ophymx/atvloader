package store

import (
	"errors"
	"os"
	"testing"
)

// Re-importing an APK whose signer differs from what is stored must fail
// unless the caller opts in. The loader APK is the fixture; a different key
// is simulated by rewriting the stored fingerprint.
func TestImportRefusesSignerChange(t *testing.T) {
	const apk = "../../self/atvloader.apk"
	if _, err := os.Stat(apk); err != nil {
		t.Skip("no loader APK built")
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	open := func() *os.File {
		f, err := os.Open(apk)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	f := open()
	res, err := s.ImportAPK(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.Version.Signer == nil || res.Version.Signer.SHA256 == "" {
		t.Fatalf("signer not recorded: %+v", res.Version)
	}
	pkg := res.App.Package

	// Same APK again: same signer, accepted (replaces the version).
	f = open()
	if _, err := s.ImportAPK(f); err != nil {
		t.Fatalf("re-import with same signer: %v", err)
	}
	f.Close()

	// Pretend the stored version came from another key.
	s.mu.Lock()
	s.idx.Apps[pkg].Versions[0].Signer = &Signer{SHA256: "00ff00ff00ff00ff00ff00ff", Name: "CN=someone else"}
	s.mu.Unlock()

	f = open()
	_, err = s.ImportAPK(f)
	f.Close()
	if !errors.Is(err, ErrSignerMismatch) {
		t.Fatalf("want ErrSignerMismatch, got %v", err)
	}
	if got := s.List(); len(got) != 1 || len(got[0].Versions) != 1 || got[0].Versions[0].Signer.Name != "CN=someone else" {
		t.Fatalf("store changed on refused import: %+v", got)
	}

	f = open()
	res, err = s.ImportAPK(f, ImportOptions{AllowSignerChange: true})
	f.Close()
	if err != nil {
		t.Fatalf("AllowSignerChange: %v", err)
	}
	if res.Version.Signer.Name == "CN=someone else" {
		t.Fatalf("new signer not recorded")
	}
}
