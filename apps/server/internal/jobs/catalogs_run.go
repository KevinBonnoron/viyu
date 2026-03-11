package jobs

import (
	"context"

	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// run is the job of the catalog queue.
func (c *Catalogs) run(ctx context.Context, key CatalogKey) error {
	if key.Connected() {
		return c.runConnected(ctx, key)
	}
	return c.runPublic(ctx, key)
}

// runPublic synchronizes a public catalog and stores how it ended. A run that was cancelled stores nothing:
// the catalog keeps the status it had, and a read starts it again.
func (c *Catalogs) runPublic(ctx context.Context, key CatalogKey) error {
	err := c.syncPublic(ctx, key)
	if err != nil && ctx.Err() == nil {
		if saveErr := c.setStatus(key, Failed); saveErr != nil {
			err = saveErr
		}
	}
	return err
}

func (c *Catalogs) syncPublic(ctx context.Context, key CatalogKey) error {
	p, err := c.platformOf(key)
	if err != nil {
		return err
	}
	// A crash leaves "running" behind, which the next read treats as a synchronization to start again.
	if err := c.setStatus(key, Running); err != nil {
		return err
	}
	info, initial, err := c.list(ctx, p, key, "")
	if err != nil {
		return err
	}
	catalogID, generation, err := c.begin(key, &info)
	if err != nil {
		return err
	}
	last := c.opts.Now()
	cr := crawl{
		platform: p, channel: info, size: c.opts.BatchSize,
		flush: func(batch []platform.Video) error {
			if err := c.writeBatch(catalogID, generation, batch); err != nil {
				return err
			}
			if now := c.opts.Now(); now.Sub(last) >= c.opts.ProgressInterval {
				last = now
				c.notify(CatalogEvent{Key: key})
			}
			return nil
		},
	}
	if err := cr.run(ctx, initial); err != nil {
		return err
	}
	collections, err := p.CatalogSync().Collections(ctx, info)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The videos are all listed: the catalog completes, keeping the collections its last complete
		// synchronization read, which is better than losing the series they name or failing the catalog.
		logging.Of(c.app).Warn("Channel collections unavailable", "platform", key.Platform, "code", taskCode("collections")(err))
		return c.complete(key, catalogID, generation, nil)
	}
	if collections == nil {
		collections = []platform.Collection{}
	}
	return c.complete(key, catalogID, generation, collections)
}

func (c *Catalogs) platformOf(key CatalogKey) (platform.Platform, error) {
	_, p, err := c.resolve(key)
	return p, err
}

// list asks for the first page of a channel: the channel and its latest videos, completed with what the
// platform's cheap feeds know.
func (c *Catalogs) list(ctx context.Context, p platform.Platform, key CatalogKey, cookieFile string) (platform.Channel, []platform.Video, error) {
	info, initial, err := p.ChannelListing(ctx, key.ChannelID, c.opts.ListingSize, cookieFile)
	if err != nil {
		return platform.Channel{}, nil, err
	}
	if info.Platform == "" {
		info.Platform = p.ID()
	}
	p.EnrichListing(ctx, info.ID, initial)
	return info, initial, nil
}

// crawl streams the whole catalog of a channel to flush, in batches.
type crawl struct {
	platform platform.Platform
	channel  platform.Channel
	// cookieFile is the private jar of the connection the listing runs as, "" for a public one.
	cookieFile string
	size       int
	// flush takes a batch of at most size videos, each listed once; it may keep the slice's videos.
	flush func(batch []platform.Video) error
}

// run lists the first page the caller already has, then the rest through the platform's CatalogSyncer. A
// video seen twice is passed on once. It stops at the first error of flush and when ctx ends.
func (cr crawl) run(ctx context.Context, initial []platform.Video) error {
	seen := make(map[string]struct{}, len(initial))
	batch := make([]platform.Video, 0, cr.size)
	visit := func(v platform.Video) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if v.ID == "" {
			return errInvalidEntry
		}
		if _, dup := seen[v.ID]; dup {
			return nil
		}
		seen[v.ID] = struct{}{}
		batch = append(batch, cr.attribute(v))
		if len(batch) < cr.size {
			return nil
		}
		return cr.flushPending(&batch)
	}
	for _, v := range initial {
		if err := visit(v); err != nil {
			return err
		}
	}
	// The first page is what the channel shows while the rest streams in: it does not wait for a full batch.
	if err := cr.flushPending(&batch); err != nil {
		return err
	}
	if err := cr.platform.CatalogSync().Catalog(ctx, cr.channel, cr.cookieFile, visit); err != nil {
		// What was listed before the failure stays: a partial catalog is better than none.
		if ctx.Err() == nil {
			_ = cr.flushPending(&batch)
		}
		return err
	}
	if err := cr.flushPending(&batch); err != nil {
		return err
	}
	return ctx.Err()
}

// flushPending passes on what a batch holds and starts a new one.
func (cr crawl) flushPending(batch *[]platform.Video) error {
	if len(*batch) == 0 {
		return nil
	}
	full := *batch
	*batch = make([]platform.Video, 0, cr.size)
	return cr.flush(full)
}

// attribute gives a listed video the platform and, when the listing names none, the channel it was listed
// under: a video stored without an owner would be left out of every catalog view of the channel.
func (cr crawl) attribute(v platform.Video) platform.Video {
	if v.Platform == "" {
		v.Platform = cr.platform.ID()
	}
	if v.ChannelID == "" {
		v.ChannelID = cr.channel.ID
		if v.ChannelName == "" {
			v.ChannelName = cr.channel.Name
		}
	}
	return v
}
