package subscriptions

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// The fake platforms: any identifier of letters and digits is a valid channel, except on "strict", which
// only accepts the ones that start with "ok-".
const (
	alpha  = "youtube"
	beta   = "twitch"
	strict = "peertube"
)

type fakePlatform struct {
	platform.Platform
	id string
}

func (p fakePlatform) ID() string           { return p.id }
func (p fakePlatform) DefaultEnabled() bool { return true }
func (p fakePlatform) ValidChannelID(id string) bool {
	if p.id == strict {
		return strings.HasPrefix(id, "ok-")
	}
	return id != ""
}

// fixture is a service on an application with the real schema.
type fixture struct {
	app     core.App
	service *Service
	sources *sources.Service
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	app := testutil.NewApp(t)
	registry := platform.NewRegistry(fakePlatform{id: alpha}, fakePlatform{id: beta}, fakePlatform{id: strict})
	runner := ytdlp.New(ytdlp.Options{CookieDir: filepath.Join(t.TempDir(), "cookies")})
	src, err := sources.New(app, registry, runner, sources.Options{KeyFile: filepath.Join(t.TempDir(), "source.key")})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{app: app, service: New(app, catalog.New(app, registry, catalog.Options{}), src, registry), sources: src}
}

func (f *fixture) record(t testing.TB, collection string, values map[string]any) *core.Record {
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

func (f *fixture) user(t testing.TB, email string) *core.Record {
	t.Helper()
	collection, err := f.app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("subscriptions-fixture-password-123")
	record.Set("role", "user")
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func (f *fixture) profile(t testing.TB, user *core.Record, name string) *core.Record {
	t.Helper()
	return f.record(t, profilesCollection, map[string]any{"user": user.Id, "name": name})
}

func (f *fixture) channel(t testing.TB, platformID, id, name string) *core.Record {
	t.Helper()
	return f.record(t, channelsCollection, map[string]any{"platform": platformID, "platform_id": id, "name": name, "expires_at": "2030-01-01 00:00:00.000Z"})
}

// subscribe stores a subscription and its channel directly, bypassing the service.
func (f *fixture) subscribe(t testing.TB, profile *core.Record, platformID, id, name string) *core.Record {
	t.Helper()
	channel := f.channel(t, platformID, id, name)
	return f.record(t, subscriptionsCollection, map[string]any{"profile": profile.Id, "channel": channel.Id})
}

func (f *fixture) creatorName(t testing.TB, id string) string {
	t.Helper()
	record, err := f.app.FindRecordById(creatorsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("name")
}

func (f *fixture) creator(t testing.TB, id string) string {
	t.Helper()
	record, err := f.app.FindRecordById(subscriptionsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("creator")
}

func (f *fixture) ungrouped(t testing.TB, id string) bool {
	t.Helper()
	record, err := f.app.FindRecordById(subscriptionsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetBool("ungrouped")
}

// setUngrouped marks a subscription as kept apart, as Ungroup does.
func (f *fixture) setUngrouped(t testing.TB, subscription *core.Record) {
	t.Helper()
	subscription.Set("ungrouped", true)
	if err := f.app.Save(subscription); err != nil {
		t.Fatal(err)
	}
}

// link stores a creator of the profile and puts the subscriptions in it, bypassing the service.
func (f *fixture) link(t testing.TB, profile *core.Record, name string, subscriptions ...*core.Record) string {
	t.Helper()
	creator := f.record(t, creatorsCollection, map[string]any{"profile": profile.Id, "name": name})
	for _, subscription := range subscriptions {
		subscription.Set("creator", creator.Id)
		if err := f.app.Save(subscription); err != nil {
			t.Fatal(err)
		}
	}
	return creator.Id
}

func (f *fixture) channelOf(t testing.TB, subscription *core.Record) *core.Record {
	t.Helper()
	channel, err := f.app.FindRecordById(channelsCollection, subscription.GetString("channel"))
	if err != nil {
		t.Fatal(err)
	}
	return channel
}

func (f *fixture) count(t testing.TB, collection string, exprs ...dbx.Expression) int {
	t.Helper()
	records, err := f.app.FindAllRecords(collection, exprs...)
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

// requireCode fails unless err is an *apierr.Error with the status and, when set, the code.
func requireCode(t testing.TB, err error, status int, code string) {
	t.Helper()
	var safe *apierr.Error
	if !errors.As(err, &safe) {
		t.Fatalf("error %v is not an apierr.Error", err)
	}
	if safe.Status != status || (code != "" && safe.Code != code) {
		t.Fatalf("error %d %s %q, want %d %s", safe.Status, safe.Code, safe.Message, status, code)
	}
}

// countQueries counts the queries fn runs that read the table.
func countQueries(app core.App, table string, fn func()) int {
	var count atomic.Int32
	for _, builder := range []dbx.Builder{app.ConcurrentDB(), app.NonconcurrentDB()} {
		db := builder.(*dbx.DB)
		previous := db.QueryLogFunc
		db.QueryLogFunc = func(_ context.Context, _ time.Duration, query string, _ *sql.Rows, _ error) {
			if strings.Contains(query, "FROM `"+table+"`") {
				count.Add(1)
			}
		}
		defer func() { db.QueryLogFunc = previous }()
	}
	fn()
	return int(count.Load())
}
