// Package cache holds the in-memory caches of the server: one generic, bounded TTL cache with optional
// single-flight loading, and the single-flight group it is built on.
package cache

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Spawner starts work on a goroutine its owner tracks for shutdown and hands it the owner's lifetime
// context. It reports false when the owner is shutting down and refuses the work.
type Spawner func(work func(ctx context.Context)) bool

// ErrRefused is returned when the Spawner refuses new work because its owner is shutting down.
var ErrRefused = errors.New("server is shutting down")

// Options configures a TTL cache.
type Options struct {
	// TTL is the lifetime of an entry stored with Put.
	TTL time.Duration
	// Max bounds the entries kept; the entry closest to expiry leaves first. Zero means unbounded.
	Max int
	// Spawn runs single-flight loads (see TTL.Do). Nil runs them on untracked goroutines.
	Spawn Spawner
	// LoadTimeout bounds a single-flight load; zero means one minute.
	LoadTimeout time.Duration
	// Now replaces the clock (tests).
	Now func() time.Time
}

type entry[V any] struct {
	value   V
	expires time.Time
}

// TTL is a bounded cache with per-entry expiry, safe for concurrent use.
type TTL[K comparable, V any] struct {
	ttl     time.Duration
	max     int
	now     func() time.Time
	flights *Flight[K, V]

	mu      sync.Mutex
	entries map[K]entry[V]
}

// New builds a TTL cache.
func New[K comparable, V any](opts Options) *TTL[K, V] {
	c := &TTL[K, V]{ttl: opts.TTL, max: opts.Max, now: opts.Now, entries: map[K]entry[V]{}}
	if c.now == nil {
		c.now = time.Now
	}
	timeout := opts.LoadTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	c.flights = NewFlight[K, V](opts.Spawn, timeout)
	return c
}

// Get returns the live entry of key.
func (c *TTL[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		var zero V
		return zero, false
	}
	return e.value, true
}

// Put stores value for the cache's TTL.
func (c *TTL[K, V]) Put(key K, value V) { c.PutFor(key, value, c.ttl) }

// PutFor stores value for ttl.
func (c *TTL[K, V]) PutFor(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, replacing := c.entries[key]; !replacing && c.max > 0 && len(c.entries) >= c.max {
		c.purgeLocked(now)
		if len(c.entries) >= c.max {
			c.evictSoonestLocked()
		}
	}
	c.entries[key] = entry[V]{value: value, expires: now.Add(ttl)}
}

func (c *TTL[K, V]) evictSoonestLocked() {
	var soonest K
	var soonestExpiry time.Time
	found := false
	for key, e := range c.entries {
		if !found || e.expires.Before(soonestExpiry) {
			soonest, soonestExpiry, found = key, e.expires, true
		}
	}
	if found {
		delete(c.entries, soonest)
	}
}

// Touch renews the lifetime of a live entry to ttl from now, and returns its value.
func (c *TTL[K, V]) Touch(key K, ttl time.Duration) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	now := c.now()
	if !ok || !now.Before(e.expires) {
		var zero V
		return zero, false
	}
	e.expires = now.Add(ttl)
	c.entries[key] = e
	return e.value, true
}

// Delete drops one entry.
func (c *TTL[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// DeleteFunc drops every entry whose key matches.
func (c *TTL[K, V]) DeleteFunc(match func(K) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if match(key) {
			delete(c.entries, key)
		}
	}
}

// Purge drops the entries that expired.
func (c *TTL[K, V]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeLocked(c.now())
}

func (c *TTL[K, V]) purgeLocked(now time.Time) {
	for key, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, key)
		}
	}
}

// Len is the number of stored entries, expired ones included until they are purged.
func (c *TTL[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Waiting is the number of callers waiting on the load of key.
func (c *TTL[K, V]) Waiting(key K) int { return c.flights.Waiting(key) }

// Do answers from the cache or runs load once for every concurrent caller of key, storing a successful
// answer. The load is detached from the callers (see Flight), so the caller that started it can leave
// without failing the others.
func (c *TTL[K, V]) Do(ctx context.Context, key K, load func(context.Context) (V, error)) (V, error) {
	if value, ok := c.Get(key); ok {
		return value, nil
	}
	return c.flights.Do(ctx, key, func(ctx context.Context) (V, error) {
		// A caller that missed just as the previous load finished starts a new flight: the answer is stored by now.
		if value, ok := c.Get(key); ok {
			return value, nil
		}
		value, err := load(ctx)
		if err == nil && ctx.Err() == nil {
			c.Put(key, value)
		}
		return value, err
	})
}

// Flight collapses concurrent calls for one key into a single execution. The execution is detached from
// every caller: it runs under its own context (bounded by a timeout and by its owner's lifetime), so the
// caller that happened to start it can leave without failing the others. It is cancelled only when the
// last caller has left. Each caller still returns as soon as its own context ends.
type Flight[K comparable, V any] struct {
	spawn   Spawner
	timeout time.Duration

	mu    sync.Mutex
	calls map[K]*call[V]
}

type call[V any] struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	value   V
	err     error
}

// NewFlight builds a Flight. A nil spawn runs the work on untracked goroutines.
func NewFlight[K comparable, V any](spawn Spawner, timeout time.Duration) *Flight[K, V] {
	if spawn == nil {
		spawn = func(work func(context.Context)) bool {
			go work(context.Background())
			return true
		}
	}
	return &Flight[K, V]{spawn: spawn, timeout: timeout, calls: map[K]*call[V]{}}
}

// Do runs fn once for concurrent callers of the same key.
func (g *Flight[K, V]) Do(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error) {
	var zero V
	g.mu.Lock()
	c := g.calls[key]
	if c == nil {
		c = &call[V]{done: make(chan struct{})}
		started := g.spawn(func(base context.Context) {
			flightCtx, cancel := context.WithTimeout(base, g.timeout)
			g.mu.Lock()
			c.cancel = cancel
			abandoned := c.waiters == 0 && g.calls[key] != c
			g.mu.Unlock()
			if abandoned {
				cancel()
			}
			value, err := fn(flightCtx)
			cancel()
			g.mu.Lock()
			c.value, c.err = value, err
			if g.calls[key] == c {
				delete(g.calls, key)
			}
			g.mu.Unlock()
			close(c.done)
		})
		if !started {
			g.mu.Unlock()
			return zero, ErrRefused
		}
		g.calls[key] = c
	}
	c.waiters++
	g.mu.Unlock()
	select {
	case <-c.done:
		return c.value, c.err
	case <-ctx.Done():
		g.mu.Lock()
		c.waiters--
		if c.waiters == 0 {
			if g.calls[key] == c {
				delete(g.calls, key)
			}
			if c.cancel != nil {
				c.cancel()
			}
		}
		g.mu.Unlock()
		return zero, ctx.Err()
	}
}

// Waiting is the number of callers currently waiting on the execution for key.
func (g *Flight[K, V]) Waiting(key K) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c := g.calls[key]; c != nil {
		return c.waiters
	}
	return 0
}
