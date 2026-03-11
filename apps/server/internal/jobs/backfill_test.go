package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestForEachBoundedRunsEveryItemWithinTheLimit(t *testing.T) {
	t.Parallel()
	items := make([]int, 20)
	for i := range items {
		items[i] = i
	}
	var running, peak, done atomic.Int32
	var seen atomic.Int32
	started := forEachBounded(context.Background(), items, 3,
		func(int) { seen.Add(1) },
		func(int) {
			now := running.Add(1)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			running.Add(-1)
			done.Add(1)
		})
	if !started || done.Load() != 20 || seen.Load() != 20 {
		t.Fatalf("started=%v done=%d before=%d", started, done.Load(), seen.Load())
	}
	if got := peak.Load(); got < 1 || got > 3 {
		t.Fatalf("parallelism %d is outside the limit of 3", got)
	}
}

func TestForEachBoundedWaitsForRunningItemsWhenCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	entered := make(chan struct{})
	var finished atomic.Bool
	result := make(chan bool)
	go func() {
		result <- forEachBounded(ctx, []int{1, 2, 3}, 1, nil, func(int) {
			close(entered)
			<-release
			finished.Store(true)
		})
	}()
	receive(t, entered, "the first item")
	cancel()
	select {
	case <-result:
		t.Fatal("forEachBounded returned while an item was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if started := receive(t, result, "the result"); started || !finished.Load() {
		t.Fatalf("started=%v finished=%v", started, finished.Load())
	}
}

func TestBackfillOptionsDefaults(t *testing.T) {
	t.Parallel()
	got := BackfillOptions{Batch: 3}.withDefaults(45 * time.Second)
	want := BackfillOptions{IdleInterval: time.Minute, BusyInterval: 15 * time.Second, StartDelay: 10 * time.Second, Batch: 3, Concurrency: 2, Retry: 6 * time.Hour, FetchTimeout: 45 * time.Second}
	if got != want {
		t.Fatalf("defaults: %+v", got)
	}
	if got := (BackfillOptions{FetchTimeout: time.Second}).withDefaults(0); got.FetchTimeout != time.Second {
		t.Fatalf("an explicit timeout was replaced: %v", got.FetchTimeout)
	}
}
