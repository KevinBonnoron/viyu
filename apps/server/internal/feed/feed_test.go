package feed

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

const (
	alpha = "youtube" // on by default
	beta  = "twitch"  // on by default, the one tests turn off
)

var fakeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// fakePlatform implements what storing and activating need. Every method that would reach the network
// reports a failure: a feed must be built from the cache alone.
type fakePlatform struct {
	platform.Platform
	t     *testing.T
	id    string
	calls *atomic.Int32
}

func (p fakePlatform) ID() string                  { return p.id }
func (p fakePlatform) DefaultEnabled() bool        { return true }
func (p fakePlatform) SingleTitle() bool           { return false }
func (p fakePlatform) ValidVideoID(id string) bool { return fakeID.MatchString(id) }
func (p fakePlatform) ValidChannelID(id string) bool {
	return fakeID.MatchString(id)
}
func (p fakePlatform) ChannelIDAliases(id string) []string { return []string{id} }

func (p fakePlatform) reached(what string) {
	p.calls.Add(1)
	p.t.Errorf("the feed reached platform %s: %s", p.id, what)
}

func (p fakePlatform) Search(context.Context, platform.Session, string, int) (platform.SearchPage, error) {
	p.reached("Search")
	return platform.SearchPage{}, nil
}

func (p fakePlatform) VideoMetadata(context.Context, string, string) (platform.Video, error) {
	p.reached("VideoMetadata")
	return platform.Video{}, nil
}

func (p fakePlatform) ChannelListing(context.Context, string, int, string) (platform.Channel, []platform.Video, error) {
	p.reached("ChannelListing")
	return platform.Channel{}, nil, nil
}

func (p fakePlatform) PlaybackManifest(context.Context, string, string) (platform.Manifest, error) {
	p.reached("PlaybackManifest")
	return platform.Manifest{}, nil
}

func (p fakePlatform) EnrichListing(context.Context, string, []platform.Video) {
	p.reached("EnrichListing")
}

func (p fakePlatform) LiveStream(context.Context, string) (platform.LiveStream, error) {
	p.reached("LiveStream")
	return platform.LiveStream{}, nil
}

// recordingRefresher remembers the channels it was asked to refresh, call by call.
type recordingRefresher struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingRefresher) Schedule(channels []*core.Record) {
	ids := make([]string, len(channels))
	for i, channel := range channels {
		ids[i] = channel.Id
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, ids)
}

func (r *recordingRefresher) scheduled() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

