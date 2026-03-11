package jobs

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// datedMetadata answers every video like a page that knows its date, and records which ids it was asked for.
func datedMetadata(asked *[]string, mu *sync.Mutex) func(context.Context, string) (platform.Video, error) {
	return func(_ context.Context, id string) (platform.Video, error) {
		mu.Lock()
		*asked = append(*asked, id)
		mu.Unlock()
		return platform.Video{
			ID: id, Platform: alpha, Title: "Full title", ChannelID: "UCsubscribed", ChannelName: "Subscribed",
			Published: "2026-03-04", Duration: 10, FullMetadata: true,
		}, nil
	}
}

func (w *world) publishedOf(t *testing.T, id string) string {
	t.Helper()
	record, err := w.store.FindVideo(alpha, id)
	if err != nil {
		t.Fatal(err)
	}
	return catalog.PublishedOf(record)
}

func TestPublicationBackfillFillsUndatedVideosAndDoesNotLoopOnFailure(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.subscribe(t, w.channel(t, alpha, "UCsubscribed", "Subscribed", ""))

	subscribed := w.video(t, platform.Video{ID: "subscribed01", Platform: alpha, Title: "Listed", ChannelID: "UCsubscribed", ChannelName: "Subscribed"})
	w.video(t, platform.Video{ID: "other000001", Platform: alpha, Title: "Listed", ChannelID: "UCother", ChannelName: "Other"})
	w.video(t, platform.Video{ID: "dated000001", Platform: alpha, Title: "Dated", ChannelID: "UCother", ChannelName: "Other", Published: "2025-01-01"})
	w.video(t, platform.Video{ID: "stream00001", Platform: alpha, Title: "Stream", ChannelID: "UCother", ChannelName: "Other", Live: true})
	w.video(t, platform.Video{ID: "unused00001", Platform: beta, Title: "Unused platform", ChannelID: "UCother", ChannelName: "Other"})
	// The subscribed channel's video is the oldest, so only the subscription puts it ahead.
	if _, err := w.app.DB().NewQuery("UPDATE videos SET created = '2000-01-01 00:00:00.000Z' WHERE id = {:id}").Bind(map[string]any{"id": subscribed.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	b := NewPublicationBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{})

	missing, err := b.missing(10)
	if err != nil || !slices.Equal(ids(missing), []string{"subscribed01", "other000001"}) {
		t.Fatalf("undated videos must be the two listed ones, the subscribed channel's first (a stream, a dated video and a platform nobody uses are left out): %v %v", ids(missing), err)
	}

	// A failing fetch changes nothing and is not repeated before the retry delay.
	ctx := context.Background()
	if tried := b.pass(ctx); tried != 2 {
		t.Fatalf("both undated videos should be tried: %d", tried)
	}
	if got := w.publishedOf(t, "subscribed01") + w.publishedOf(t, "other000001"); got != "" {
		t.Fatalf("a failed fetch invented a date: %q", got)
	}
	var asked []string
	var mu sync.Mutex
	w.alpha.metadata = datedMetadata(&asked, &mu)
	if tried := b.pass(ctx); tried != 0 || len(asked) != 0 {
		t.Fatalf("recent attempts were repeated: tried=%d asked=%v", tried, asked)
	}

	// After the delay one pass dates both, through the normal store path.
	for id := range b.attempted {
		b.attempted[id] = time.Now().Add(-b.opts.Retry - time.Minute)
	}
	if tried := b.pass(ctx); tried != 2 {
		t.Fatalf("expired attempts were not retried: %d", tried)
	}
	for _, id := range []string{"subscribed01", "other000001"} {
		if got := w.publishedOf(t, id); got != "2026-03-04" {
			t.Fatalf("video %s was not dated: %q", id, got)
		}
		// The fetch stored the whole page, not just the date.
		record, err := w.store.FindVideo(alpha, id)
		if err != nil {
			t.Fatal(err)
		}
		if title, _ := catalog.TitlesOf(record); title != "Full title" || record.GetFloat("duration") != 10 {
			t.Fatalf("video %s did not get its full metadata: %q %v", id, title, record.GetFloat("duration"))
		}
	}
	if got := w.publishedOf(t, "dated000001"); got != "2025-01-01" {
		t.Fatalf("a dated video was touched: %q", got)
	}
	// Nothing is left to date.
	if tried := b.pass(ctx); tried != 0 {
		t.Fatalf("dated videos were fetched again: %d", tried)
	}
}

func TestPublicationBackfillFetchGivesUpAtItsTimeout(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.video(t, platform.Video{ID: "slow0000001", Platform: alpha, Title: "Slow", ChannelID: "UCslow", ChannelName: "Slow"})
	started := make(chan struct{})
	w.alpha.metadata = func(ctx context.Context, _ string) (platform.Video, error) {
		close(started)
		<-ctx.Done()
		return platform.Video{}, errors.Join(ctx.Err(), errors.New("killed"))
	}
	b := NewPublicationBackfill(w.app, w.registry, w.store, w.sources, nil, BackfillOptions{FetchTimeout: 20 * time.Millisecond})
	if tried := b.pass(context.Background()); tried != 1 {
		t.Fatalf("tried %d", tried)
	}
	receive(t, started, "the lookup")
	if got := w.publishedOf(t, "slow0000001"); got != "" {
		t.Fatalf("a lookup that timed out dated the video: %q", got)
	}
}

func TestPublicationBackfillRunWorksOnWakeAndStopsWithItsContext(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	asked := make(chan string, 8)
	w.alpha.metadata = func(_ context.Context, id string) (platform.Video, error) {
		asked <- id
		return platform.Video{ID: id, Platform: alpha, Title: id, ChannelID: "UCrun", ChannelName: "Run", Published: "2026-03-04", FullMetadata: true}, nil
	}
	b := NewPublicationBackfill(w.app, w.registry, w.store, w.sources, w.dates, BackfillOptions{StartDelay: time.Hour, IdleInterval: time.Hour, BusyInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Run(ctx)
	}()
	// Storing an undated listing wakes the worker, which otherwise waits an hour.
	w.video(t, platform.Video{ID: "listed00001", Platform: alpha, Title: "Listed", ChannelID: "UCrun", ChannelName: "Run"})
	if got := receive(t, asked, "the woken pass"); got != "listed00001" {
		t.Fatalf("woken pass looked up %q", got)
	}
	cancel()
	receive(t, done, "Run to return")
}
