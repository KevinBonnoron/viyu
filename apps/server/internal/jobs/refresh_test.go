package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// listings counts and scripts the channel listings of the fake platform.
type listings struct {
	mu    sync.Mutex
	asked []string
	sizes []int
	fail  map[string]bool
	// gate, when set, is entered by every listing before it answers.
	gate func(ctx context.Context, id string) error
}

func (l *listings) answer(ctx context.Context, id string, limit int) (platform.Channel, []platform.Video, error) {
	l.mu.Lock()
	l.asked = append(l.asked, id)
	l.sizes = append(l.sizes, limit)
	fail := l.fail[id]
	gate := l.gate
	l.mu.Unlock()
	if gate != nil {
		if err := gate(ctx, id); err != nil {
			return platform.Channel{}, nil, err
		}
	}
	if fail {
		return platform.Channel{}, nil, errors.New("listing failed: private-cookie-value")
	}
	return platform.Channel{ID: id, Platform: alpha, Name: "Channel " + id, Thumbnail: fixtureAvatar, SubscriberCount: 5},
		[]platform.Video{
			{ID: "old-" + id, Platform: alpha, Title: "Old", ChannelID: id, ChannelName: "Channel " + id, Published: "2025-01-01"},
			{ID: "new-" + id, Platform: alpha, Title: "New", ChannelID: id, ChannelName: "Channel " + id, Published: "2026-06-01"},
		}, nil
}

func (l *listings) askedIDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.asked)
}

func (w *world) refresher(t *testing.T, opts RefresherOptions) (*Refresher, *listings) {
	t.Helper()
	l := &listings{fail: map[string]bool{}}
	w.alpha.listing = l.answer
	group := NewGroup()
	t.Cleanup(group.Stop)
	return NewRefresher(w.app, w.registry, w.store, w.sources, group.Go, opts), l
}

// staleChannel stores a subscribed-style channel with a cached video and expires its listing.
func (w *world) staleChannel(t *testing.T, id string) *core.Record {
	t.Helper()
	w.video(t, platform.Video{ID: "kept-" + id, Platform: alpha, Title: "Kept", ChannelID: id, ChannelName: id, Published: "2026-06-01"})
	w.expire(t, alpha, id)
	return w.findChannel(t, alpha, id)
}

// freshChannel stores a channel whose listing was just cached.
func (w *world) freshChannel(t *testing.T, id string) *core.Record {
	t.Helper()
	w.video(t, platform.Video{ID: "kept-" + id, Platform: alpha, Title: "Kept", ChannelID: id, ChannelName: id, Published: "2026-06-01"})
	info := platform.Channel{ID: id, Platform: alpha, Name: id}
	if err := w.store.StoreChannelListing(&info, ""); err != nil {
		t.Fatal(err)
	}
	return w.findChannel(t, alpha, id)
}

func TestChannelCachesTheListingNewestFirstAndReportsFailuresAsSafeErrors(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r, l := w.refresher(t, RefresherOptions{ListingSize: 7})
	enriched := atomic.Bool{}
	w.alpha.enrich = func(videos []platform.Video) {
		enriched.Store(true)
		videos[0].Title = "Canonical"
	}

	info, videos, err := r.Channel(context.Background(), alpha, "UCone")
	if err != nil {
		t.Fatal(err)
	}
	if !enriched.Load() || l.sizes[0] != 7 {
		t.Fatalf("enriched=%v size=%d", enriched.Load(), l.sizes[0])
	}
	if info.RecordID == "" || len(videos) != 2 || videos[0].ID != "new-UCone" || videos[1].ID != "old-UCone" {
		t.Fatalf("listing must be newest first with record ids: %+v %+v", info, videos)
	}
	for _, v := range videos {
		if v.RecordID == "" || v.ChannelRecordID != info.RecordID {
			t.Fatalf("video was not stored under the channel: %+v (channel %s)", v, info.RecordID)
		}
	}
	// The listing ran through EnrichListing before it was stored.
	if title, _ := catalog.TitlesOf(mustVideo(t, w, "old-UCone")); title != "Canonical" {
		t.Fatalf("the stored listing was not enriched: %q", title)
	}
	if record := w.findChannel(t, alpha, "UCone"); NeedsRefresh(w.app, record) || record.GetString("thumbnail") != fixtureAvatar {
		t.Fatalf("a listed channel must be fresh with its profile: %+v", record.PublicExport())
	}

	_, _, err = r.Channel(context.Background(), "nowhere", "UCone")
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Code != apierr.CodeInvalidSourceArg {
		t.Fatalf("an unknown platform is an invalid input: %v", err)
	}
	l.fail["UCbad"] = true
	_, _, err = r.Channel(context.Background(), alpha, "UCbad")
	if !errors.As(err, &safe) || safe.Code != apierr.CodeRequestFailed || safe.Cause == nil {
		t.Fatalf("a failing listing is a request failure with its cause attached: %v", err)
	}
	if safe.Message == "" || strings.Contains(safe.Message, "private-cookie-value") {
		t.Fatalf("the message must not carry the cause: %q", safe.Message)
	}
}