type fixture struct {
	t         *testing.T
	app       core.App
	store     *catalog.Store
	accounts  *sources.Service
	svc       *Service
	refresher *recordingRefresher
	platform  *atomic.Int32 // platform calls
	ytdlpRan  string        // a file the fake yt-dlp creates when it runs
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	app := testutil.NewApp(t)
	f := &fixture{t: t, app: app, refresher: &recordingRefresher{}, platform: &atomic.Int32{}}
	registry := platform.NewRegistry(
		fakePlatform{t: t, id: alpha, calls: f.platform},
		fakePlatform{t: t, id: beta, calls: f.platform},
	)
	f.ytdlpRan = filepath.Join(t.TempDir(), "ran")
	binary := testutil.Script(t, "yt-dlp", "touch '"+f.ytdlpRan+"'\nexit 1")
	runner := ytdlp.New(ytdlp.Options{Binary: binary, CookieDir: filepath.Join(app.DataDir(), "cookies")})
	accounts, err := sources.New(app, registry, runner, sources.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f.store = catalog.New(app, registry, catalog.Options{})
	f.accounts = accounts
	f.svc = New(app, f.store, accounts, f.refresher)
	t.Cleanup(func() {
		if _, err := os.Stat(f.ytdlpRan); err == nil {
			t.Errorf("yt-dlp ran while serving a feed")
		}
		if n := f.platform.Load(); n != 0 {
			t.Errorf("%d platform calls", n)
		}
	})
	return f
}

func (f *fixture) save(collection string, values map[string]any) *core.Record {
	f.t.Helper()
	schema, err := f.app.FindCollectionByNameOrId(collection)
	if err != nil {
		f.t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := f.app.Save(record); err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *fixture) user(email string) *core.Record {
	f.t.Helper()
	collection, err := f.app.FindCollectionByNameOrId("users")
	if err != nil {
		f.t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("feed-fixture-password-123")
	record.Set("role", "user")
	if err := f.app.Save(record); err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *fixture) profile(user *core.Record, name string) *core.Record {
	f.t.Helper()
	return f.save("profiles", map[string]any{"user": user.Id, "name": name})
}

// video describes a stored video: its id is also the name of its channel unless channel is set.
type video struct {
	id, channel, published string
	tags                   []string
	subscriberOnly         bool
	platform               string
	owner                  string
}

// put saves the videos (public unless owner is set) and returns their record ids by native id.
func (f *fixture) put(videos ...video) map[string]string {
	f.t.Helper()
	ids := map[string]string{}
	for _, v := range videos {
		p := v.platform
		if p == "" {
			p = alpha
		}
		listing := platform.Video{ID: v.id, Platform: p, Title: v.id, ChannelID: v.channel, ChannelName: v.channel, Tags: v.tags, Published: v.published, SubscriberOnly: v.subscriberOnly, SubscriberChecked: true}
		if err := f.store.StoreVideo(&listing, v.owner); err != nil {
			f.t.Fatal(err)
		}
		ids[v.id] = listing.RecordID
	}
	return ids
}

// channel returns the record of a stored channel and sets its listing expiry.
func (f *fixture) channel(platformID, id string, expiresIn time.Duration) *core.Record {
	f.t.Helper()
	record, err := f.store.FindChannel(platformID, id)
	if err != nil {
		f.t.Fatal(err)
	}
	record.Set("expires_at", time.Now().Add(expiresIn))
	if err := f.app.Save(record); err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *fixture) subscribe(profile *core.Record, channel *core.Record) {
	f.t.Helper()
	f.save("subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id})
}

// subscribeTo makes the profile follow a stored channel with a fresh listing.
func (f *fixture) subscribeTo(profile *core.Record, channelID string) *core.Record {
	f.t.Helper()
	channel := f.channel(alpha, channelID, time.Hour)
	f.subscribe(profile, channel)
	return channel
}

func (f *fixture) like(profile *core.Record, recordID string) {
	f.t.Helper()
	f.save("video_reactions", map[string]any{"profile": profile.Id, "video": recordID, "reaction": "like"})
}

func (f *fixture) feed(user, profile *core.Record) []string {
	f.t.Helper()
	videos, err := f.svc.Feed(context.Background(), user.Id, profile.Id)
	if err != nil {
		f.t.Fatal(err)
	}
	return recordIDs(videos)
}

func (f *fixture) recommend(user, profile *core.Record, anchorRecordID string) []string {
	f.t.Helper()
	videos, err := f.svc.Recommendations(context.Background(), user.Id, profile.Id, anchorRecordID)
	if err != nil {
		f.t.Fatal(err)
	}
	return recordIDs(videos)
}

func recordIDs(videos []platform.Video) []string {
	ids := make([]string, 0, len(videos))
	for _, v := range videos {
		ids = append(ids, v.RecordID)
	}
	return ids
}

// want turns native ids into the record ids the answers carry.
func want(ids map[string]string, natives ...string) []string {
	out := make([]string, 0, len(natives))
	for _, native := range natives {
		out = append(out, ids[native])
	}
	return out
}

func TestFeedIsolatesSubscriptionsAndReadsTheCacheOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user("feed-owner@example.test"), f.user("feed-other@example.test")
	first, second, empty := f.profile(owner, "First"), f.profile(owner, "Second"), f.profile(owner, "Empty")
	foreign := f.profile(other, "Foreign")
	ids := f.put(
		video{id: "first-new", channel: "UCfirst", published: "2026-10-02"},
		video{id: "first-old", channel: "UCfirst", published: "2026-10-01"},
		video{id: "second-new", channel: "UCsecond", published: "2026-10-02"},
		video{id: "second-old", channel: "UCsecond", published: "2026-10-01"},
		video{id: "foreign-video", channel: "UCforeign", published: "2026-10-03"},
	)
	f.subscribeTo(first, "UCfirst")
	f.subscribeTo(second, "UCsecond")
	f.subscribeTo(foreign, "UCforeign")

	for _, test := range []struct {
		profile *core.Record
		want    []string
	}{
		{first, want(ids, "first-new", "first-old")},
		{second, want(ids, "second-new", "second-old")},
		{empty, []string{}},
		{first, want(ids, "first-new", "first-old")},
	} {
		if got := f.feed(owner, test.profile); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("profile %s feed=%v want=%v", test.profile.GetString("name"), got, test.want)
		}
	}
	if calls := f.refresher.scheduled(); len(calls) != 0 {
		t.Fatalf("fresh channels were scheduled for a refresh: %v", calls)
	}
}

func TestFeedOfAnEmptyCacheIsEmptyNotAnError(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("feed-empty@example.test")
	profile := f.profile(owner, "Mine")
	stub, err := f.store.UpsertChannel(alpha, "UCnever", "Never listed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f.subscribe(profile, stub)

	videos, err := f.svc.Feed(context.Background(), owner.Id, profile.Id)
	if err != nil || videos == nil || len(videos) != 0 {
		t.Fatalf("an empty cache gives an empty feed: %v %v", videos, err)
	}
	if got, err := f.svc.Recommendations(context.Background(), owner.Id, profile.Id, "neverseen00000"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("an empty cache gives empty recommendations: %v %v", got, err)
	}
}

func TestStaleChannelsAreScheduledOnceAndStillFeedTheLastListing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("feed-stale@example.test")
	profile := f.profile(owner, "Mine")
	ids := f.put(
		video{id: "fresh-video", channel: "UCfresh", published: "2026-10-03"},
		video{id: "stale-video", channel: "UCstale", published: "2026-10-02"},
		video{id: "off-video", channel: "UCoff", published: "2026-10-01", platform: beta},
	)
	fresh := f.subscribeTo(profile, "UCfresh")
	stale := f.channel(alpha, "UCstale", -time.Hour)
	f.subscribe(profile, stale)
	never, err := f.store.UpsertChannel(alpha, "UCnever", "Never listed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f.subscribe(profile, never)
	// A stale channel of a platform the account turned off is left out of the refresh as well.
	off, err := f.store.FindChannel(beta, "UCoff")
	if err != nil {
		t.Fatal(err)
	}
	f.subscribe(profile, off)
	if err := f.accounts.SetEnabled(owner.Id, beta, false); err != nil {
		t.Fatal(err)
	}
	// The stale channel's video row expired too: it is still the last known listing.
	record, err := f.store.FindVideo(alpha, "stale-video")
	if err != nil {
		t.Fatal(err)
	}
	record.Set("expires_at", time.Now().Add(-time.Hour))
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}

	got := f.feed(owner, profile)
	if wanted := want(ids, "fresh-video", "stale-video"); !reflect.DeepEqual(got, wanted) {
		t.Fatalf("feed=%v want=%v (the disabled platform stays out, the expired listing stays in)", got, wanted)
	}
	calls := f.refresher.scheduled()
	if len(calls) != 1 {
		t.Fatalf("the stale channels are handed over in one call, got %v", calls)
	}
	if wantIDs := []string{never.Id, stale.Id}; !sameSet(calls[0], wantIDs) {
		t.Fatalf("scheduled=%v want %v (not %s, which is fresh)", calls[0], wantIDs, fresh.Id)
	}
	// Every request reports what it found stale; the refresher coalesces.
	f.feed(owner, profile)
	if n := len(f.refresher.scheduled()); n != 2 {
		t.Fatalf("one Schedule call per request, got %d in total", n)
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func TestFeedLeavesOutPrivateVideosOfOtherAccountsAndDisabledPlatforms(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user("feed-visible@example.test"), f.user("feed-hidden@example.test")
	profile := f.profile(owner, "Mine")
	ids := f.put(
		video{id: "public", channel: "UCshared", published: "2026-10-03"},
		video{id: "mine", channel: "UCshared", published: "2026-10-02", owner: owner.Id},
		video{id: "theirs", channel: "UCshared", published: "2026-10-01", owner: other.Id},
		video{id: "beta-video", channel: "UCbeta", published: "2026-10-04", platform: beta},
	)
	f.subscribeTo(profile, "UCshared")
	betaChannel, err := f.store.FindChannel(beta, "UCbeta")
	if err != nil {
		t.Fatal(err)
	}
	f.subscribe(profile, betaChannel)

	if got, wanted := f.feed(owner, profile), want(ids, "beta-video", "public", "mine"); !reflect.DeepEqual(got, wanted) {
		t.Fatalf("feed=%v want=%v", got, wanted)
	}
	if err := f.accounts.SetEnabled(owner.Id, beta, false); err != nil {
		t.Fatal(err)
	}
	if got, wanted := f.feed(owner, profile), want(ids, "public", "mine"); !reflect.DeepEqual(got, wanted) {
		t.Fatalf("a platform the account turned off must not feed: %v want=%v", got, wanted)
	}
}

func TestFeedKeepsTheFiftyBestCandidates(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("feed-top@example.test")
	profile := f.profile(owner, "Mine")
	var videos []video
	for i := range 60 {
		videos = append(videos, video{id: "v" + string(rune('a'+i/26)) + string(rune('a'+i%26)), channel: "UCmany", published: time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC).Format(platform.DayLayout)})
	}
	f.put(videos...)
	f.subscribeTo(profile, "UCmany")
	if got := f.feed(owner, profile); len(got) != feedLimit {
		t.Fatalf("feed holds %d videos, want %d", len(got), feedLimit)
	}
}

func TestFeedLikesAreScopedToProfile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("feed-ranking@example.test")
	first, second := f.profile(owner, "First"), f.profile(owner, "Second")
	ids := f.put(
		video{id: "blue-video", channel: "UCshared", tags: []string{"blue"}, published: "2026-01-01"},
		video{id: "red-video", channel: "UCshared", tags: []string{"red"}, published: "2026-01-01"},
		video{id: "blue-seed", channel: "UCseeds", tags: []string{"blue"}},
		video{id: "red-seed-one", channel: "UCseeds", tags: []string{"red"}},
		video{id: "red-seed-two", channel: "UCseeds", tags: []string{"red"}},
	)
	f.subscribeTo(first, "UCshared")
	f.subscribeTo(second, "UCshared")
	f.like(first, ids["blue-seed"])
	f.like(second, ids["red-seed-one"])
	f.like(second, ids["red-seed-two"])

	if got := f.feed(owner, first); !reflect.DeepEqual(got, want(ids, "blue-video", "red-video")) {
		t.Fatalf("sibling likes changed the first profile's ranking: %v", got)
	}
	if got := f.feed(owner, second); !reflect.DeepEqual(got, want(ids, "red-video", "blue-video")) {
		t.Fatalf("the second profile's likes did not determine its ranking: %v", got)
	}
}

// queries counts the SQL queries fn runs.
func queries(app core.App, fn func()) int {
	var count atomic.Int32
	for _, builder := range []dbx.Builder{app.ConcurrentDB(), app.NonconcurrentDB()} {
		db := builder.(*dbx.DB)
		previous := db.QueryLogFunc
		db.QueryLogFunc = func(context.Context, time.Duration, string, *sql.Rows, error) { count.Add(1) }
		defer func() { db.QueryLogFunc = previous }()
	}
	fn()
	return int(count.Load())
}

func TestLikedTagFrequencyUsesABoundedNumberOfQueries(t *testing.T) {
	// Each test owns its app, so the query hook below only sees its own queries.
	f := newFixture(t)
	owner := f.user("feed-likes@example.test")
	few, many := f.profile(owner, "Few"), f.profile(owner, "Many")
	var videos []video
	for i := range 40 {
		videos = append(videos, video{id: "seed" + string(rune('a'+i/26)) + string(rune('a'+i%26)), channel: "UCseed" + string(rune('a'+i%7)), tags: []string{"blue", "tag" + string(rune('a'+i%5))}})
	}
	ids := f.put(videos...)
	for i, v := range videos {
		if i < 3 {
			f.like(few, ids[v.id])
		}
		f.like(many, ids[v.id])
	}

	var fewFrequency, manyFrequency map[string]int
	count := func(profile *core.Record, into *map[string]int) int {
		return queries(f.app, func() {
			frequency, err := f.svc.likedTagFrequency(profile.Id)
			if err != nil {
				t.Fatal(err)
			}
			*into = frequency
		})
	}
	fewQueries, manyQueries := count(few, &fewFrequency), count(many, &manyFrequency)
	if manyFrequency["blue"] != 40 || fewFrequency["blue"] != 3 || manyFrequency["taga"] != 8 {
		t.Fatalf("frequencies: few=%v many=%v", fewFrequency, manyFrequency)
	}
	if fewQueries != manyQueries || manyQueries > 4 {
		t.Fatalf("liked tags cost %d queries for 3 likes and %d for 40: the number of queries must not grow with the likes", fewQueries, manyQueries)
	}
}

func TestRecommendationsRespectHideSubscriberOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("reco-subscriber@example.test")
	profile := f.profile(owner, "Mine")
	ids := f.put(
		video{id: "current", channel: "streamer", published: "2026-10-01"},
		video{id: "public-vod", channel: "streamer", published: "2026-09-02"},
		video{id: "sub-vod", channel: "streamer", published: "2026-09-01", subscriberOnly: true},
	)
	f.subscribeTo(profile, "streamer")
	settings := f.save("settings", map[string]any{"user": owner.Id, "hide_subscriber_only": true})

	if got := f.recommend(owner, profile, ids["current"]); !reflect.DeepEqual(got, want(ids, "public-vod")) {
		t.Fatalf("hidden subscriber-only VOD leaked: %v", got)
	}
	// Opting back in brings the restricted VOD back.
	settings.Set("hide_subscriber_only", false)
	if err := f.app.Save(settings); err != nil {
		t.Fatal(err)
	}
	if got := f.recommend(owner, profile, ids["current"]); !reflect.DeepEqual(got, want(ids, "public-vod", "sub-vod")) {
		t.Fatalf("subscriber-only VOD missing after opting in: %v", got)
	}
}

func TestRecommendationsSkipWatchedVideos(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("reco-watched@example.test")
	profile, sibling := f.profile(owner, "Mine"), f.profile(owner, "Sibling")
	ids := f.put(
		video{id: "current", channel: "UCwatched", published: "2026-10-01"},
		video{id: "watched-one", channel: "UCsubscribed", published: "2026-09-02"},
		video{id: "fresh-one", channel: "UCsubscribed", published: "2026-09-01"},
		video{id: "sibling-watched", channel: "UCsubscribed", published: "2026-08-01"},
	)
	f.subscribeTo(profile, "UCsubscribed")
	f.save("watch_history", map[string]any{"profile": profile.Id, "video": ids["watched-one"], "watched_at": "2026-11-01 00:00:00.000Z"})
	f.save("watch_history", map[string]any{"profile": sibling.Id, "video": ids["sibling-watched"], "watched_at": "2026-11-01 00:00:00.000Z"})

	if got := f.recommend(owner, profile, ids["current"]); !reflect.DeepEqual(got, want(ids, "fresh-one", "sibling-watched")) {
		t.Fatalf("watched video suggested, or another profile's history applied: %v", got)
	}
}

func TestRecommendationsAnchorOnCurrentVideo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("reco-anchor@example.test")
	profile := f.profile(owner, "Mine")
	// The watched video: its channel and tags anchor the ranking. Its channel is not subscribed to.
	// The other three are equally old: a same-topic one, a same-channel one and an off-topic one.
	ids := f.put(
		video{id: "current", channel: "UCwatched", tags: []string{"space"}, published: "2026-10-01"},
		video{id: "same-topic", channel: "UCsubscribed", tags: []string{"space"}, published: "2026-09-01"},
		video{id: "same-channel", channel: "UCwatched", published: "2026-09-01"},
		video{id: "off-topic", channel: "UCsubscribed", tags: []string{"cooking"}, published: "2026-09-01"},
	)
	f.subscribeTo(profile, "UCsubscribed")

	// The current video is excluded; the same-channel video ranks first, the shared-tag one before the off-topic one.
	// Same age, so the presentation order is the ranking order.
	if got := f.recommend(owner, profile, ids["current"]); !reflect.DeepEqual(got, want(ids, "same-channel", "same-topic", "off-topic")) {
		t.Fatalf("recommendations=%v", got)
	}
}

