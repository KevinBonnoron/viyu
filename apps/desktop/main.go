package main

import (
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// WebKitGTK's DMA-BUF renderer leaves the window blank (the web process never runs the page) on
	// Wayland with the Nix Mesa closure. The shared-memory path renders correctly; an explicit value in
	// the environment still wins.
	if _, set := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); !set {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	app := application.New(application.Options{
		Name:        "Viyu",
		Description: "Client for a remote Viyu server",
		Icon:        applicationIcon,
		Assets:      application.AssetOptions{Handler: singlePageHandler(FS())},
		Linux:       application.LinuxOptions{ProgramName: "viyu"},
	})
	app.SetIcon(applicationIcon)
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Viyu", Width: 1280, Height: 800,
		Linux: application.LinuxWindow{Icon: applicationIcon, WebviewGpuPolicy: application.WebviewGpuPolicyAlways},
	})
	return app.Run()
}

func singlePageHandler(assets fs.FS) http.Handler {
	files := application.AssetFileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "." && name != "index.html" {
			if _, err := fs.Stat(assets, name); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}
