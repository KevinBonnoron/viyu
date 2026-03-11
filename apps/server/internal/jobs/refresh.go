package jobs

import (
	"context"
	"slices"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// RefresherOptions tunes a Refresher; a zero field takes the default.
type RefresherOptions struct {
	// Concurrency bounds the channels listed at once, by Refresh and by the background queue (default 4).
	Concurrency int
	// ListingSize is how many videos of a channel one listing asks for (default 30).
	ListingSize int
	// Retry is how long a channel whose background refresh ended, successfully or not, is left alone
	// before Schedule queues it again (default 10 minutes). A channel that lists nothing never looks fresh,
	// and a channel that fails would otherwise be listed again on every read of the feed.
	Retry time.Duration
	// Timeout bounds one background refresh (default 2 minutes).
	Timeout time.Duration
	// Now replaces the clock (tests).
	Now func() time.Time
}

// staleBatch bounds the ids of one freshness query.
const staleBatch = 500

// refreshTarget is what a background refresh needs to list a channel.
type refreshTarget struct{ platform, id string }

// Refresher keeps the listings of subscribed channels fresh: a blocking refresh for the caller who wants
// the answer, and a background one for reads that must never wait for a downloader.
type Refresher struct {
	app       core.App
	platforms *platform.Registry
	store     *catalog.Store
	sources   *sources.Service
	opts      RefresherOptions
	now       func() time.Time
	queue     *Queue[string, refreshTarget, struct{}]
}

// NewRefresher builds a Refresher. spawn owns the goroutines of the background queue (jobs.Group.Go).
func NewRefresher(app core.App, platforms *platform.Registry, store *catalog.Store, sources *sources.Service, spawn cache.Spawner, opts RefresherOptions) *Refresher {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.ListingSize <= 0 {
		opts.ListingSize = 30
	}
	if opts.Retry <= 0 {
		opts.Retry = 10 * time.Minute
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	r := &Refresher{app: app, platforms: platforms, store: store, sources: sources, opts: opts, now: opts.Now}
	if r.now == nil {
		r.now = time.Now
	}
	r.queue = NewQueue(Options[string, refreshTarget, struct{}]{
		Concurrency:   opts.Concurrency,
		MaxUnfinished: 256,
		MaxFinished:   512,
		Timeout:       opts.Timeout,
		Spawn:         spawn,
		Now:           opts.Now,
		Run: func(ctx context.Context, _ string, target refreshTarget) (struct{}, error) {
			_, _, err := r.Channel(ctx, target.platform, target.id)
			return struct{}{}, err
		},
		OnFinish: func(_ string, entry Entry[refreshTarget, struct{}]) {
			if entry.Status == Failed {
				logging.Of(r.app).Warn("Failed to refresh subscribed channel", "id", entry.In.id, "platform", entry.In.platform, "code", entry.Code)
			}
		},
	})
	return r
}

// Channel lists a public channel's latest videos and caches both the channel and its videos. A failure
// is an *apierr.Error.
func (r *Refresher) Channel(ctx context.Context, platformID, nativeID string) (platform.Channel, []platform.Video, error) {
	p, ok := r.platforms.Get(platformID)
	if !ok {
		return platform.Channel{}, nil, apierr.InvalidInput("Unknown video platform.")
	}
	info, videos, err := p.ChannelListing(ctx, nativeID, r.opts.ListingSize, "")
	if err != nil {
		return platform.Channel{}, nil, apierr.From(err)
	}
	p.EnrichListing(ctx, info.ID, videos)
	if err := r.store.StoreVideos(videos, ""); err != nil {
		logging.Of(r.app).Warn("Failed to cache channel videos", "id", nativeID, "platform", platformID, "error", err)
	}
	platform.SortByPublication(videos)
	if err := r.store.StoreChannelListing(&info, ""); err != nil {
		logging.Of(r.app).Warn("Failed to cache channel info", "id", nativeID, "platform", platformID, "error", err)
	}
	return info, videos, nil
}

// NeedsRefresh reports whether a subscribed channel has no usable cached listing: its expiry is in the
// past, or none of its videos is fresh.
func NeedsRefresh(app core.App, channel *core.Record) bool {
	if !channel.GetDateTime("expires_at").Time().After(time.Now()) {
		return true
	}
	now := time.Now().UTC().Format(platform.StoredLayout)
	fresh, err := app.CountRecords("videos", dbx.HashExp{"channel": channel.Id}, dbx.NewExp("expires_at > {:now}", dbx.Params{"now": now}))
	return err != nil || fresh == 0
}

// stale returns the channels of a known platform that need a refresh, in order. The freshness of the
// videos of the channels with a live expiry is read with one query per batch rather than one each.
func (r *Refresher) stale(channels []*core.Record) []*core.Record {
	now := r.now()
	var known []*core.Record
	var live []any // channels whose expiry is in the future: only their videos tell
	for _, channel := range channels {
		if _, ok := r.platforms.Get(channel.GetString("platform")); !ok {
			continue
		}
		known = append(known, channel)
		if channel.GetDateTime("expires_at").Time().After(now) {
			live = append(live, channel.Id)
		}
	}
	// A failed query counts as no fresh video, like NeedsRefresh.
	hasFresh := map[string]bool{}
	cutoff := now.UTC().Format(platform.StoredLayout)
	for chunk := range slices.Chunk(live, staleBatch) {
		var rows []struct {
			Channel string `db:"channel"`
		}
		err := r.app.DB().Select("channel").Distinct(true).From("videos").
			Where(dbx.In("channel", chunk...)).
			AndWhere(dbx.NewExp("expires_at > {:now}", dbx.Params{"now": cutoff})).
			All(&rows)
		if err != nil {
			continue
		}
		for _, row := range rows {
			hasFresh[row.Channel] = true
		}
	}
	var needing []*core.Record
	for _, channel := range known {
		if !channel.GetDateTime("expires_at").Time().After(now) || !hasFresh[channel.Id] {
			needing = append(needing, channel)
		}
	}
	return needing
}

// Refresh lists the stale channels with bounded parallelism and waits for them. A failing channel is
// counted and never blocks the others; only the cancellation of ctx is returned as an error.
func (r *Refresher) Refresh(ctx context.Context, channels []*core.Record) (refreshed, failed int, err error) {
	var mu sync.Mutex
	forEachBounded(ctx, r.stale(channels), r.opts.Concurrency, nil, func(channel *core.Record) {
		id := channel.GetString("platform_id")
		_, _, refreshErr := r.Channel(ctx, channel.GetString("platform"), id)
		mu.Lock()
		defer mu.Unlock()
		if refreshErr != nil {
			logging.Of(r.app).Warn("Failed to refresh subscribed channel", "id", id, "error", refreshErr)
			failed++
		} else {
			refreshed++
		}
	})
	return refreshed, failed, ctx.Err()
}

// Schedule queues the stale channels for a background refresh and returns at once: a read of the feed or
// of the recommendations must never wait for a downloader. A channel that already has a refresh pending or
// running is not queued twice, one refreshed or failed a moment ago is left alone (see
// RefresherOptions.Retry), and what the queue cannot hold is dropped: the next read asks again.
func (r *Refresher) Schedule(channels []*core.Record) {
	for _, channel := range r.stale(channels) {
		if entry, known := r.queue.Get(channel.Id); known && entry.Status.Terminal() && r.now().Sub(entry.Updated) < r.opts.Retry {
			continue
		}
		r.queue.Restart(channel.Id, refreshTarget{platform: channel.GetString("platform"), id: channel.GetString("platform_id")})
	}
}
