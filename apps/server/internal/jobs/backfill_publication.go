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

// PublicationBackfill dates the cached videos whose listing carried no publication date, from the video's
// own page: listings often omit it (a flat yt-dlp playlist, a search result).
type PublicationBackfill struct {
	app       core.App
	platforms *platform.Registry
	store     *catalog.Store
	sources   *sources.Service
	wake      *Signal
	opts      BackfillOptions
	// attempted remembers when each video was last looked up. Only the goroutine of Run touches it.
	attempted map[string]time.Time
}

// NewPublicationBackfill builds the worker. wake is the signal the catalog store fires when it leaves a
// video undated (catalog.Options.OnPublicationsWanted); nil means nobody wakes it.
func NewPublicationBackfill(app core.App, platforms *platform.Registry, store *catalog.Store, sources *sources.Service, wake *Signal, opts BackfillOptions) *PublicationBackfill {
	if wake == nil {
		wake = NewSignal()
	}
	return &PublicationBackfill{
		app: app, platforms: platforms, store: store, sources: sources, wake: wake,
		opts:      opts.withDefaults(45 * time.Second),
		attempted: map[string]time.Time{},
	}
}

// Run is the worker loop; it returns when ctx ends. It must be run by one goroutine at a time.
func (b *PublicationBackfill) Run(ctx context.Context) {
	runBackfill(ctx, b.opts, b.wake, b.pass)
}

// missing returns cached videos without a publication date that were not attempted recently: subscribed
// channels first, then the most recently stored. A stream in progress has no date to find yet.
func (b *PublicationBackfill) missing(limit int) ([]*core.Record, error) {
	var candidates []*core.Record
	err := candidateQuery(b.app, "videos",
		dbx.NewExp("(published = '' OR published IS NULL) AND COALESCE(live, 0) = 0"),
		"channel IN (SELECT channel FROM subscriptions)", "created",
		platformIDsInUse(b.sources), limit).
		All(&candidates)
	if err != nil {
		return nil, err
	}
	return untried(candidates, b.attempted, b.opts.Retry, limit), nil
}

// pass fetches one batch of undated videos and returns how many it tried.
func (b *PublicationBackfill) pass(ctx context.Context) int {
	videos, err := b.missing(b.opts.Batch)
	if err != nil {
		logging.Of(b.app).Warn("Failed to list videos without publication date", "error", err)
		return 0
	}
	started := forEachBounded(ctx, videos, b.opts.Concurrency,
		func(video *core.Record) { b.attempted[video.Id] = time.Now() },
		func(video *core.Record) { b.fetch(ctx, video.GetString("platform"), video.GetString("platform_id")) })
	if !started {
		return 0
	}
	return len(videos)
}

// fetch stores the full metadata of a video the way a viewer opening it would, which fills its
// publication date.
func (b *PublicationBackfill) fetch(ctx context.Context, platformID, nativeID string) {
	p, ok := b.platforms.Get(platformID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, b.opts.FetchTimeout)
	defer cancel()
	video, err := p.VideoMetadata(ctx, nativeID, "")
	if err != nil {
		if ctx.Err() == nil {
			logging.Of(b.app).Warn("Failed to fetch video publication date", "id", nativeID, "platform", platformID, "error", err)
		}
		return
	}
	if err := b.store.StoreVideo(&video, ""); err != nil {
		logging.Of(b.app).Warn("Failed to cache video publication date", "id", nativeID, "platform", platformID, "error", err)
	}
}
