package jobs

import (
	"context"
	"slices"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// CatalogOptions tunes Catalogs; a zero field takes the default.
type CatalogOptions struct {
	// Concurrency is how many catalogs are listed at once (default 2).
	Concurrency int
	// MaxUnfinished bounds the pending and running synchronizations (default 32): a request beyond it is
	// refused, never queued without bound.
	MaxUnfinished int
	// CacheLimit bounds the finished synchronizations remembered, and the connected catalogs kept in
	// memory (default 64).
	CacheLimit int
	// PublicLifetime is how long a complete public catalog counts as current (default 24 hours).
	PublicLifetime time.Duration
	// ConnectedLifetime is how long a connected catalog is served from memory (default 15 minutes).
	ConnectedLifetime time.Duration
	// ListingSize is how many videos the first listing of a channel asks for (default 30): the page it
	// shows while the whole catalog streams in.
	ListingSize int
	// BatchSize is how many videos one write transaction holds (default 100).
	BatchSize int
	// ProgressInterval is the least time between two notifications of a public catalog that is still
	// loading (default 5 seconds).
	ProgressInterval time.Duration
	// ScopeInterval is how often a running connected catalog checks that its connection is still the
	// current one (default 1 second).
	ScopeInterval time.Duration
	// MaxAttempts is how many times a synchronization runs when the platform rate-limits it (default 3).
	MaxAttempts int
	// RateLimitPause holds the queue back after a rate limit that did not say for how long (default 30
	// seconds).
	RateLimitPause time.Duration
	// RetryBase and RetryMax bound the pause before a failed public synchronization is tried again: it is
	// RetryBase after the first failure and doubles after each one, up to RetryMax (defaults 5 minutes and
	// 6 hours).
	RetryBase, RetryMax time.Duration
	// Now replaces the clock (tests).
	Now func() time.Time
}

func (o CatalogOptions) withDefaults() CatalogOptions {
	o.Concurrency = positive(o.Concurrency, 2)
	o.MaxUnfinished = positive(o.MaxUnfinished, 32)
	o.CacheLimit = positive(o.CacheLimit, 64)
	o.PublicLifetime = positiveDuration(o.PublicLifetime, 24*time.Hour)
	o.ConnectedLifetime = positiveDuration(o.ConnectedLifetime, 15*time.Minute)
	o.ListingSize = positive(o.ListingSize, 30)
	o.BatchSize = positive(o.BatchSize, 100)
	o.ProgressInterval = positiveDuration(o.ProgressInterval, 5*time.Second)
	o.ScopeInterval = positiveDuration(o.ScopeInterval, time.Second)
	o.MaxAttempts = positive(o.MaxAttempts, 3)
	o.RateLimitPause = positiveDuration(o.RateLimitPause, 30*time.Second)
	o.RetryBase = positiveDuration(o.RetryBase, defaultRetryBase)
	o.RetryMax = positiveDuration(o.RetryMax, defaultRetryMax)
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// CatalogKey names a catalog. A public catalog is the one everybody shares (UserID and Revision empty); a
// connected catalog is what one account's session lists, valid for exactly that connection (Revision is
// sources.Service.SessionVersion). ChannelID is a platform-native id in any of the forms of
// platform.Platform.ChannelIDAliases: Catalogs reads it by its canonical form.
type CatalogKey struct {
	Platform  string
	ChannelID string
	UserID    string
	Revision  string
}

// Connected tells whether the key names a connected catalog.
func (k CatalogKey) Connected() bool { return k.UserID != "" }

// Sync is where the synchronization of a catalog stands.
type Sync struct {
	Status Status `json:"status"`
	Loaded int    `json:"loaded"`
}

// Catalog is a channel with the videos of its catalog loaded so far, as the channel page shows it. The
// videos are addressed by record id when serialized (see platform.Video).
type Catalog struct {
	// Channel is nil while the channel has no profile yet.
	Channel *platform.Channel `json:"channel"`
	Videos  []platform.Video  `json:"videos"`
	Sync    Sync              `json:"sync"`
}

// SnapshotOptions tunes Catalogs.Snapshot.
type SnapshotOptions struct {
	// Retry starts again a synchronization that failed for good, or a connected one that failed (those are
	// not retried on their own: a connection is only used while a page asks for it).
	Retry bool
}

// CatalogEvent tells a listener that the catalog of a channel changed: it loaded more, or it finished
// (Done), successfully or not.
type CatalogEvent struct {
	Key  CatalogKey
	Done bool
}

// Catalogs synchronizes the whole catalog of channels, for every platform that can list one.
//
// A public catalog is persisted (channel_catalogs, channel_catalog_entries) so it survives a restart and can
// be read without running anything; its status is part of what is stored. A connected catalog is memory-only
// and belongs to one connection: nothing of it is shared with another account or another session.
// Every synchronization is a job of one bounded Queue.
type Catalogs struct {
	app       core.App
	platforms *platform.Registry
	store     *catalog.Store
	sources   *sources.Service
	spawn     cache.Spawner
	opts      CatalogOptions
	queue     *Queue[CatalogKey, CatalogKey, struct{}]

	mu        sync.Mutex
	connected map[CatalogKey]*connectedCatalog

	listenMu  sync.RWMutex
	listeners []func(CatalogEvent)
}

// NewCatalogs builds Catalogs. spawn owns the goroutines of the queue and of the connection watchers
// (jobs.Group.Go).
func NewCatalogs(app core.App, platforms *platform.Registry, store *catalog.Store, sources *sources.Service, spawn cache.Spawner, opts CatalogOptions) *Catalogs {
	opts = opts.withDefaults()
	c := &Catalogs{app: app, platforms: platforms, store: store, sources: sources, spawn: spawn, opts: opts, connected: map[CatalogKey]*connectedCatalog{}}
	c.queue = NewQueue(Options[CatalogKey, CatalogKey, struct{}]{
		Concurrency:   opts.Concurrency,
		MaxUnfinished: opts.MaxUnfinished,
		MaxFinished:   opts.CacheLimit,
		MaxAttempts:   opts.MaxAttempts,
		Spawn:         spawn,
		Now:           opts.Now,
		Code:          taskCode("catalog"),
		Retry:         rateLimitPause(opts.RateLimitPause),
		Backoff:       newBackoff(opts.RetryBase, opts.RetryMax),
		// A connected catalog only lives as long as the page that asks for it and the connection it was
		// made for: it is never retried behind the user's back.
		Final: func(key CatalogKey, err error) bool { return key.Connected() || permanentFailure(err) },
		Run: func(ctx context.Context, key CatalogKey, _ CatalogKey) (struct{}, error) {
			return struct{}{}, c.run(ctx, key)
		},
		OnFinish: c.finished,
		OnRetry:  c.retrying,
	})
	return c
}

// OnChange registers a listener called, without a lock held, whenever a public catalog loads more or
// finishes. Register before any work starts.
func (c *Catalogs) OnChange(listener func(CatalogEvent)) {
	c.listenMu.Lock()
	defer c.listenMu.Unlock()
	c.listeners = append(c.listeners, listener)
}

func (c *Catalogs) notify(event CatalogEvent) {
	c.listenMu.RLock()
	listeners := slices.Clone(c.listeners)
	c.listenMu.RUnlock()
	for _, listener := range listeners {
		listener(event)
	}
}

func (c *Catalogs) finished(key CatalogKey, entry Entry[CatalogKey, struct{}]) {
	if entry.Status == Failed {
		logging.Of(c.app).Warn("Channel catalog synchronization failed", "platform", key.Platform, "connected", key.Connected(), "code", entry.Code)
	}
	if !key.Connected() {
		c.notify(CatalogEvent{Key: key, Done: true})
	}
}

// retrying logs a synchronization that failed and will be tried again.
func (c *Catalogs) retrying(key CatalogKey, entry Entry[CatalogKey, struct{}]) {
	logging.Of(c.app).Warn("Channel catalog synchronization failed, trying again later", "platform", key.Platform, "code", entry.Code, "failures", entry.Failures, "retryAt", entry.RetryAt)
}

// Unfinished is the number of catalogs being synchronized or waiting for it.
func (c *Catalogs) Unfinished() int { return c.queue.Unfinished() }

// ConnectedKey is the key of the catalog the account's current connection lists: the revision is the one
// of the connection as it is now. It fails like sources.Service.SessionVersion when there is no connection.
func (c *Catalogs) ConnectedKey(userID, platformID, channelID string) (CatalogKey, error) {
	revision, err := c.sources.SessionVersion(userID, platformID)
	if err != nil {
		return CatalogKey{}, err
	}
	return CatalogKey{Platform: platformID, ChannelID: channelID, UserID: userID, Revision: revision}, nil
}

// ScopeCurrent checks that a connected key still names the account's current connection: a connection that
// was replaced or removed invalidates everything bound to it (apierr SessionInvalid, or the error of the
// lookup). A public key is always current.
func (c *Catalogs) ScopeCurrent(key CatalogKey) error {
	if !key.Connected() {
		return nil
	}
	revision, err := c.sources.SessionVersion(key.UserID, key.Platform)
	if err != nil {
		return err
	}
	if revision != key.Revision {
		return apierr.SessionInvalid()
	}
	return nil
}

// resolve normalizes a key: the platform must exist and list catalogs, and the channel id takes its
// canonical form.
func (c *Catalogs) resolve(key CatalogKey) (CatalogKey, platform.Platform, error) {
	p, ok := c.platforms.Get(key.Platform)
	if !ok {
		return key, nil, apierr.InvalidInput("Unknown video platform.")
	}
	if p.CatalogSync() == nil {
		return key, nil, apierr.InvalidInput("This source cannot list the whole catalog of a channel.")
	}
	if aliases := p.ChannelIDAliases(key.ChannelID); len(aliases) > 0 {
		key.ChannelID = aliases[0]
	}
	if key.ChannelID == "" {
		return key, nil, apierr.InvalidInput("Provide a channel.")
	}
	return key, p, nil
}

// canonicalKey is the public key of a channel record, or false when its platform lists no catalog.
func (c *Catalogs) canonicalKey(channel *core.Record) (CatalogKey, bool) {
	key, _, err := c.resolve(CatalogKey{Platform: channel.GetString("platform"), ChannelID: channel.GetString("platform_id")})
	return key, err == nil
}

// Snapshot answers the page of a channel: the videos of its catalog loaded so far and where the
// synchronization stands. It starts the synchronization when the catalog has none that is current: a
// complete one is current for its lifetime, a failed one is tried again by the queue (opts.Retry only
// asks for the ones that failed for good again), and a refused one (too many unfinished jobs) answers with
// an error status. It never waits for the platform.
func (c *Catalogs) Snapshot(key CatalogKey, opts SnapshotOptions) (Catalog, error) {
	key, _, err := c.resolve(key)
	if err != nil {
		return Catalog{}, err
	}
	if key.Connected() {
		return c.snapshotConnected(key, opts.Retry)
	}
	admitted := c.ensurePublic(key, opts.Retry)
	out, err := c.Stored(key.Platform, key.ChannelID)
	if err != nil {
		return Catalog{}, apierr.Internal("Failed to get channel data", err)
	}
	if !admitted {
		out.Sync.Status = Failed
	}
	return out, nil
}

// Ensure makes sure the public catalog of the channel has a synchronization that is current (see
// Snapshot) and reports where it stands: Failed only for a synchronization that failed for good, which is
// not retried. deferred is true when the work could not be queued because the queue is full: the caller is
// expected to ask again once a job finished.
func (c *Catalogs) Ensure(key CatalogKey) (status Status, deferred bool, err error) {
	key, _, err = c.resolve(key)
	if err != nil {
		return Pending, false, err
	}
	admitted := c.ensurePublic(key, false)
	row, err := c.row(c.app, key)
	if err != nil {
		return Pending, false, err
	}
	return c.statusOf(key, row), !admitted, nil
}
