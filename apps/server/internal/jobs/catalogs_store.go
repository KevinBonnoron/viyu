package jobs

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Public catalogs are persisted in two collections: channel_catalogs (one row per channel: its status, the
// time it completed, the generation being written) and channel_catalog_entries (one row per video of a
// catalog, tagged with the generation that last saw it). A synchronization writes the new generation next to
// the old one and drops the entries of every other generation in the very transaction that marks the
// catalog complete: a catalog is never smaller than its last complete listing while it loads, and a video
// row deleted from the cache cannot make a complete catalog look shorter without the catalog noticing.

const (
	catalogsCollection = "channel_catalogs"
	entriesCollection  = "channel_catalog_entries"
)

// row returns the stored catalog of a public key, nil when there is none. app is the application or a
// transaction.
func (c *Catalogs) row(app core.App, key CatalogKey) (*core.Record, error) {
	row, err := app.FindFirstRecordByFilter(catalogsCollection, "platform = {:platform} && platform_id = {:id}", dbx.Params{"platform": key.Platform, "id": key.ChannelID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// rowOrNew is row, or a new record that carries the identity.
func (c *Catalogs) rowOrNew(app core.App, key CatalogKey) (*core.Record, error) {
	row, err := c.row(app, key)
	if err != nil || row != nil {
		return row, err
	}
	collection, err := app.FindCollectionByNameOrId(catalogsCollection)
	if err != nil {
		return nil, err
	}
	row = core.NewRecord(collection)
	row.Set("platform", key.Platform)
	row.Set("platform_id", key.ChannelID)
	return row, nil
}

// storedStatus is what a stored row says: complete only while it is current, and pending for everything
// else (no row, a synchronization that was running when the process stopped, one that failed and has to be
// tried again, a complete catalog that grew old). A failure is never reported as such: whether it is final
// is known only to the process that saw it (see statusOf).
func (c *Catalogs) storedStatus(row *core.Record) Status {
	if row == nil {
		return Pending
	}
	switch Status(row.GetString("status")) {
	case Complete:
		if completed := row.GetDateTime("completed_at").Time(); !completed.IsZero() && c.opts.Now().Sub(completed) < c.opts.PublicLifetime {
			return Complete
		}
	}
	return Pending
}

// statusOf is the status of a public catalog: the job that is working on it (a failed job waiting for its
// next attempt included), a failure that is final (Failed: nothing will change), or else what is stored.
func (c *Catalogs) statusOf(key CatalogKey, row *core.Record) Status {
	if entry, ok := c.queue.Get(key); ok && (!entry.Status.Terminal() || entry.Status == Failed) {
		return entry.Status
	}
	return c.storedStatus(row)
}

// reported is the status of a catalog as clients are told: a failure that is final is not an error they
// can act on, so the catalog counts as settled with what it holds.
func reported(status Status) Status {
	if status == Failed {
		return Complete
	}
	return status
}

// ensurePublic starts the synchronization of a public catalog unless it is current; false means the queue
// refused it.
func (c *Catalogs) ensurePublic(key CatalogKey, retry bool) (admitted bool) {
	if entry, ok := c.queue.Get(key); ok {
		switch {
		case !entry.Status.Terminal():
			return true
		case entry.Status == Failed && !retry:
			// It failed for good: asking again would fail again.
			return true
		}
	}
	row, err := c.row(c.app, key)
	if err != nil {
		logging.Of(c.app).Warn("Failed to read a channel catalog", "platform", key.Platform, "error", err)
		return true
	}
	if c.storedStatus(row) == Complete {
		return true
	}
	_, admission := c.queue.Restart(key, key)
	return admission != Refused
}

// Stored reads a public catalog as it is stored, without starting anything: the videos loaded so far, with
// the stored status (or the live one while a job works on it). A channel that has no catalog yet answers
// pending with no videos.
func (c *Catalogs) Stored(platformID, channelID string) (Catalog, error) {
	key, _, err := c.resolve(CatalogKey{Platform: platformID, ChannelID: channelID})
	if err != nil {
		return Catalog{}, err
	}
	row, err := c.row(c.app, key)
	if err != nil {
		return Catalog{}, err
	}
	records, err := c.records(row)
	if err != nil {
		return Catalog{}, err
	}
	videos := c.store.VideosOf(records)
	return Catalog{Channel: c.channelOf(key, row), Videos: videos, Sync: Sync{Status: reported(c.statusOf(key, row)), Loaded: len(videos)}}, nil
}

// records lists the stored videos of a catalog, newest first, as the stored rows are (their metadata is
// whatever the cache knows). A video an account owns privately is never part of a public catalog.
func (c *Catalogs) records(row *core.Record) ([]*core.Record, error) {
	if row == nil {
		return []*core.Record{}, nil
	}
	records := []*core.Record{}
	err := c.app.RecordQuery("videos").
		InnerJoin(entriesCollection, dbx.NewExp("[[channel_catalog_entries.video]] = [[videos.id]]")).
		AndWhere(dbx.HashExp{"channel_catalog_entries.catalog": row.Id}).
		OrderBy("videos.published DESC", "videos.platform_id").
		All(&records)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(records, func(r *core.Record) bool { return !catalog.VisibleTo(r, "") }), nil
}

// channelOf presents the channel of a catalog: the one the last synchronization listed, else the stored
// channel of the key; nil when there is none or it is a stub.
func (c *Catalogs) channelOf(key CatalogKey, row *core.Record) *platform.Channel {
	var record *core.Record
	if row != nil && row.GetString("channel") != "" {
		record, _ = c.store.Channel(row.GetString("channel"))
	}
	if record == nil {
		record, _ = c.store.FindChannel(key.Platform, key.ChannelID)
	}
	if record == nil || !catalog.VisibleTo(record, key.UserID) {
		return nil
	}
	return catalog.ChannelOf(record)
}

// begin starts a generation: the channel the listing described is stored, and the catalog row points at it
// and says it is running.
func (c *Catalogs) begin(key CatalogKey, info *platform.Channel) (catalogID, generation string, err error) {
	generation, err = randomID()
	if err != nil {
		return "", "", err
	}
	err = c.app.RunInTransaction(func(tx core.App) error {
		if err := c.store.In(tx).StoreChannelListing(info, ""); err != nil {
			return err
		}
		row, err := c.rowOrNew(tx, key)
		if err != nil {
			return err
		}
		row.Set("channel", info.RecordID)
		row.Set("status", string(Running))
		row.Set("task_generation", generation)
		if err := tx.Save(row); err != nil {
			return err
		}
		catalogID = row.Id
		return nil
	})
	return catalogID, generation, err
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// setStatus stores the status of a public catalog.
func (c *Catalogs) setStatus(key CatalogKey, status Status) error {
	return c.app.RunInTransaction(func(tx core.App) error {
		row, err := c.rowOrNew(tx, key)
		if err != nil {
			return err
		}
		row.Set("status", string(status))
		return tx.Save(row)
	})
}

// writeBatch stores a batch of videos and puts them in the catalog under the generation, all in one
// transaction. The videos get their record ids.
func (c *Catalogs) writeBatch(catalogID, generation string, batch []platform.Video) error {
	return c.app.RunInTransaction(func(tx core.App) error {
		if err := c.store.In(tx).StoreVideos(batch, ""); err != nil {
			return err
		}
		return writeEntries(tx, catalogID, generation, batch)
	})
}

// writeEntries makes the catalog hold the stored videos of batch under the generation: the entries that
// exist are renewed and the others created, found with one query.
func writeEntries(tx core.App, catalogID, generation string, batch []platform.Video) error {
	videoIDs := make([]any, 0, len(batch))
	for _, v := range batch {
		videoIDs = append(videoIDs, v.RecordID)
	}
	var existing []*core.Record
	if err := tx.RecordQuery(entriesCollection).AndWhere(dbx.HashExp{"catalog": catalogID}).AndWhere(dbx.In("video", videoIDs...)).All(&existing); err != nil {
		return err
	}
	entries := make(map[string]*core.Record, len(existing))
	for _, entry := range existing {
		entries[entry.GetString("video")] = entry
	}
	collection, err := tx.FindCollectionByNameOrId(entriesCollection)
	if err != nil {
		return err
	}
	for _, v := range batch {
		entry := entries[v.RecordID]
		switch {
		case entry == nil:
			entry = core.NewRecord(collection)
			entry.Set("catalog", catalogID)
			entry.Set("video", v.RecordID)
		case entry.GetString("generation") == generation:
			continue
		}
		entry.Set("generation", generation)
		if err := tx.Save(entry); err != nil {
			return err
		}
		entries[v.RecordID] = entry
	}
	return nil
}

// complete swaps the generations: the entries the listing did not see are dropped, the collections the
// channel listed are kept with the catalog (nil leaves the stored ones as they are) and the catalog is marked
// complete, in one transaction.
func (c *Catalogs) complete(key CatalogKey, catalogID, generation string, collections []platform.Collection) error {
	return c.app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery("DELETE FROM channel_catalog_entries WHERE catalog = {:catalog} AND generation != {:generation}").Bind(dbx.Params{"catalog": catalogID, "generation": generation}).Execute(); err != nil {
			return err
		}
		row, err := c.rowOrNew(tx, key)
		if err != nil {
			return err
		}
		row.Set("status", string(Complete))
		row.Set("completed_at", c.opts.Now())
		if collections != nil {
			row.Set("collections", collections)
		}
		return tx.Save(row)
	})
}

// collectionsOf reads the collections the last complete synchronization of a catalog stored.
func collectionsOf(row *core.Record) []platform.Collection {
	var collections []platform.Collection
	if err := row.UnmarshalJSONField("collections", &collections); err != nil {
		return nil
	}
	return collections
}