func TestNeedsRefreshWhenTheListingExpiredOrNoVideoIsFresh(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	if channel := w.staleChannel(t, "UCexpired"); !NeedsRefresh(w.app, channel) {
		t.Fatal("a channel whose listing expired must be refreshed")
	}
	if channel := w.freshChannel(t, "UCfresh"); NeedsRefresh(w.app, channel) {
		t.Fatal("a channel listed just now must not be refreshed")
	}
	// A live expiry is not enough when every cached video expired.
	channel := w.freshChannel(t, "UCstalevideos")
	if _, err := w.app.DB().NewQuery("UPDATE videos SET expires_at = '2000-01-01 00:00:00.000Z' WHERE channel = {:id}").Bind(map[string]any{"id": channel.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	if !NeedsRefresh(w.app, channel) {
		t.Fatal("a channel with no fresh video must be refreshed")
	}
	// And a channel that never cached a video has nothing usable either.
	info := platform.Channel{ID: "UCempty", Platform: alpha, Name: "Empty"}
	if err := w.store.StoreChannelListing(&info, ""); err != nil {
		t.Fatal(err)
	}
	if !NeedsRefresh(w.app, w.findChannel(t, alpha, "UCempty")) {
		t.Fatal("a channel without videos must be refreshed")
	}
}

// Storing a channel profile (a search result card, the avatar backfill) is not caching a listing: a
// subscribed channel whose listing expired must still be refreshed afterwards.
func TestChannelProfileStoresDoNotHideAnExpiredListing(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r, _ := w.refresher(t, RefresherOptions{})
	w.staleChannel(t, "UCfixture")
	needsRefresh := func() bool {
		t.Helper()
		return NeedsRefresh(w.app, w.findChannel(t, alpha, "UCfixture"))
	}
	if !needsRefresh() {
		t.Fatal("a channel whose listing expired must be refreshed")
	}

	// A search result card names the channel; it lists nothing.
	if _, err := w.store.UpsertChannel(alpha, "UCfixture", "Fixture channel renamed", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := w.findChannel(t, alpha, "UCfixture").GetString("name"); got != "Fixture channel renamed" || !needsRefresh() {
		t.Fatalf("a channel card renewed the listing (name=%q, needs refresh=%v)", got, needsRefresh())
	}

	// The profile backfill lists a single entry and caches no video.
	NewProfileBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{}).fetch(context.Background(), alpha, "UCfixture")
	if got := w.findChannel(t, alpha, "UCfixture").GetString("thumbnail"); got != fixtureAvatar || !needsRefresh() {
		t.Fatalf("the profile backfill renewed the listing (avatar=%q, needs refresh=%v)", got, needsRefresh())
	}

	// A channel first seen by one of these stores starts expired, like one created by a video.
	w.channel(t, alpha, "UCbrandnew", "Brand new", "")
	if year := w.findChannel(t, alpha, "UCbrandnew").GetDateTime("expires_at").Time().Year(); year >= 2000 {
		t.Fatalf("a channel created by a card must start expired: %d", year)
	}

	// A real listing renews it.
	if _, _, err := r.Channel(context.Background(), alpha, "UCfixture"); err != nil {
		t.Fatal(err)
	}
	if needsRefresh() {
		t.Fatal("a channel whose listing was just cached must not be refreshed again")
	}
}

func TestRefreshOnlyRefreshesStaleChannelsAndToleratesFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r, l := w.refresher(t, RefresherOptions{})
	fresh := w.freshChannel(t, "UCfresh")
	stale := w.staleChannel(t, "UCstale")
	broken := w.staleChannel(t, "UCbroken")
	other := w.staleChannel(t, "UCother")
	unknown := w.record(t, "channels", map[string]any{"platform": "peertube", "platform_id": "UCunknown", "expires_at": time.Unix(0, 0)})
	l.fail["UCbroken"] = true
	ctx := context.Background()

	// A fresh channel is served from the cache: nothing is listed.
	if refreshed, failed, err := r.Refresh(ctx, []*core.Record{fresh, unknown}); refreshed != 0 || failed != 0 || err != nil || len(l.askedIDs()) != 0 {
		t.Fatalf("fresh cache: %d %d %v %v", refreshed, failed, err, l.askedIDs())
	}

	// A failing channel is counted and does not stop the ones after it.
	refreshed, failed, err := r.Refresh(ctx, []*core.Record{fresh, broken, stale, unknown, other})
	if err != nil || refreshed != 2 || failed != 1 {
		t.Fatalf("refreshed=%d failed=%d err=%v", refreshed, failed, err)
	}
	asked := l.askedIDs()
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"UCbroken", "UCother", "UCstale"}) {
		t.Fatalf("only the stale channels of known platforms are listed: %v", asked)
	}
	if _, err := w.store.FindVideo(alpha, "new-UCstale"); err != nil {
		t.Fatalf("refreshed video was not cached: %v", err)
	}
	// What was refreshed is fresh now, what failed is still stale.
	if NeedsRefresh(w.app, w.findChannel(t, alpha, "UCstale")) || !NeedsRefresh(w.app, w.findChannel(t, alpha, "UCbroken")) {
		t.Fatal("the refresh must renew exactly the channels it listed")
	}
}

