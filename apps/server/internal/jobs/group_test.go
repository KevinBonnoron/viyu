package jobs

import (
	"context"
	"testing"
	"time"
)

func TestStopCancelsAndWaitsForEverythingStarted(t *testing.T) {
	g := NewGroup()
	started := make(chan struct{})
	finished := make(chan struct{})
	if !g.Go(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("a running group refused work")
	}
	leave, ok := g.Enter()
	if !ok {
		t.Fatal("a running group refused a request")
	}
	<-started
	stopped := make(chan struct{})
	go func() { g.Stop(); close(stopped) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not cancel the work")
	}
	select {
	case <-stopped:
		t.Fatal("stop returned while a request was still running")
	case <-time.After(50 * time.Millisecond):
	}
	leave()
	leave() // leaving twice must not release a second request
	<-stopped
	if g.Go(func(context.Context) { t.Error("work started after stop") }) {
		t.Fatal("a stopped group accepted work")
	}
	if _, ok := g.Enter(); ok || !g.Stopping() {
		t.Fatal("a stopped group accepted a request")
	}
	g.Stop()
}
