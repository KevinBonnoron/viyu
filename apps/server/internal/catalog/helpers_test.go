package catalog

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

// The fake platforms: "alpha" stores channels under a handle with or without its "@" and may have
// translated titles; "solo" has a single title only.
const (
	alpha = "youtube"
	solo  = "twitch"
)

var fakeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

type fakePlatform struct {
	platform.Platform // the methods a store never calls stay unimplemented
	id                string
	single            bool
}

func (p fakePlatform) ID() string                  { return p.id }
func (p fakePlatform) SingleTitle() bool           { return p.single }
func (p fakePlatform) ValidVideoID(id string) bool { return fakeID.MatchString(id) }
func (p fakePlatform) ValidChannelID(id string) bool {
	return fakeID.MatchString(strings.TrimPrefix(id, "@"))
}
func (p fakePlatform) ChannelIDAliases(id string) []string {
	if p.single {
		return []string{id}
	}
	bare := strings.TrimPrefix(id, "@")
	return []string{bare, "@" + bare}
}

// wakes counts the wake-ups a store asked for.
type wakes struct{ profiles, publications atomic.Int32 }

func newStore(t testing.TB) (*Store, core.App, *wakes) {
	t.Helper()
	app := testutil.NewApp(t)
	RegisterHooks(app)
	w := &wakes{}
	store := New(app, platform.NewRegistry(fakePlatform{id: alpha}, fakePlatform{id: solo, single: true}), Options{
		OnProfilesWanted:     func() { w.profiles.Add(1) },
		OnPublicationsWanted: func() { w.publications.Add(1) },
	})
	return store, app, w
}

func testUser(t testing.TB, app core.App, email string) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("catalog-fixture-password-123")
	record.Set("role", "user")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func testProfile(t testing.TB, app core.App, user *core.Record, name string) *core.Record {
	t.Helper()
	return testRecord(t, app, "profiles", map[string]any{"user": user.Id, "name": name})
}

func testRecord(t testing.TB, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	schema, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// listing is a flat listing of a video of a channel of the same name pattern.
func listing(platformID, id string) platform.Video {
	return platform.Video{ID: id, Platform: platformID, ChannelID: "UC" + id, ChannelName: id, Title: id}
}

func mustStore(t testing.TB, s *Store, v platform.Video, ownerID string) platform.Video {
	t.Helper()
	if err := s.StoreVideo(&v, ownerID); err != nil {
		t.Fatal(err)
	}
	return v
}

func mustFindVideo(t testing.TB, s *Store, platformID, id string) *core.Record {
	t.Helper()
	record, err := s.FindVideo(platformID, id)
	if err != nil {
		t.Fatalf("video %s: %v", id, err)
	}
	return record
}

func mustFindChannel(t testing.TB, s *Store, platformID, id string) *core.Record {
	t.Helper()
	record, err := s.FindChannel(platformID, id)
	if err != nil {
		t.Fatalf("channel %s: %v", id, err)
	}
	return record
}

// countChannelQueries counts the queries fn runs that read the channels table.
func countChannelQueries(app core.App, fn func()) int {
	var count atomic.Int32
	for _, builder := range []dbx.Builder{app.ConcurrentDB(), app.NonconcurrentDB()} {
		db := builder.(*dbx.DB)
		previous := db.QueryLogFunc
		db.QueryLogFunc = func(_ context.Context, _ time.Duration, query string, _ *sql.Rows, _ error) {
			if strings.Contains(query, "FROM `channels`") {
				count.Add(1)
			}
		}
		defer func() { db.QueryLogFunc = previous }()
	}
	fn()
	return int(count.Load())
}