func TestRefreshRunsAtMostConcurrencyListingsAtOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r, l := w.refresher(t, RefresherOptions{Concurrency: 2})
	var channels []*core.Record
	for i := range 6 {
		channels = append(channels, w.staleChannel(t, fmt.Sprintf("UCpar%d", i)))
	}
	var running, peak atomic.Int32
	arrived := make(chan struct{}, 6)
	release := make(chan struct{})
	l.mu.Lock()
	l.gate = func(ctx context.Context, _ string) error {
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		arrived <- struct{}{}
		defer running.Add(-1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.mu.Unlock()

	type result struct{ refreshed, failed int }
	done := make(chan result, 1)
	go func() {
		refreshed, failed, _ := r.Refresh(context.Background(), channels)
		done <- result{refreshed, failed}
	}()
	// Two listings are in flight together (the bound is reached), and release lets everything finish.
	receive(t, arrived, "the first listing")
	receive(t, arrived, "the second listing")
	close(release)
	if got := receive(t, done, "Refresh"); got != (result{refreshed: 6}) {
		t.Fatalf("result: %+v", got)
	}
	if got := peak.Load(); got != 2 {
		t.Fatalf("peak parallelism %d, want exactly the bound of 2", got)
	}
}

func TestRefreshReturnsTheContextErrorWhenCancelled(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r, l := w.refresher(t, RefresherOptions{Concurrency: 1})
	channels := []*core.Record{w.staleChannel(t, "UCx1"), w.staleChannel(t, "UCx2"), w.staleChannel(t, "UCx3")}
	ctx, cancel := context.WithCancel(context.Background())
	l.mu.Lock()
	l.gate = func(ctx context.Context, _ string) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	l.mu.Unlock()
	refreshed, failed, err := r.Refresh(ctx, channels)
	if !errors.Is(err, context.Canceled) || refreshed != 0 || failed != 1 {
		t.Fatalf("refreshed=%d failed=%d err=%v", refreshed, failed, err)
	}
	if asked := l.askedIDs(); len(asked) != 1 {
		t.Fatalf("channels were listed after the cancellation: %v", asked)
	}
}

func TestScheduleQueuesStaleChannelsOnceWithoutBlocking(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r, l := w.refresher(t, RefresherOptions{})
	fresh := w.freshChannel(t, "UCfresh")
	stale := w.staleChannel(t, "UCstale")
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	l.mu.Lock()
	l.gate = func(ctx context.Context, _ string) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.mu.Unlock()

	// The listing blocks, yet Schedule returns at once, and asking again does not queue the channel twice.
	returned := make(chan struct{})
	go func() {
		r.Schedule([]*core.Record{fresh, stale})
		r.Schedule([]*core.Record{stale})
		close(returned)
	}()
	receive(t, returned, "Schedule to return while the listing is blocked")
	receive(t, entered, "the background listing")
	if got := r.queue.Unfinished(); got != 1 {
		t.Fatalf("one stale channel must be queued once: %d", got)
	}
	close(release)
	entry, err := r.queue.Wait(context.Background(), stale.Id)
	if err != nil || entry.Status != Complete {
		t.Fatalf("the background refresh did not complete: %+v %v", entry, err)
	}
	if got := l.askedIDs(); !slices.Equal(got, []string{"UCstale"}) {
		t.Fatalf("listed %v", got)
	}
	if NeedsRefresh(w.app, w.findChannel(t, alpha, "UCstale")) {
		t.Fatal("the background refresh did not renew the listing")
	}
	if _, queued := r.queue.Get(fresh.Id); queued {
		t.Fatal("a fresh channel was queued")
	}
}

func TestScheduleLeavesAFailedChannelAloneUntilTheRetryDelayPasses(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()) }
	r, l := w.refresher(t, RefresherOptions{Retry: time.Hour, Now: now})
	l.fail["UCflaky"] = true
	channel := w.staleChannel(t, "UCflaky")
	wait := func() {
		t.Helper()
		if entry, err := r.queue.Wait(context.Background(), channel.Id); err != nil || entry.Status != Failed {
			t.Fatalf("expected a failed job: %+v %v", entry, err)
		}
	}

	r.Schedule([]*core.Record{channel})
	wait()
	// Every read of the feed asks again; the channel is not listed again before the delay.
	r.Schedule([]*core.Record{channel})
	r.Schedule([]*core.Record{channel})
	if got := l.askedIDs(); len(got) != 1 {
		t.Fatalf("a failed channel was listed again inside its retry window: %v", got)
	}
	clock.Add(int64(time.Hour + time.Minute))
	l.mu.Lock()
	l.fail["UCflaky"] = false
	l.mu.Unlock()
	r.Schedule([]*core.Record{channel})
	if entry, err := r.queue.Wait(context.Background(), channel.Id); err != nil || entry.Status != Complete {
		t.Fatalf("the channel was not retried after the delay: %+v %v", entry, err)
	}
	if got := l.askedIDs(); len(got) != 2 {
		t.Fatalf("listed %v", got)
	}
}

