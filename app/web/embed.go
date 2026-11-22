// Package web embeds the compiled Pierre WebView frontend assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// Assets returns the embedded frontend filesystem rooted at dist/.
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