func TestRecommendationsIgnoreUnrecordedAndInvisibleAnchors(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user("reco-unknown@example.test"), f.user("reco-private@example.test")
	profile := f.profile(owner, "Mine")
	ids := f.put(
		video{id: "cached", channel: "UCsubscribed", published: "2026-09-01"},
		video{id: "private", channel: "UCprivate", tags: []string{"secret"}, published: "2026-09-02", owner: other.Id},
		video{id: "secret-topic", channel: "UCsubscribed", tags: []string{"secret"}, published: "2026-08-01"},
	)
	f.subscribeTo(profile, "UCsubscribed")

	// An unknown record id still gets generic recommendations: nothing anchors them, nothing is excluded.
	if got := f.recommend(owner, profile, "neverseen00000"); !reflect.DeepEqual(got, want(ids, "cached", "secret-topic")) {
		t.Fatalf("recommendations=%v", got)
	}
	// A private video of another account anchors nothing: neither its tags nor its channel pull candidates up.
	if got := f.recommend(owner, profile, ids["private"]); !reflect.DeepEqual(got, want(ids, "cached", "secret-topic")) {
		t.Fatalf("a private anchor leaked into the ranking: %v", got)
	}
}

func TestRecommendationsScoringIsScopedToProfile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("reco-scoped@example.test")
	profile := f.profile(owner, "Mine")
	ids := f.put(
		video{id: "current", channel: "UCwatched", tags: []string{"red"}, published: "2026-10-01"},
		video{id: "red-video", channel: "UCsubscribed", tags: []string{"red"}, published: "2026-09-01"},
		video{id: "blue-video", channel: "UCsubscribed", tags: []string{"blue"}, published: "2026-09-01"},
		// Liked by this profile: the "blue" topic must rise for it.
		video{id: "blue-seed", channel: "UCseeds", tags: []string{"blue"}},
	)
	f.subscribeTo(profile, "UCsubscribed")
	f.like(profile, ids["blue-seed"])

	// The current video's "red" tag (+3) outweighs the profile's "blue" like (+2), so red comes first.
	if got := f.recommend(owner, profile, ids["current"]); !reflect.DeepEqual(got, want(ids, "red-video", "blue-video")) {
		t.Fatalf("recommendations=%v", got)
	}
}

