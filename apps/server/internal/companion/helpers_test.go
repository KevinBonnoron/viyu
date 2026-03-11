package companion

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/youtube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// now is the clock of every test: syncs are dated relative to it.
var now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// fixture is a service on an application with the real schema. The real YouTube platform validates the
// identifiers; it never reaches the network here.
type fixture struct {
	app   core.App
	store *catalog.Store
	subs  *subscriptions.Service
	svc   *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	app := testutil.NewApp(t)
	registry := platform.NewRegistry(youtube.New(youtube.Options{Runner: ytdlp.New(ytdlp.Options{Binary: "/nonexistent/yt-dlp"})}))
	runner := ytdlp.New(ytdlp.Options{CookieDir: filepath.Join(t.TempDir(), "cookies")})
	src, err := sources.New(app, registry, runner, sources.Options{KeyFile: filepath.Join(t.TempDir(), "source.key")})
	if err != nil {
		t.Fatal(err)
	}
	store := catalog.New(app, registry, catalog.Options{})
	subs := subscriptions.New(app, store, src, registry)
	svc := New(app, store, subs, src)
	svc.now = func() time.Time { return now }
	return &fixture{app: app, store: store, subs: subs, svc: svc}
}

func (f *fixture) record(t *testing.T, collection string, values map[string]any) *core.Record {
	t.Helper()
	schema, err := f.app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func (f *fixture) user(t *testing.T, email string) *core.Record {
	t.Helper()
	collection, err := f.app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("companion-fixture-password-123")
	record.Set("role", "user")
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func (f *fixture) profile(t *testing.T, user *core.Record, name string) *core.Record {
	t.Helper()
	return f.record(t, "profiles", map[string]any{"user": user.Id, "name": name})
}

// paired pairs a device to the profile through the service and returns its token and device.
func (f *fixture) paired(t *testing.T, user, profile *core.Record, name string) (string, *Device) {
	t.Helper()
	code, _, err := f.svc.CreatePairing(user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	token, device, err := f.svc.Pair(code, name)
	if err != nil {
		t.Fatal(err)
	}
	return token, device
}

// device is a paired device of a new account with one profile.
func (f *fixture) device(t *testing.T) (*Device, *core.Record, *core.Record) {
	t.Helper()
	user := f.user(t, "owner@example.test")
	profile := f.profile(t, user, "Main")
	_, device := f.paired(t, user, profile, "Chrome")
	return device, user, profile
}

// manualSubscription subscribes the profile the way a user does: not synced.
func (f *fixture) manualSubscription(t *testing.T, profile *core.Record, channelID string) *core.Record {
	t.Helper()
	channel := f.record(t, "channels", map[string]any{"platform": "youtube", "platform_id": channelID, "expires_at": "2030-01-01 00:00:00.000Z"})
	return f.record(t, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id})
}

// subscribed lists the platform ids of the channels the profile follows, with whether the sync made them.
func (f *fixture) subscribed(t *testing.T, profile *core.Record) map[string]bool {
	t.Helper()
	rows, err := f.app.FindRecordsByFilter("subscriptions", "profile = {:profile}", "", 0, 0, dbx.Params{"profile": profile.Id})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, row := range rows {
		channel, err := f.app.FindRecordById("channels", row.GetString("channel"))
		if err != nil {
			t.Fatal(err)
		}
		found[channel.GetString("platform_id")] = row.GetBool("synced")
	}
	return found
}

// history returns the profile's entry for a YouTube video, nil when it has none.
func (f *fixture) history(t *testing.T, profile *core.Record, videoID string) *core.Record {
	t.Helper()
	video, err := f.store.FindVideo("youtube", videoID)
	if err != nil {
		return nil
	}
	record, err := f.app.FindFirstRecordByFilter("watch_history", "profile = {:profile} && video = {:video}", dbx.Params{"profile": profile.Id, "video": video.Id})
	if err != nil {
		return nil
	}
	return record
}

// channelID and videoID build identifiers the YouTube platform accepts.
func channelID(n string) string { return "UC" + strings.Repeat("0", 22-len(n)) + n }
func videoID(n string) string   { return strings.Repeat("v", 11-len(n)) + n }

// requireCode fails unless err is an *apierr.Error with the status and code.
func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Status != status || safe.Code != code {
		t.Fatalf("error = %v, want %d %s", err, status, code)
	}
}
