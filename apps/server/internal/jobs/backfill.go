package jobs

import (
	"context"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// The server owns what a listing omits: cached channels without an avatar or subscriber count, and
// cached videos without a publication date, are completed in the background, subscribed channels first.
// PocketBase's realtime updates of the collections deliver the result to clients, which never request it.

// BackfillOptions tunes a backfill worker; a zero field takes the default.
type BackfillOptions struct {
	// IdleInterval is the pause after a pass that found nothing to do (default 1 minute).
	IdleInterval time.Duration
	// BusyInterval is the pause after a pass that did work (default 15 seconds).
	BusyInterval time.Duration
	// StartDelay is the wait before the first pass (default 10 seconds).
	StartDelay time.Duration
	// Batch is how many records one pass looks up (default 10).
	Batch int
	// Concurrency is how many lookups run at once within a pass (default 2).
	Concurrency int
	// Retry is how long a record whose lookup did not complete it is left alone (default 6 hours).
	Retry time.Duration
	// FetchTimeout bounds one lookup. The default is 45 seconds for the publication backfill and
	// unbounded for the profile backfill, whose listing is a single entry.
	FetchTimeout time.Duration
}

func (o BackfillOptions) withDefaults(fetchTimeout time.Duration) BackfillOptions {
	defaults := BackfillOptions{
		IdleInterval: time.Minute,
		BusyInterval: 15 * time.Second,
		StartDelay:   10 * time.Second,
		Batch:        10,
		Concurrency:  2,
		Retry:        6 * time.Hour,
		FetchTimeout: fetchTimeout,
	}
	if o.IdleInterval > 0 {
		defaults.IdleInterval = o.IdleInterval
	}
	if o.BusyInterval > 0 {
		defaults.BusyInterval = o.BusyInterval
	}
	if o.StartDelay > 0 {
		defaults.StartDelay = o.StartDelay
	}
	if o.Batch > 0 {
		defaults.Batch = o.Batch
	}
	if o.Concurrency > 0 {
		defaults.Concurrency = o.Concurrency
	}
	if o.Retry > 0 {
		defaults.Retry = o.Retry
	}
	if o.FetchTimeout > 0 {
		defaults.FetchTimeout = o.FetchTimeout
	}
	return defaults
}

// platformIDsInUse are the platforms a backfill looks up, as query arguments. Platforms nobody uses any
// more are not looked up: the work is for everyone, so it stops only when every account has turned the
// platform off.
func platformIDsInUse(s *sources.Service) []any {
	used := s.PlatformsInUse()
	ids := make([]any, len(used))
	for i, id := range used {
		ids[i] = id
	}
	return ids
}

// untried keeps, in order, the first limit records that were not attempted within the retry window.
func untried(candidates []*core.Record, attempted map[string]time.Time, retry time.Duration, limit int) []*core.Record {
	picked := make([]*core.Record, 0, limit)
	for _, record := range candidates {
		if last, ok := attempted[record.Id]; ok && time.Since(last) < retry {
			continue
		}
		picked = append(picked, record)
		if len(picked) == limit {
			break
		}
	}
	return picked
}

// candidateQuery is the query of the records a backfill considers: undone ones of the platforms in use,
// subscribed channels first (subscribed is the SQL that tells it), then newest first.
func candidateQuery(app core.App, collection string, missing dbx.Expression, subscribed, recency string, ids []any, limit int) *dbx.SelectQuery {
	return app.RecordQuery(collection).
		AndWhere(missing).
		AndWhere(dbx.In("platform", ids...)).
		OrderBy("("+subscribed+") DESC", recency+" DESC", "id ASC").
		Limit(int64(limit * 10))
}

// forEachBounded calls fn on every item with at most limit calls at once and returns when all returned.
// before runs on the calling goroutine just before an item starts. When ctx ends no further item starts
// and the result is false.
func forEachBounded[T any](ctx context.Context, items []T, limit int, before func(T), fn func(T)) bool {
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, max(limit, 1))
	for _, item := range items {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return false
		}
		if ctx.Err() != nil { // both were ready: a cancelled context wins
			return false
		}
		if before != nil {
			before(item)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			fn(item)
		}()
	}
	return true
}

// runBackfill is the loop both workers share: a pass after the start delay, then after the idle or busy
// interval according to what the pass did, or as soon as wake fires. It returns when ctx ends.
func runBackfill(ctx context.Context, opts BackfillOptions, wake *Signal, pass func(ctx context.Context) int) {
	timer := time.NewTimer(opts.StartDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-wake.C():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if pass(ctx) > 0 {
			timer.Reset(opts.BusyInterval)
		} else {
			timer.Reset(opts.IdleInterval)
		}
	}
}
