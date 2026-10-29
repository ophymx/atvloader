// Package self carries the ATV Loader APK embedded into the server binary.
package self

import (
	"embed"
	"io/fs"
)

//go:embed *
var files embed.FS

// APK returns the embedded loader APK, or nil if none was bundled at build time.
func APK() (fs.File, string, bool) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, "", false
	}
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) < 4 || e.Name()[len(e.Name())-4:] != ".apk" {
			continue
		}
		f, err := files.Open(e.Name())
		if err != nil {
			continue
		}
		return f, e.Name(), true
	}
	return nil, "", false
}
