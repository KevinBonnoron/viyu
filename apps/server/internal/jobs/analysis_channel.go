package jobs

import (
	"context"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// analyseChannel brings the stored series of one channel up to date. It never waits for a job: whatever is
// still running wakes the channel again when it finishes, and the pass that follows sees its result.
func (a *Analysis) analyseChannel(ctx context.Context, channel *core.Record, inUse bool) {
	id := channel.Id
	a.marks.put(id, Running)
	defer a.settleMark(id)
	if !inUse {
		return
	}
	key, ok := a.catalogs.canonicalKey(channel)
	if !ok {
		return
	}
	status, deferred, err := a.catalogs.Ensure(key)
	if err != nil {
		if ctx.Err() == nil {
			logging.Of(a.app).Warn("Failed to discover a channel catalog", "channel", id, "error", err)
		}
		return
	}
	if deferred {
		a.deferred.add(id)
	}
	a.infer(ctx, channel, key, status == Complete)
}

// settleMark records that the worker is done with a channel, or that it has to come back.
func (a *Analysis) settleMark(id string) {
	if a.wakes.has(id) {
		a.marks.put(id, Pending)
		return
	}
	a.marks.put(id, Complete)
}

// infer reads the stored catalog of a channel, infers its series, completes them with what the thumbnails
// and the videos' own pages give (as jobs: what is not done yet shows next time) and stores them.
// catalogComplete says whether the catalog is whole, which is what lets a series disappear from storage. It
// is also what lets the collections the channel made count: they were read with the last complete listing,
// and they name series (in their own order) that take their videos away from inference.
func (a *Analysis) infer(ctx context.Context, channel *core.Record, key CatalogKey, catalogComplete bool) {
	row, err := a.catalogs.row(a.app, key)
	if err != nil || row == nil {
		return
	}
	records, err := a.catalogs.records(row)
	if err != nil {
		logging.Of(a.app).Warn("Failed to read a channel catalog", "channel", channel.Id, "error", err)
		return
	}
	videos := a.store.VideosOf(records)
	var named []series.Series
	if catalogComplete {
		named, videos = series.FromCollections(videos, collectionsOf(row))
	}
	list := a.parser.Infer(videos, nil)
	a.applyThumbnails(channel.Id, list)
	a.requestPublications(channel.Id, list, records)
	if ctx.Err() != nil {
		return
	}
	if err := a.series.Save(channel.Id, catalogComplete, series.Merge(named, list)); err != nil {
		logging.Of(a.app).Warn("Failed to store series", "channel", channel.Id, "error", err)
	}
}

// requestPublications queues the fetch of the pages of the videos the series need dates from, unless what
// is stored of a video already is as good as a fetch would make it.
func (a *Analysis) requestPublications(channelID string, list []series.Series, records []*core.Record) {
	stored := make(map[string]*core.Record, len(records))
	for _, r := range records {
		stored[r.GetString("platform")+":"+r.GetString("platform_id")] = r
	}
	seen := map[string]bool{}
	for _, v := range series.MissingPublications(list) {
		id := v.Platform + ":" + v.ID
		if seen[id] {
			continue
		}
		seen[id] = true
		if publicationSettled(stored[id]) {
			continue
		}
		if !a.submitMetadata(channelID, v) {
			a.deferred.add(channelID)
		}
	}
}

// publicationSettled tells whether the stored video was verified and either is dated or was fetched
// recently enough: asking again would learn nothing.
func publicationSettled(r *core.Record) bool {
	if r == nil {
		return false
	}
	meta := catalog.MetadataOf(r)
	return meta.OriginalTitleVerified && (catalog.PublishedOf(r) != "" || meta.Fresh())
}

// ensureJob makes sure the queue has a job for key that is not finished with an input the caller has
// outgrown. A finished job that stale says is out of date is run again, failed for good or not: its input
// is not the one that failed. A job that failed and waits for its next attempt is the queue's to run.
// queued is false when the queue is full: the work was not admitted.
func ensureJob[K comparable, In, Out any](q *Queue[K, In, Out], key K, in In, stale func(Entry[In, Out]) bool) (entry Entry[In, Out], queued bool) {
	entry, admission := q.Submit(key, in)
	switch {
	case admission == Refused:
		return entry, false
	case admission == Existing && entry.Status.Terminal() && stale(entry):
		entry, admission = q.Restart(key, in)
		return entry, admission != Refused
	}
	return entry, true
}
