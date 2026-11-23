// Package testutil holds the helpers the tests of the server packages share: a bootstrapped PocketBase
// application with the real schema, and fake downloader scripts.
package testutil

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	// The schema: importing it registers the migrations.
	_ "github.com/KevinBonnoron/viyu/apps/server/internal/migrations"
)

var schemaFixture = sync.OnceValues(func() (map[string][]byte, error) {
	dir, err := os.MkdirTemp("", "viyu-schema-fixture-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	if err := app.Bootstrap(); err != nil {
		return nil, err
	}
	if err := app.RunAllMigrations(); err != nil {
		_ = app.ResetBootstrapState()
		return nil, err
	}
	if err := app.ResetBootstrapState(); err != nil {
		return nil, err
	}
	files := make(map[string][]byte, 2)
	for _, name := range []string{"data.db", "auxiliary.db"} {
		files[name], err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
	}
	return files, nil
})

// CopySchemaFixture copies a migrated, closed schema into a fresh data directory.
// Each caller gets independent databases; existing database files are never overwritten.
func CopySchemaFixture(t testing.TB, dir string) {
	t.Helper()
	files, err := schemaFixture()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

// NewApp returns a bootstrapped application on a temporary data directory with the real schema applied.
// It is shut down when the test ends.
func NewApp(t testing.TB) core.App {
	t.Helper()
	dir := t.TempDir()
	CopySchemaFixture(t, dir)
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
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
//
// The file is written under syscall.ForkLock: a parallel test that forks while the script is still open for
// writing would hand its child that descriptor until the child execs, and running the script meanwhile fails
// with "text file busy" (ETXTBSY).
func Script(t testing.TB, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	syscall.ForkLock.Lock()
	err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	syscall.ForkLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return path
}
