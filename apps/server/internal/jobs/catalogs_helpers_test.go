package jobs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// gamma is a platform that cannot list a catalog (Twitch, PeerTube…).
const gamma = "peertube"

// syncPlatform is a fakePlatform with the capabilities catalog synchronization looks at: canonical channel
// ids like a handle with and without its "@", an optional connector and an optional catalog syncer.
type syncPlatform struct {
	*fakePlatform
	syncer    platform.CatalogSyncer
	connector platform.Connector
}

func (p *syncPlatform) CatalogSync() platform.CatalogSyncer { return p.syncer }
func (p *syncPlatform) Connector() platform.Connector       { return p.connector }
func (p *syncPlatform) Descriptor() platform.Descriptor {
	return platform.Descriptor{Platform: p.id, Name: p.id}
}
func (p *syncPlatform) ChannelIDAliases(id string) []string {
	canonical := strings.TrimPrefix(id, "@")
	return []string{canonical, "@" + canonical}
}

// scriptedCatalog is a CatalogSyncer whose listing a test scripts. call counts the runs from 1. A test that
// sets collections scripts the collections of a channel too; by default a channel has none.
type scriptedCatalog struct {
	calls       atomic.Int32
	run         func(ctx context.Context, call int, channel platform.Channel, cookieFile string, visit func(platform.Video) error) error
	collections func(ctx context.Context, channel platform.Channel) ([]platform.Collection, error)
}

func (s *scriptedCatalog) Catalog(ctx context.Context, channel platform.Channel, cookieFile string, visit func(platform.Video) error) error {
	return s.run(ctx, int(s.calls.Add(1)), channel, cookieFile, visit)
}

func (s *scriptedCatalog) Collections(ctx context.Context, channel platform.Channel) ([]platform.Collection, error) {
	if s.collections == nil {
		return nil, nil
	}
	return s.collections(ctx, channel)
}

// fakeConnector accepts every session: the cookies are whatever the test says.
type fakeConnector struct{}

func (fakeConnector) NormalizeCookies(raw string) (string, error)       { return raw, nil }
func (fakeConnector) Validate(context.Context, string) error            { return nil }
func (fakeConnector) MarkWatched(context.Context, string, string) error { return nil }
func (fakeConnector) Feed(context.Context, string, string, int, int) (platform.Feed, error) {
	return platform.Feed{}, nil
}

// clock is the wall clock plus an offset a test moves: queues use their clock to end a pause, so it has to
// keep running.
type clock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.offset += d
	c.mu.Unlock()
}

// syncWorld is an application with the real schema, a platform that lists catalogs (alpha, connectable) and
// one that cannot (gamma), a running Group, and a clock.
type syncWorld struct {
	*world
	group  *Group
	list   *syncPlatform // alpha
	noList *syncPlatform // gamma
	syncer *scriptedCatalog
	clock  *clock
}

// fixtureVideos lists n videos of a channel, newest first, named "<prefix>-<i>" with the title "Video <i>".
func fixtureVideos(channel, prefix string, n int) []platform.Video {
	videos := make([]platform.Video, n)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range videos {
		videos[i] = platform.Video{
			ID: fmt.Sprintf("%s-%d", prefix, i+1), Platform: alpha, ChannelID: channel, ChannelName: "Channel " + channel,
			Title: fmt.Sprintf("Video %d", i+1), Published: base.AddDate(0, 0, n-i).Format(platform.DayLayout),
			Thumbnail: fmt.Sprintf("https://yt3.googleusercontent.com/%s-%d=s0", prefix, i+1),
		}
	}
	return videos
}

func newSyncWorld(t *testing.T) *syncWorld {
	t.Helper()
	app := testutil.NewApp(t)
	catalog.RegisterHooks(app)
	syncer := &scriptedCatalog{run: func(context.Context, int, platform.Channel, string, func(platform.Video) error) error { return nil }}
	list := &syncPlatform{fakePlatform: &fakePlatform{id: alpha, defaultEnabled: true}, syncer: syncer, connector: fakeConnector{}}
	list.fakePlatform.listing = func(_ context.Context, id string, limit int) (platform.Channel, []platform.Video, error) {
		canonical := strings.TrimPrefix(id, "@")
		return platform.Channel{ID: canonical, Platform: alpha, Name: "Channel " + canonical}, fixtureVideos(canonical, canonical, limit), nil
	}
	noList := &syncPlatform{fakePlatform: &fakePlatform{id: gamma, defaultEnabled: true}}
	registry := platform.NewRegistry(list, noList)
	store := catalog.New(app, registry, catalog.Options{})
	service, err := sources.New(app, registry, ytdlp.New(ytdlp.Options{CookieDir: t.TempDir()}), sources.Options{KeyFile: filepath.Join(t.TempDir(), "key")})
	if err != nil {
		t.Fatal(err)
	}
	group := NewGroup()
	t.Cleanup(group.Stop)
	return &syncWorld{
		world:  &world{app: app, registry: registry, store: store, sources: service, profiles: NewSignal(), dates: NewSignal()},
		group:  group,
		list:   list,
		noList: noList,
		syncer: syncer,
		clock:  &clock{},
	}
}

func (w *syncWorld) catalogs(opts CatalogOptions) *Catalogs {
	opts.Now = w.clock.Now
	return NewCatalogs(w.app, w.registry, w.store, w.sources, w.group.Go, opts)
}

// wait blocks until the synchronization of key is finished.
func waitCatalog(t *testing.T, c *Catalogs, key CatalogKey) Entry[CatalogKey, struct{}] {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := c.queue.Wait(ctx, key)
	if err != nil {
		t.Fatalf("catalog %v did not finish: %v", key, err)
	}
	return entry
}

func publicKey(channel string) CatalogKey { return CatalogKey{Platform: alpha, ChannelID: channel} }

// waitUntil polls cond until it holds; the test fails when it does not within 20 seconds.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// poke makes a queue look at its clock again, the way its own timer would when it fires: tests move a fake
// clock past a backoff and cannot wait for a timer armed on the real one.
func poke[K comparable, In, Out any](q *Queue[K, In, Out]) {
	q.Forget(func(K) bool { return false })
}
