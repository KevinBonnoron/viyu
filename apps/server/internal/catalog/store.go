// Package catalog is the PocketBase-backed store of the videos and channels the server has seen: it
// upserts them, applies the owners rules (what a connected account fetched stays private to it), presents
// the records as platform types and resolves record ids back to platform-native identities.
//
// Concurrency: there is no lock. PocketBase serializes writers on a single connection, so every write
// batch runs in one transaction (at most batchSize items), and the unique indexes idx_unique_video and
// idx_unique_channel make an upsert atomic: when an insert still loses a race (a writer outside the
// transaction machinery, a store bound to someone else's transaction) the record is looked up again and
// updated, a bounded number of times.
//
// Failure is closed: an item that could not be stored is reported through the returned error and keeps an
// empty RecordID, so the native id it carries can never be serialized (see platform.ErrNotStored).
package catalog

import (
	"errors"
	"slices"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	// batchSize bounds how many videos or channels one write transaction holds.
	batchSize = 100
	// upsertAttempts bounds how often an insert that lost a race against another one is retried as an
	// update.
	upsertAttempts = 3
	// recordTTL is how long a stored video or channel listing counts as fresh.
	recordTTL = 24 * time.Hour
	// renewMargin is how stale an expiry must be before a store renews it. Every save is a realtime event
	// pushed to every client, so a record that was renewed less than this long ago keeps its expiry: a
	// listing stored over and over (a catalog crawl, a search) writes a record at most once per margin,
	// and only when something else changed in between.
	renewMargin = time.Hour
)

// Options tunes a Store.
type Options struct {
	// OnProfilesWanted is called, without blocking, after a write that leaves channels without avatar or
	// subscriber count, so a worker can fill them in.
	OnProfilesWanted func()
	// OnPublicationsWanted is called, without blocking, after a write that leaves videos without a
	// publication date, so a worker can look them up.
	OnPublicationsWanted func()
}

// Store reads and writes the videos and channels collections. It holds no state of its own and is safe
// for concurrent use.
type Store struct {
	app       core.App
	platforms *platform.Registry
	opts      Options
}

// New returns the store of app. The registry tells the platform-specific storage rules (single title,
// channel id aliases, valid ids).
func New(app core.App, platforms *platform.Registry, opts Options) *Store {
	return &Store{app: app, platforms: platforms, opts: opts}
}

// In returns the same store bound to tx, a transaction obtained from RunInTransaction: everything it
// writes commits or rolls back with that transaction, and a wake-up callback waits for the commit. A
// video stored through it keeps the record ids it was given even if the transaction later rolls back.
func (s *Store) In(tx core.App) *Store {
	return &Store{app: tx, platforms: s.platforms, opts: s.opts}
}

// write runs fn in a transaction: the one the store is already bound to, or a new one. fn must use the
// store it is given, never s, which would wait for the transaction's connection.
func (s *Store) write(fn func(tx *Store) error) error {
	return s.app.RunInTransaction(func(tx core.App) error { return fn(s.In(tx)) })
}

// wanted tells which background work a write made necessary.
type wanted struct{ profiles, publications bool }

func (w *wanted) merge(other wanted) {
	w.profiles = w.profiles || other.profiles
	w.publications = w.publications || other.publications
}

// wake calls the callbacks of what a write made necessary: at once, or, when the store is bound to a
// transaction, once that transaction committed (a worker woken earlier would not see the rows).
func (s *Store) wake(w wanted) {
	if !w.profiles && !w.publications {
		return
	}
	call := func() {
		if w.profiles && s.opts.OnProfilesWanted != nil {
			s.opts.OnProfilesWanted()
		}
		if w.publications && s.opts.OnPublicationsWanted != nil {
			s.opts.OnPublicationsWanted()
		}
	}
	if info := s.app.TxInfo(); info != nil {
		info.OnComplete(func(txErr error) error {
			if txErr == nil {
				call()
			}
			return nil
		})
		return
	}
	call()
}

// chunks splits items in batches of at most batchSize.
func chunks[T any](items []T) [][]T {
	var out [][]T
	for chunk := range slices.Chunk(items, batchSize) {
		out = append(out, chunk)
	}
	return out
}

// renewal returns the expiry a store gives a record whose current expiry is current, and whether it
// differs from current (see renewMargin).
func renewal(current time.Time, now time.Time) (time.Time, bool) {
	target := now.Add(recordTTL)
	if current.After(target.Add(-renewMargin)) {
		return current, false
	}
	return target, true
}

var errUnidentified = errors.New("catalog: a record needs a platform and a native id")
