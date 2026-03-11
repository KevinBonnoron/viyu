package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// profileListing answers every channel with a complete profile and records the ids it was asked for.
func profileListing(asked *[]string, mu *sync.Mutex) func(context.Context, string, int) (platform.Channel, []platform.Video, error) {
	return func(_ context.Context, id string, limit int) (platform.Channel, []platform.Video, error) {
		mu.Lock()
		*asked = append(*asked, fmt.Sprintf("%s/%d", id, limit))
		mu.Unlock()
		return platform.Channel{ID: id, Platform: alpha, Name: "Fixture channel", Thumbnail: fixtureAvatar, SubscriberCount: 1234}, nil, nil
	}
}

func TestProfileBackfillFillsIncompleteChannelsAndDoesNotRetryTooSoon(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	const existing = "https://yt3.googleusercontent.com/existing=s176-c"
	w.channel(t, alpha, "UCfixture", "Fixture channel", "")
	complete := w.channel(t, alpha, "UCcomplete", "Complete", existing)
	complete.Set("subscriber_count", 42)
	if err := w.app.Save(complete); err != nil {
		t.Fatal(err)
	}
	var asked []string
	var mu sync.Mutex
	failing := false
	listing := profileListing(&asked, &mu)
	w.alpha.listing = func(ctx context.Context, id string, limit int) (platform.Channel, []platform.Video, error) {
		if failing {
			return platform.Channel{}, nil, errors.New("listing failed")
		}
		return listing(ctx, id, limit)
	}
	b := NewProfileBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{})
	ctx := context.Background()

	if tried := b.pass(ctx); tried != 1 {
		t.Fatalf("only the incomplete channel should be looked up: %d", tried)
	}
	if !slices.Equal(asked, []string{"UCfixture/1"}) {
		t.Fatalf("lookups: %v", asked)
	}
	if got := w.findChannel(t, alpha, "UCfixture"); got.GetString("thumbnail") != fixtureAvatar || got.GetFloat("subscriber_count") != 1234 {
		t.Fatalf("missing profile was not filled: %+v", got.PublicExport())
	}
	if got := w.findChannel(t, alpha, "UCcomplete"); got.GetString("thumbnail") != existing || got.GetFloat("subscriber_count") != 42 {
		t.Fatalf("a complete channel was touched: %+v", got.PublicExport())
	}

	// A channel still incomplete is not asked again before the retry delay.
	failing = true
	record := w.findChannel(t, alpha, "UCfixture")
	record.Set("thumbnail", "")
	if err := w.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if tried := b.pass(ctx); tried != 0 {
		t.Fatalf("recent attempt was repeated: %d", tried)
	}
	// After the delay it is attempted again, and a failing lookup changes nothing.
	b.attempted[record.Id] = time.Now().Add(-b.opts.Retry - time.Minute)
	if tried := b.pass(ctx); tried != 1 {
		t.Fatalf("expired attempt was not retried: %d", tried)
	}
	if got := w.findChannel(t, alpha, "UCfixture").GetString("thumbnail"); got != "" {
		t.Fatalf("failed lookup invented an avatar: %q", got)
	}
}