func TestScheduleDoesNothingOnceTheOwnerStopped(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	l := &listings{fail: map[string]bool{}}
	w.alpha.listing = l.answer
	group := NewGroup()
	r := NewRefresher(w.app, w.registry, w.store, w.sources, group.Go, RefresherOptions{})
	channel := w.staleChannel(t, "UCstopped")
	group.Stop()
	r.Schedule([]*core.Record{channel})
	if got := l.askedIDs(); len(got) != 0 {
		t.Fatalf("a stopped owner ran a listing: %v", got)
	}
}

func TestStopCancelsARunningBackgroundRefresh(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	l := &listings{fail: map[string]bool{}}
	w.alpha.listing = l.answer
	entered := make(chan struct{}, 1)
	l.gate = func(ctx context.Context, _ string) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	group := NewGroup()
	r := NewRefresher(w.app, w.registry, w.store, w.sources, group.Go, RefresherOptions{})
	channel := w.staleChannel(t, "UCrunning")
	r.Schedule([]*core.Record{channel})
	receive(t, entered, "the background listing")
	stopped := make(chan struct{})
	go func() {
		group.Stop()
		close(stopped)
	}()
	receive(t, stopped, "Stop to return")
}

func mustVideo(t *testing.T, w *world, id string) *core.Record {
	t.Helper()
	record, err := w.store.FindVideo(alpha, id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