func TestRecommendationsKeepTheTwentyBest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user("reco-top@example.test")
	profile := f.profile(owner, "Mine")
	var videos []video
	for i := range 30 {
		videos = append(videos, video{id: "v" + string(rune('a'+i)), channel: "UCmany", published: time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC).Format(platform.DayLayout)})
	}
	f.put(videos...)
	f.subscribeTo(profile, "UCmany")
	if got := f.recommend(owner, profile, "neverseen00000"); len(got) != recommendationLimit {
		t.Fatalf("%d recommendations, want %d", len(got), recommendationLimit)
	}
}

func TestScoreVideo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	old := platform.Video{Tags: []string{"a", "b"}, ChannelRecordID: "ch", Published: "2026-01-01"}
	frequency := map[string]int{"a": 2}
	if got := scoreVideo(old, frequency, nil, now); got != 5+4 {
		t.Fatalf("base + liked tags: %v", got)
	}
	if got := scoreVideo(old, frequency, &anchor{channelRecordID: "ch", tags: []string{"b"}}, now); got != 5+4+3+10 {
		t.Fatalf("anchor tag and channel: %v", got)
	}
	recent := platform.Video{Published: now.Add(-24 * time.Hour).Format(time.RFC3339)}
	if got := scoreVideo(recent, nil, nil, now); !(got > 5+8 && got < 5+10) {
		t.Fatalf("recency boost of a day-old video: %v", got)
	}
	if got := scoreVideo(platform.Video{Published: "not a date"}, nil, nil, now); got != 5 {
		t.Fatalf("an unparsable date gives no boost: %v", got)
	}
}
