package jobs

import (
	"slices"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// catalogState is what is known of the public catalog of a channel without running anything.
type catalogState struct {
	Sync
	// Live: a job is working on the catalog or waits for a slot.
	Live bool
	// Stale: the catalog completed once but is too old to count, so a new synchronization is due.
	Stale bool
}

// statusBatch bounds how many ids one IN query looks up.
const statusBatch = 200

// states reads the state of the public catalogs of channels, by channel record id, with one query per
// batch for the catalog rows and one for the number of entries of all of them. A channel whose platform
// lists no catalog has no entry; one that has no catalog row yet is pending with nothing loaded.
func (c *Catalogs) states(channels []*core.Record) (map[string]catalogState, error) {
	keys := make(map[string]CatalogKey, len(channels))
	var ids []any
	for _, channel := range channels {
		if key, ok := c.canonicalKey(channel); ok {
			keys[channel.Id] = key
			ids = append(ids, key.ChannelID)
		}
	}
	rows, err := c.rowsOf(ids)
	if err != nil {
		return nil, err
	}
	loaded, err := c.loadedOf(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]catalogState, len(keys))
	for channelID, key := range keys {
		row := rows[key]
		state := catalogState{Sync: Sync{Status: c.storedStatus(row)}}
		state.Stale = row != nil && state.Status == Pending && Status(row.GetString("status")) == Complete
		if row != nil {
			state.Loaded = loaded[row.Id]
		}
		if entry, ok := c.queue.Get(key); ok {
			switch {
			case !entry.Status.Terminal():
				state.Status, state.Live, state.Stale = entry.Status, true, false
			case entry.Status == Failed:
				// It failed for good: nothing is going to change, so nothing is awaited.
				state.Status, state.Stale = Complete, false
			}
		}
		out[channelID] = state
	}
	return out, nil
}

// rowsOf reads the catalog rows of the channel ids, by key.
func (c *Catalogs) rowsOf(ids []any) (map[CatalogKey]*core.Record, error) {
	rows := map[CatalogKey]*core.Record{}
	for chunk := range slices.Chunk(ids, statusBatch) {
		var found []*core.Record
		if err := c.app.RecordQuery(catalogsCollection).AndWhere(dbx.In("platform_id", chunk...)).All(&found); err != nil {
			return nil, err
		}
		for _, row := range found {
			rows[CatalogKey{Platform: row.GetString("platform"), ChannelID: row.GetString("platform_id")}] = row
		}
	}
	return rows, nil
}

// loadedOf counts the entries of the catalogs, by catalog row id.
func (c *Catalogs) loadedOf(rows map[CatalogKey]*core.Record) (map[string]int, error) {
	ids := make([]any, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	type count struct {
		Catalog string `db:"catalog"`
		Total   int    `db:"total"`
	}
	loaded := make(map[string]int, len(ids))
	for chunk := range slices.Chunk(ids, statusBatch) {
		var counts []count
		if err := c.app.DB().Select("catalog", "COUNT(*) AS total").From(entriesCollection).Where(dbx.In("catalog", chunk...)).GroupBy("catalog").All(&counts); err != nil {
			return nil, err
		}
		for _, n := range counts {
			loaded[n.Catalog] = n.Total
		}
	}
	return loaded, nil
}
