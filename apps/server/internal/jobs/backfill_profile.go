package jobs

import (
	"context"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// ProfileBackfill fills in the avatar and subscriber count of cached channels whose listing omitted them.
type ProfileBackfill struct {
	app       core.App
	platforms *platform.Registry
	store     *catalog.Store
	sources   *sources.Service
	wake      *Signal
	opts      BackfillOptions
	// attempted remembers when each channel was last looked up. Only the goroutine of Run touches it.
	attempted map[string]time.Time
}

// NewProfileBackfill builds the worker. wake is the signal the catalog store fires when it leaves a
// channel without a profile (catalog.Options.OnProfilesWanted); nil means nobody wakes it.
func NewProfileBackfill(app core.App, platforms *platform.Registry, store *catalog.Store, sources *sources.Service, wake *Signal, opts BackfillOptions) *ProfileBackfill {
	if wake == nil {
		wake = NewSignal()
	}
	return &ProfileBackfill{
		app: app, platforms: platforms, store: store, sources: sources, wake: wake,
		opts:      opts.withDefaults(0),
		attempted: map[string]time.Time{},
	}
}

// Run is the worker loop; it returns when ctx ends. It must be run by one goroutine at a time.
func (b *ProfileBackfill) Run(ctx context.Context) {
	runBackfill(ctx, b.opts, b.wake, b.pass)
}

// missing returns cached channels without an avatar or subscriber count that were not attempted
// recently: subscribed channels first, then the most recently seen.
func (b *ProfileBackfill) missing(limit int) ([]*core.Record, error) {
	var candidates []*core.Record
	err := candidateQuery(b.app, "channels",
		dbx.NewExp("(thumbnail = '' OR thumbnail IS NULL OR subscriber_count IS NULL OR subscriber_count = 0)"),
		"id IN (SELECT channel FROM subscriptions)", "updated",
		platformIDsInUse(b.sources), limit).
		All(&candidates)
	if err != nil {
		return nil, err
	}
	return untried(candidates, b.attempted, b.opts.Retry, limit), nil
}

// pass looks up one batch of incomplete channels and returns how many it tried.
func (b *ProfileBackfill) pass(ctx context.Context) int {
	channels, err := b.missing(b.opts.Batch)
	if err != nil {
		logging.Of(b.app).Warn("Failed to list channels without profile", "error", err)
		return 0
	}
	started := forEachBounded(ctx, channels, b.opts.Concurrency,
		func(channel *core.Record) { b.attempted[channel.Id] = time.Now() },
		func(channel *core.Record) {
			b.fetch(ctx, channel.GetString("platform"), channel.GetString("platform_id"))
		})
	if !started {
		return 0
	}
	return len(channels)
}

// fetch lists one entry of the channel and stores its profile. The one-entry listing caches no video, so
// the channel must not look freshly listed: that is StoreChannelProfile, not StoreChannelListing.
func (b *ProfileBackfill) fetch(ctx context.Context, platformID, nativeID string) {
	p, ok := b.platforms.Get(platformID)
	if !ok {
		return
	}
	if b.opts.FetchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.opts.FetchTimeout)
		defer cancel()
	}
	info, _, err := p.ChannelListing(ctx, nativeID, 1, "")
	if err != nil {
		if ctx.Err() == nil {
			logging.Of(b.app).Warn("Failed to fetch channel profile", "id", nativeID, "platform", platformID, "error", err)
		}
		return
	}
	if err := b.store.StoreChannelProfile(&info, ""); err != nil {
		logging.Of(b.app).Warn("Failed to cache channel profile", "id", nativeID, "error", err)
	}
}
