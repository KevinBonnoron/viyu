package jobs

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// A connected catalog is what one account's session lists. It is deliberately memory-only: persisting it
// would put an account's view of a channel (private or members-only videos) in shared collections. It is
// keyed by the exact connection revision, so reconnecting never serves what the previous session saw, and
// everything bound to a connection that was replaced or removed is cancelled and dropped.

// connectedCatalog is what a connected synchronization has listed so far. Catalogs.mu guards it.
type connectedCatalog struct {
	channel  *platform.Channel
	videos   map[string]platform.Video // by record id
	accessed time.Time
}

func (c *Catalogs) snapshotConnected(key CatalogKey, retry bool) (Catalog, error) {
	c.sweep(key)
	state := c.connectedState(key)
	status := c.ensureConnected(key, retry)
	out := c.connectedResponse(state)
	out.Sync.Status = status
	return out, nil
}

// connectedState returns the state of a key, created with the channel as the account may see it.
func (c *Catalogs) connectedState(key CatalogKey) *connectedCatalog {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.connected[key]
	if state == nil {
		state = &connectedCatalog{videos: map[string]platform.Video{}}
		if record, err := c.store.FindChannel(key.Platform, key.ChannelID); err == nil && catalog.VisibleTo(record, key.UserID) {
			state.channel = catalog.ChannelOf(record)
		}
		c.connected[key] = state
	}
	state.accessed = c.opts.Now()
	return state
}

// ensureConnected starts the synchronization of a connected catalog unless it is current.
func (c *Catalogs) ensureConnected(key CatalogKey, retry bool) Status {
	if entry, ok := c.queue.Get(key); ok {
		switch {
		case !entry.Status.Terminal():
			return entry.Status
		case entry.Status == Failed && !retry:
			return Failed
		case entry.Status == Complete && c.opts.Now().Sub(entry.Updated) < c.opts.ConnectedLifetime:
			return Complete
		}
	}
	if _, admission := c.queue.Restart(key, key); admission == Refused {
		return Failed
	}
	return Pending
}

func (c *Catalogs) connectedResponse(state *connectedCatalog) Catalog {
	c.mu.Lock()
	defer c.mu.Unlock()
	videos := make([]platform.Video, 0, len(state.videos))
	for _, v := range state.videos {
		videos = append(videos, v)
	}
	slices.SortFunc(videos, func(a, b platform.Video) int {
		if byDate := platform.ComparePublished(b.Published, a.Published); byDate != 0 {
			return byDate
		}
		return cmp.Compare(a.RecordID, b.RecordID)
	})
	out := Catalog{Videos: videos, Sync: Sync{Loaded: len(videos)}}
	if state.channel != nil {
		channel := *state.channel
		out.Channel = &channel
	}
	return out
}

// sweep drops the connected catalogs that cannot be served any more: the ones of the same account and
// platform bound to another connection than current's, the ones nobody read for their lifetime, and the
// least recently read ones beyond the cache limit. Their jobs are cancelled.
func (c *Catalogs) sweep(current CatalogKey) {
	now := c.opts.Now()
	c.mu.Lock()
	var dropped []CatalogKey
	var idle []CatalogKey
	for key, state := range c.connected {
		entry, known := c.queue.Get(key)
		active := known && !entry.Status.Terminal()
		switch {
		case key.UserID == current.UserID && key.Platform == current.Platform && key.Revision != current.Revision:
			dropped = append(dropped, key)
		case !active && now.Sub(state.accessed) > c.opts.ConnectedLifetime:
			dropped = append(dropped, key)
		case !active && key != current:
			idle = append(idle, key)
		}
	}
	slices.SortFunc(idle, func(a, b CatalogKey) int { return c.connected[a].accessed.Compare(c.connected[b].accessed) })
	for len(c.connected)-len(dropped) >= c.opts.CacheLimit && len(idle) > 0 {
		dropped, idle = append(dropped, idle[0]), idle[1:]
	}
	for _, key := range dropped {
		delete(c.connected, key)
	}
	c.mu.Unlock()
	if len(dropped) > 0 {
		c.queue.Forget(func(key CatalogKey) bool { return slices.Contains(dropped, key) })
	}
}

// runConnected lists a channel as the account of the key, with a cookie jar of its own that lives as long
// as the job, and keeps what it lists in memory. The job gives up when the connection stops being the
// current one.
func (c *Catalogs) runConnected(ctx context.Context, key CatalogKey) error {
	err := c.syncConnected(ctx, key)
	if c.ScopeCurrent(key) != nil {
		c.drop(key)
	}
	return err
}

func (c *Catalogs) syncConnected(ctx context.Context, key CatalogKey) error {
	p, err := c.platformOf(key)
	if err != nil {
		return err
	}
	if err := c.ScopeCurrent(key); err != nil {
		return err
	}
	cookieFile, cleanup, err := c.sources.CookieFile(key.UserID, key.Platform)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := c.ScopeCurrent(key); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop, err := c.watchScope(ctx, key, cancel)
	if err != nil {
		return err
	}
	defer stop()
	info, initial, err := c.list(ctx, p, key, cookieFile)
	if err != nil {
		return err
	}
	// The channel is stored like any other, owned by the account when it is new: the page addresses it
	// by record id.
	if err := c.store.StoreChannelListing(&info, key.UserID); err != nil {
		return err
	}
	state := c.connectedState(key)
	c.mu.Lock()
	state.channel = &info
	c.mu.Unlock()
	listed := map[string]struct{}{}
	cr := crawl{platform: p, channel: info, cookieFile: cookieFile, size: c.opts.BatchSize, flush: func(batch []platform.Video) error {
		return c.keep(state, key, batch, listed)
	}}
	if err := cr.run(ctx, initial); err != nil {
		return err
	}
	if err := c.ScopeCurrent(key); err != nil {
		return err
	}
	c.forgetUnlisted(state, listed)
	return nil
}

// keep stores a batch of videos for the account and remembers them in the state.
func (c *Catalogs) keep(state *connectedCatalog, key CatalogKey, batch []platform.Video, listed map[string]struct{}) error {
	// The videos are stored like everything else, owned by the account, so they too are addressed by
	// record id.
	if err := c.store.StoreVideos(batch, key.UserID); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range batch {
		v.Source = key.Platform
		state.videos[v.RecordID] = v
		listed[v.RecordID] = struct{}{}
	}
	return nil
}

// forgetUnlisted drops the videos a complete listing no longer shows.
func (c *Catalogs) forgetUnlisted(state *connectedCatalog, listed map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range state.videos {
		if _, ok := listed[id]; !ok {
			delete(state.videos, id)
		}
	}
}

func (c *Catalogs) drop(key CatalogKey) {
	c.mu.Lock()
	delete(c.connected, key)
	c.mu.Unlock()
}

// watchScope cancels a connected job when its connection is replaced or removed, even if nobody asks for
// the catalog again: the job holds a cookie jar, which must not outlive the session. The watcher belongs to
// the job; stop ends it and waits.
func (c *Catalogs) watchScope(ctx context.Context, key CatalogKey, cancel context.CancelFunc) (stop func(), err error) {
	done, joined := make(chan struct{}), make(chan struct{})
	if !c.spawn(func(owner context.Context) {
		defer close(joined)
		ticker := time.NewTicker(c.opts.ScopeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-owner.Done():
				return
			case <-ticker.C:
				if c.ScopeCurrent(key) != nil {
					cancel()
					return
				}
			}
		}
	}) {
		return nil, errUnavailable
	}
	return func() { close(done); <-joined }, nil
}