func TestProfileBackfillListsSubscribedChannelsFirstAndIncludesMissingSubscriberCounts(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for i := range 12 {
		w.channel(t, alpha, fmt.Sprintf("UCa%02d", i), "Other", "")
	}
	// A platform the server does not know, and one nobody uses: neither is looked up.
	w.record(t, "channels", map[string]any{"platform": "peertube", "platform_id": "UCunsupported", "expires_at": time.Unix(0, 0)})
	w.channel(t, beta, "UCunused", "Unused", "")
	// An avatar without a subscriber count is still incomplete: the player reads both from the channel record.
	w.channel(t, alpha, "UCavatarOnly", "Avatar only", "https://yt3.googleusercontent.com/only=s176-c")
	w.subscribe(t, w.channel(t, alpha, "UCzsubscribed", "Subscribed", ""))
	b := NewProfileBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{})

	channels, err := b.missing(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 14 || channels[0].GetString("platform_id") != "UCzsubscribed" {
		t.Fatalf("subscribed channel must come first among %d channels: %v", len(channels), ids(channels))
	}
	seen := map[string]bool{}
	for _, id := range ids(channels) {
		seen[id] = true
	}
	if seen["UCunsupported"] || seen["UCunused"] || !seen["UCavatarOnly"] {
		t.Fatalf("unknown or unused platform listed, or avatar-only channel missing: %v", seen)
	}
	b.attempted[channels[0].Id] = time.Now()
	next, err := b.missing(1)
	if err != nil || len(next) != 1 || next[0].GetString("platform_id") == "UCzsubscribed" {
		t.Fatalf("recently attempted channel was listed again: %v %v", ids(next), err)
	}

	// The platform nobody used is looked up as soon as an account turns it on.
	user := w.user(t)
	if err := w.sources.SetEnabled(user.Id, beta, true); err != nil {
		t.Fatal(err)
	}
	all, err := b.missing(30)
	if err != nil || !slices.Contains(ids(all), "UCunused") {
		t.Fatalf("a platform an account turned on was skipped: %v %v", ids(all), err)
	}
}

func TestProfileBackfillBatchIsBoundedByOptions(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for i := range 7 {
		w.channel(t, alpha, fmt.Sprintf("UCb%02d", i), "Other", "")
	}
	var asked []string
	var mu sync.Mutex
	w.alpha.listing = profileListing(&asked, &mu)
	b := NewProfileBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{Batch: 3, Concurrency: 1})
	if tried := b.pass(context.Background()); tried != 3 || len(asked) != 3 {
		t.Fatalf("one pass must stay within the batch: tried=%d asked=%v", tried, asked)
	}
}

func TestProfileBackfillStopsStartingLookupsWhenCancelled(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for i := range 4 {
		w.channel(t, alpha, fmt.Sprintf("UCc%02d", i), "Other", "")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	var mu sync.Mutex
	w.alpha.listing = func(ctx context.Context, _ string, _ int) (platform.Channel, []platform.Video, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		cancel()
		<-ctx.Done()
		return platform.Channel{}, nil, ctx.Err()
	}
	b := NewProfileBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{Concurrency: 1})
	if tried := b.pass(ctx); tried != 0 {
		t.Fatalf("a cancelled pass reports no work: %d", tried)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("lookups started after cancellation: %d", calls)
	}
}

func TestProfileBackfillRunWorksOnStartAndOnWakeAndStopsWithItsContext(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.channel(t, alpha, "UCfirst", "First", "")
	asked := make(chan string, 8)
	w.alpha.listing = func(_ context.Context, id string, _ int) (platform.Channel, []platform.Video, error) {
		asked <- id
		return platform.Channel{ID: id, Platform: alpha, Name: id, Thumbnail: fixtureAvatar, SubscriberCount: 1}, nil, nil
	}
	b := NewProfileBackfill(w.app, w.registry, w.store, w.sources, w.profiles, BackfillOptions{StartDelay: time.Millisecond, IdleInterval: time.Hour, BusyInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Run(ctx)
	}()
	if got := receive(t, asked, "the first pass"); got != "UCfirst" {
		t.Fatalf("first pass looked up %q", got)
	}
	// The pauses are an hour: only a wake-up can start the next pass, and the store asks for it itself
	// when it stores a channel without a profile.
	w.channel(t, alpha, "UCsecond", "Second", "")
	if got := receive(t, asked, "the pass a new channel woke"); got != "UCsecond" {
		t.Fatalf("woken pass looked up %q", got)
	}
	cancel()
	receive(t, done, "Run to return")
}

func profileOf(record *core.Record) (string, float64) {
	return record.GetString("thumbnail"), record.GetFloat("subscriber_count")
}

func TestProfileBackfillFetchSkipsAnUnknownPlatform(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	b := NewProfileBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{})
	w.channel(t, alpha, "UCsame", "Same", "")
	b.fetch(context.Background(), "peertube", "UCsame")
	if thumbnail, count := profileOf(w.findChannel(t, alpha, "UCsame")); thumbnail != "" || count != 0 {
		t.Fatalf("a lookup on an unknown platform changed a channel: %q %v", thumbnail, count)
	}
}
