// Package web embeds the admin UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed admin landing.html
var files embed.FS

// Landing is the public front page template.
func Landing() []byte {
	b, err := files.ReadFile("landing.html")
	if err != nil {
		panic(err)
	}
	return b
}

// Admin returns the admin UI file tree rooted at its index.html.
func Admin() fs.FS {
	sub, err := fs.Sub(files, "admin")
	if err != nil {
		panic(err)
	}
	return sub
}
