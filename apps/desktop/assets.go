// The native client embeds only the remote web interface.
package main

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func FS() fs.FS {
	assets, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return assets
}
