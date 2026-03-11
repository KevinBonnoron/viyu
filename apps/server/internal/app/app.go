// Package app composes the server: it builds the PocketBase application, wires every service to its
// collaborators, registers the HTTP routes and the PocketBase hooks, and owns the background work through
// one jobs.Group that Shutdown stops. It holds no business rule.
package app

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/plugins/migratecmd"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/hook"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	_ "github.com/KevinBonnoron/viyu/apps/server/internal/migrations"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ui"
)

// New builds the application. Nothing starts until it serves: the services are composed (and the
// background work started) when the server starts, so commands such as `migrate` or `superuser` never touch
// the downloader, the cookie directory or the source key.
func New(opts Options) *pocketbase.PocketBase {
	if opts.DataDir == "" {
		opts.DataDir = "db/pb_data"
	}
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: opts.DataDir})
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{Automigrate: false, Dir: "migrations", TemplateLang: migratecmd.TemplateLangGo})
	accounts.RegisterAdminRoles(app)
	accounts.RegisterHooks(app)
	catalog.RegisterHooks(app)
	subscriptions.RegisterHooks(app)
	app.OnBootstrap().BindFunc(bootstrapSuperuser)

	var running composition
	var server *http.Server
	var shutdownOnce sync.Once
	app.OnRecordAfterCreateSuccess("subscriptions").BindFunc(func(event *core.RecordEvent) error {
		if err := event.Next(); err != nil {
			return err
		}
		running.subscribed(event.App, event.Record)
		return nil
	})
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		server = se.Server
		server.WriteTimeout = 0
		server.ReadHeaderTimeout = 30 * time.Second
		server.IdleTimeout = 2 * time.Minute
		se.InstallerFunc = printInstallerLink
		if err := running.start(se, opts); err != nil {
			return err
		}
		se.Router.GET("/{path...}", apis.Static(uiFS(opts.UIDir), true)).BindFunc(ui.SecurityHeaders)
		return se.Next()
	})
	// Run before PocketBase's graceful server shutdown. Stopping our own group cancels yt-dlp and every
	// stream even if a client stays connected indefinitely.
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{Priority: -10000, Func: func(te *core.TerminateEvent) error {
		shutdownOnce.Do(running.stop)
		return te.Next()
	}})
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{Priority: -9998, Func: func(te *core.TerminateEvent) error {
		// Run after PocketBase's graceful listener shutdown, before database reset.
		if server != nil {
			_ = server.Close()
		}
		return te.Next()
	}})
	return app
}

// bootstrapSuperuser creates the dashboard superuser from the environment when there is none yet.
func bootstrapSuperuser(e *core.BootstrapEvent) error {
	if err := e.Next(); err != nil {
		return err
	}
	email := os.Getenv("PB_SUPERUSER_EMAIL")
	password := os.Getenv("PB_SUPERUSER_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	count, err := e.App.CountRecords(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	collection, err := e.App.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword(password)
	return e.App.Save(record)
}

// Serve bootstraps, migrates and serves on addr.
func Serve(app *pocketbase.PocketBase, addr string) error {
	if err := app.Bootstrap(); err != nil {
		return err
	}
	if err := app.RunAllMigrations(); err != nil {
		return err
	}
	return apis.Serve(app, apis.ServeConfig{HttpAddr: addr, ShowStartBanner: false})
}

// Shutdown stops the background work and resets the application.
func Shutdown(app *pocketbase.PocketBase) {
	_ = app.OnTerminate().Trigger(&core.TerminateEvent{App: app}, func(te *core.TerminateEvent) error {
		return te.App.ResetBootstrapState()
	})
}

func uiFS(dir string) fs.FS {
	if dir != "" {
		return os.DirFS(dir)
	}
	return ui.FS()
}

func printInstallerLink(app core.App, superuser *core.Record, baseURL string) error {
	token, err := superuser.NewStaticAuthToken(30 * time.Minute)
	if err != nil {
		return err
	}
	log.Printf("No dashboard superuser yet. Create one at %s/_/#/pbinstall/%s or run: viyu superuser upsert EMAIL PASS", strings.TrimRight(baseURL, "/"), token)
	return nil
}

// janitor periodically drops what expired in the in-memory caches nobody is asking for any more. The
// lookups themselves already ignore expired entries; this bounds memory for entries never looked up again.
func janitor(ctx context.Context, every time.Duration, purges ...func()) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, purge := range purges {
				purge()
			}
		}
	}
}
