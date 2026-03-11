package ui

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
