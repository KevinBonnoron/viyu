package app

import (
	"slices"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// The configured superuser is created once: a password changed in the dashboard survives a restart with
// stale deployment secrets. It reads the process environment, so it cannot run in parallel.
func TestSuperuserEnvironmentBootstrapPreservesExistingAccount(t *testing.T) {
	t.Setenv("PB_SUPERUSER_EMAIL", "admin@example.test")
	t.Setenv("PB_SUPERUSER_PASSWORD", "initial-superuser-password")
	dir := t.TempDir()
	app := New(Options{DataDir: dir})
	t.Cleanup(func() { Shutdown(app) })
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@example.test")
	if err != nil || !record.ValidatePassword("initial-superuser-password") {
		t.Fatalf("configured superuser cannot authenticate: %v", err)
	}
	id := record.Id
	record.SetPassword("dashboard-updated-password")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	Shutdown(app)

	t.Setenv("PB_SUPERUSER_EMAIL", "replacement@example.test")
	t.Setenv("PB_SUPERUSER_PASSWORD", "replacement-superuser-password")
	app = New(Options{DataDir: dir})
	t.Cleanup(func() { Shutdown(app) })
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	record, err = app.FindRecordById(core.CollectionNameSuperusers, id)
	if err != nil || record.Email() != "admin@example.test" || !record.ValidatePassword("dashboard-updated-password") {
		t.Fatalf("restart changed the existing superuser: %v", err)
	}
	count, err := app.CountRecords(core.CollectionNameSuperusers)
	if err != nil || count != 1 {
		t.Fatalf("restart created another superuser: count=%d err=%v", count, err)
	}
}

func TestSuperuserEnvironmentBootstrapConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		email    string
		password string
		wantErr  bool
	}{
		{name: "manual setup without variables"},
		{name: "email alone leaves manual setup", email: "admin@example.test"},
		{name: "password alone leaves manual setup", password: "unused-superuser-password"},
		{name: "invalid email fails startup", email: "not-an-email", password: "valid-superuser-password", wantErr: true},
		{name: "invalid password fails startup", email: "admin@example.test", password: "short", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PB_SUPERUSER_EMAIL", tc.email)
			t.Setenv("PB_SUPERUSER_PASSWORD", tc.password)
			app := New(Options{DataDir: t.TempDir()})
			t.Cleanup(func() { Shutdown(app) })
			err := app.Bootstrap()
			if (err != nil) != tc.wantErr {
				t.Fatalf("startup error=%v, want error=%v", err, tc.wantErr)
			}
			if err != nil && tc.password != "" && strings.Contains(err.Error(), tc.password) {
				t.Fatal("startup error exposes the configured password")
			}
			count, countErr := app.CountRecords(core.CollectionNameSuperusers)
			if countErr != nil || count != 0 {
				t.Fatalf("incomplete or invalid configuration persisted a superuser: count=%d err=%v", count, countErr)
			}
		})
	}
}

// The production bootstrap keeps PocketBase's rate limiter on; newApp switches it off for the other suites.
func TestProductionBootstrapEnablesRateLimits(t *testing.T) {
	t.Parallel()
	app := New(Options{DataDir: t.TempDir(), DisableChannelProfiles: true})
	t.Cleanup(func() { Shutdown(app) })
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	limits := app.Settings().RateLimits
	if !limits.Enabled {
		t.Fatal("rate limits are disabled after the production bootstrap")
	}
	for _, label := range []string{"*:auth", "*:create", "/api/stream/", "/api/"} {
		if !slices.ContainsFunc(limits.Rules, func(rule core.RateLimitRule) bool { return rule.Label == label }) {
			t.Errorf("no rate limit rule %q: %+v", label, limits.Rules)
		}
	}
}
