// Package jobs owns the background work of the server: the Group that tracks every goroutine for
// shutdown, the generic Queue that bounds and remembers jobs, and the workers built on them.
package jobs

import (
	"context"
	"sync"
)

// Group tracks the goroutines and requests of one application, so shutdown cancels them all through one
// context and waits for them. Nothing in the server starts a goroutine that is not owned by a Group.
type Group struct {
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

// NewGroup returns a running Group.
func NewGroup() *Group {
	ctx, cancel := context.WithCancel(context.Background())
	return &Group{ctx: ctx, cancel: cancel}
}

// Context is cancelled when the Group stops; background work and detached lookups run under it.
func (g *Group) Context() context.Context { return g.ctx }

// Go runs work on a goroutine the Group waits for. It reports false, and runs nothing, once the Group is stopping.
func (g *Group) Go(work func(ctx context.Context)) bool {
	leave, ok := g.Enter()
	if !ok {
		return false
	}
	go func() {
		defer leave()
		work(g.ctx)
	}()
	return true
}

// Enter registers work the caller runs itself (an HTTP request) and returns the function that ends it. It
// reports false once the Group is stopping.
func (g *Group) Enter() (leave func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, false
	}
	g.wg.Add(1)
	var once sync.Once
	return func() { once.Do(g.wg.Done) }, true
}

// Stopping reports whether Stop was called.
func (g *Group) Stopping() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closing
}

// Stop refuses new work, cancels the Group's context and waits for everything that was started. It is
// safe to call more than once.
func (g *Group) Stop() {
	g.mu.Lock()
	g.closing = true
	g.cancel()
	g.mu.Unlock()
	g.wg.Wait()
}
