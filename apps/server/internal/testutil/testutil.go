// Package testutil holds the helpers the tests of the server packages share: a bootstrapped PocketBase
// application with the real schema, and fake downloader scripts.
package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	// The schema: importing it registers the migrations.
	_ "github.com/KevinBonnoron/viyu/apps/server/internal/migrations"
)

// NewApp returns a bootstrapped application on a temporary data directory with the real schema applied.
// It is shut down when the test ends.
func NewApp(t testing.TB) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	return app
}

// Script writes an executable shell script named name (a fake yt-dlp, say) in a new temporary directory
// and returns its path. The body runs under /bin/sh.
func Script(t testing.TB, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
