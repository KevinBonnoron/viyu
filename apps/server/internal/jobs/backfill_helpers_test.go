package jobs

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

const (
	// alpha is a platform every account uses by default; beta is one nobody turned on.
	alpha = "youtube"
	beta  = "twitch"

	fixtureAvatar = "https://yt3.googleusercontent.com/fixture-avatar=s0"
)

var fakeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// fakePlatform implements what the workers call; the rest of the interface stays unimplemented and panics
// if a worker ever reaches for it.
type fakePlatform struct {
	platform.Platform
	id             string
	defaultEnabled bool

	// listing and metadata answer the lookups; nil means the lookup fails.
	listing  func(ctx context.Context, id string, limit int) (platform.Channel, []platform.Video, error)
	metadata func(ctx context.Context, id string) (platform.Video, error)
	enrich   func(videos []platform.Video)
}

func (p *fakePlatform) ID() string                  { return p.id }
func (p *fakePlatform) DefaultEnabled() bool        { return p.defaultEnabled }
func (p *fakePlatform) SingleTitle() bool           { return true }
func (p *fakePlatform) ValidVideoID(id string) bool { return fakeID.MatchString(id) }
func (p *fakePlatform) ValidChannelID(id string) bool {
	return fakeID.MatchString(strings.TrimPrefix(id, "@"))
}
func (p *fakePlatform) ChannelIDAliases(id string) []string { return []string{id} }

func (p *fakePlatform) ChannelListing(ctx context.Context, id string, limit int, _ string) (platform.Channel, []platform.Video, error) {
	if p.listing == nil {
		return platform.Channel{}, nil, context.DeadlineExceeded
	}
	return p.listing(ctx, id, limit)
}

func (p *fakePlatform) VideoMetadata(ctx context.Context, id, _ string) (platform.Video, error) {
	if p.metadata == nil {
		return platform.Video{}, context.DeadlineExceeded
	}
	return p.metadata(ctx, id)
}

func (p *fakePlatform) EnrichListing(_ context.Context, _ string, videos []platform.Video) {
	if p.enrich != nil {
		p.enrich(videos)
	}
}

// world is an application with the real schema, a catalog store, a sources service and two fake platforms.
type world struct {
	app      core.App
	alpha    *fakePlatform
	beta     *fakePlatform
	registry *platform.Registry
	store    *catalog.Store
	sources  *sources.Service
	profiles *Signal // woken when the store leaves a channel without a profile
	dates    *Signal // woken when the store leaves a video undated
	counter  atomic.Int32
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		app:      testutil.NewApp(t),
		alpha:    &fakePlatform{id: alpha, defaultEnabled: true},
		beta:     &fakePlatform{id: beta},
		profiles: NewSignal(),
		dates:    NewSignal(),
	}
	catalog.RegisterHooks(w.app)
	w.registry = platform.NewRegistry(w.alpha, w.beta)
	w.store = catalog.New(w.app, w.registry, catalog.Options{OnProfilesWanted: w.profiles.Wake, OnPublicationsWanted: w.dates.Wake})
	svc, err := sources.New(w.app, w.registry, ytdlp.New(ytdlp.Options{CookieDir: t.TempDir()}), sources.Options{KeyFile: filepath.Join(t.TempDir(), "key")})
	if err != nil {
		t.Fatal(err)
	}
	w.sources = svc
	return w
}

func (w *world) record(t *testing.T, collection string, values map[string]any) *core.Record {
	t.Helper()
	schema, err := w.app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := w.app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// user creates an account.
func (w *world) user(t *testing.T) *core.Record {
	t.Helper()
	n := w.counter.Add(1)
	collection, err := w.app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail("user" + string(rune('a'+n)) + "@example.test")
	record.SetPassword("jobs-fixture-password-123")
	record.Set("role", "user")
	if err := w.app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// subscribe subscribes a new profile to the channel record.
func (w *world) subscribe(t *testing.T, channel *core.Record) {
	t.Helper()
	profile := w.record(t, "profiles", map[string]any{"user": w.user(t).Id, "name": "Profile"})
	w.record(t, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id})
}

// channel stores a channel and returns its record.
func (w *world) channel(t *testing.T, platformID, id, name, thumbnail string) *core.Record {
	t.Helper()
	record, err := w.store.UpsertChannel(platformID, id, name, thumbnail, "")
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// video stores a video through the catalog and returns its record.
func (w *world) video(t *testing.T, v platform.Video) *core.Record {
	t.Helper()
	if err := w.store.StoreVideo(&v, ""); err != nil {
		t.Fatal(err)
	}
	record, err := w.store.FindVideo(v.Platform, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func (w *world) findChannel(t *testing.T, platformID, id string) *core.Record {
	t.Helper()
	record, err := w.store.FindChannel(platformID, id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// expire makes the channel's listing expired.
func (w *world) expire(t *testing.T, platformID, id string) {
	t.Helper()
	record := w.findChannel(t, platformID, id)
	record.Set("expires_at", time.Now().Add(-time.Hour))
	if err := w.app.Save(record); err != nil {
		t.Fatal(err)
	}
}

// ids lists the native ids of records.
func ids(records []*core.Record) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.GetString("platform_id")
	}
	return out
}

// receive waits for a value or fails the test.
func receive[T any](t *testing.T, c <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}
