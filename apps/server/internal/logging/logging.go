// Package logging gives the server its logger: PocketBase's own (kept in its logs store and shown in the
// dashboard) plus standard error for warnings and errors, so `docker logs` shows what went wrong without
// opening the dashboard. PocketBase itself only prints its logs to the console in --dev mode.
package logging

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

var stderr slog.Handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})

// Of is the logger of app. It wraps app.Logger() on each call (two small allocations, on paths that log
// anyway) because PocketBase replaces its logger whenever the app bootstraps again.
func Of(app core.App) *slog.Logger {
	base := app.Logger()
	if app.IsDev() {
		// PocketBase already prints every log to the console in dev mode.
		return base
	}
	return slog.New(tee{base.Handler(), stderr})
}

// tee sends a record to every handler that wants it.
type tee []slog.Handler

func (t tee) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t tee) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, h := range t {
		if h.Enabled(ctx, record.Level) {
			errs = append(errs, h.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (t tee) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(tee, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (t tee) WithGroup(name string) slog.Handler {
	out := make(tee, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}
