package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/chat"
	"github.com/KevinBonnoron/viyu/apps/server/internal/comments"
	"github.com/KevinBonnoron/viyu/apps/server/internal/feed"
	"github.com/KevinBonnoron/viyu/apps/server/internal/httpapi"
	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/live"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/media"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/all"
	"github.com/KevinBonnoron/viyu/apps/server/internal/search"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series/ocr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/stats"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// JobOptions tunes the background workers; the zero value is production behaviour.
type JobOptions struct {
	Profiles     jobs.BackfillOptions
	Publications jobs.BackfillOptions
	Refresher    jobs.RefresherOptions
	Catalogs     jobs.CatalogOptions
	Analysis     jobs.AnalysisOptions
}

// janitorEvery is how often the in-memory caches drop what expired.
const janitorEvery = time.Minute

// running is what a started application holds on to.
type running struct {
	group     *jobs.Group
	analysis  *jobs.Analysis
	platforms *platform.Registry
}

// composition is the started application, set once the server starts. The PocketBase hooks registered
// before that read it through an atomic pointer and do nothing while it is empty.
type composition struct {
	current atomic.Pointer[running]
}

// subscribed tells the analysis about a new subscription: a channel whose platform lists a catalog has series
// to find. It runs inside PocketBase's record hook, so it only wakes the worker.
func (c *composition) subscribed(app core.App, subscription *core.Record) {
	r := c.current.Load()
	if r == nil {
		return
	}
	channel, err := app.FindRecordById("channels", subscription.GetString("channel"))
	if err != nil {
		return
	}
	if p, ok := r.platforms.Get(channel.GetString("platform")); ok && p.CatalogSync() != nil {
		r.analysis.Wake(channel.Id)
	}
}

// stop cancels the background work and every request in flight, and waits for them.
func (c *composition) stop() {
	if r := c.current.Load(); r != nil {
		r.group.Stop()
	}
}

// start composes the services over the application, registers the routes and starts the background work.
func (c *composition) start(se *core.ServeEvent, opts Options) error {
	app := se.App
	group := jobs.NewGroup()
	runnerOptions := opts.YTDLP
	if runnerOptions.CookieDir == "" {
		runnerOptions.CookieDir = filepath.Join(app.DataDir(), "source-cookies")
	}
	runner := ytdlp.New(runnerOptions)
	log := logging.Of(app)

	opts.YouTube.Runner, opts.YouTube.Log = runner, log
	opts.Twitch.Runner, opts.Twitch.Log = runner, log
	opts.PeerTube.Log = log
	platforms := all.Registry(all.Options{YouTube: opts.YouTube, Twitch: opts.Twitch, PeerTube: opts.PeerTube})

	accountSources, err := sources.New(app, platforms, runner, sources.Options{})
	if err != nil {
		return err
	}
	profilesWake, publicationsWake := jobs.NewSignal(), jobs.NewSignal()
	store := catalog.New(app, platforms, catalog.Options{OnProfilesWanted: profilesWake.Wake, OnPublicationsWanted: publicationsWake.Wake})

	refresher := jobs.NewRefresher(app, platforms, store, accountSources, group.Go, opts.Jobs.Refresher)
	catalogs := jobs.NewCatalogs(app, platforms, store, accountSources, group.Go, opts.Jobs.Catalogs)
	parser := series.NewParser(platformNames(platforms))
	seriesStore := series.NewStore(app, store, platforms, parser)
	reader := opts.OCR
	if reader == nil {
		reader = ocr.New(ocr.Options{})
	}
	analysis := jobs.NewAnalysis(app, platforms, store, accountSources, seriesStore, parser, reader, catalogs, group.Go, opts.Jobs.Analysis)

	mediaProxy := media.New(media.Options{Platforms: platforms, Cookies: accountSources, HTTP: opts.MediaHTTP, Spawn: group.Go})
	chatOptions := opts.Chat
	chatOptions.Spawn = group.Go
	commentsService := comments.New(comments.Options{Platforms: platforms, Cookies: accountSources, Spawn: group.Go})
	liveService := live.New(live.Options{App: app, Platforms: platforms, Store: store, Spawn: group.Go, Log: log})

	httpapi.New(httpapi.Deps{
		App:           app,
		Group:         group,
		Platforms:     platforms,
		Store:         store,
		Sources:       accountSources,
		Media:         mediaProxy,
		Chat:          chat.New(chatOptions),
		Comments:      commentsService,
		Live:          liveService,
		Search:        search.New(platforms, store, log),
		Accounts:      accounts.New(app, store, accountSources, platforms),
		Subscriptions: subscriptions.New(app, store, accountSources, platforms),
		Feed:          feed.New(app, store, accountSources, refresher),
		Stats:         stats.New(app, store, accountSources),
		Refresher:     refresher,
		Catalogs:      catalogs,
		Analysis:      analysis,
		Series:        seriesStore,
		OCR:           reader,
	}).Register(se)

	if !opts.DisableChannelProfiles {
		group.Go(jobs.NewProfileBackfill(app, platforms, store, accountSources, profilesWake, opts.Jobs.Profiles).Run)
		group.Go(jobs.NewPublicationBackfill(app, platforms, store, accountSources, publicationsWake, opts.Jobs.Publications).Run)
	}
	group.Go(analysis.Run)
	group.Go(func(ctx context.Context) {
		janitor(ctx, janitorEvery, mediaProxy.Purge, commentsService.Purge, liveService.Purge)
	})
	c.current.Store(&running{group: group, analysis: analysis, platforms: platforms})
	return nil
}

// platformNames lists the lowercase display names of the platforms: a title that merely names a platform is no
// series name.
func platformNames(platforms *platform.Registry) []string {
	names := make([]string, 0, len(platforms.All()))
	for _, p := range platforms.All() {
		names = append(names, strings.ToLower(p.Descriptor().Name))
	}
	return names
}
