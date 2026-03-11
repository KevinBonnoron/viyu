package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tracker is a Spawner that owns its goroutines, like the application's background group.
type tracker struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

func newTracker(t *testing.T) *tracker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tr := &tracker{ctx: ctx, cancel: cancel}
	t.Cleanup(func() { tr.stop() })
	return tr
}

func (tr *tracker) spawn(work func(context.Context)) bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return false
	}
	tr.wg.Add(1)
	go func() { defer tr.wg.Done(); work(tr.ctx) }()
	return true
}

func (tr *tracker) stop() {
	tr.mu.Lock()
	tr.closed = true
	tr.cancel()
	tr.mu.Unlock()
	tr.wg.Wait()
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestEntriesExpireAndTheBoundEvictsTheOneClosestToExpiry(t *testing.T) {
	clk := &clock{now: time.Unix(1000, 0)}
	c := New[string, int](Options{TTL: time.Minute, Max: 2, Now: clk.Now})
	c.Put("a", 1)
	clk.advance(10 * time.Second)
	c.Put("b", 2)
	clk.advance(10 * time.Second)
	c.Put("c", 3)
	if _, ok := c.Get("a"); ok {
		t.Fatal("the oldest entry survived the bound")
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("b: %d %v", v, ok)
	}
	c.Put("b", 20) // replacing never evicts
	if c.Len() != 2 {
		t.Fatalf("len %d", c.Len())
	}
	clk.advance(2 * time.Minute)
	if _, ok := c.Get("b"); ok {
		t.Fatal("an expired entry was served")
	}
	if c.Len() != 2 {
		t.Fatalf("expired entries leave on purge, len %d", c.Len())
	}
	c.Purge()
	if c.Len() != 0 {
		t.Fatalf("purge left %d", c.Len())
	}
}

func TestTouchRenewsOnlyLiveEntriesAndDeleteFuncMatchesKeys(t *testing.T) {
	clk := &clock{now: time.Unix(1000, 0)}
	c := New[string, int](Options{TTL: time.Minute, Now: clk.Now})
	c.Put("x:1", 1)
	c.Put("x:2", 2)
	c.Put("y:1", 3)
	clk.advance(50 * time.Second)
	if v, ok := c.Touch("x:1", time.Minute); !ok || v != 1 {
		t.Fatalf("touch %d %v", v, ok)
	}
	clk.advance(30 * time.Second)
	if _, ok := c.Get("x:1"); !ok {
		t.Fatal("a touched entry expired")
	}
	if _, ok := c.Touch("x:2", time.Minute); ok {
		t.Fatal("an expired entry was revived")
	}
	c.DeleteFunc(func(key string) bool { return key[0] == 'x' })
	if _, ok := c.Get("x:1"); ok {
		t.Fatal("DeleteFunc kept a match")
	}
}

func TestDoLoadsOnceForConcurrentCallersAndCachesTheAnswer(t *testing.T) {
	tr := newTracker(t)
	c := New[string, int](Options{TTL: time.Minute, Spawn: tr.spawn})
	var loads atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) (int, error) {
		loads.Add(1)
		<-release
		return 7, nil
	}
	results := make(chan int, 2)
	for range 2 {
		go func() {
			v, err := c.Do(context.Background(), "k", load)
			if err != nil {
				v = -1
			}
			results <- v
		}()
	}
	for c.flights.Waiting("k") < 2 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	if a, b := <-results, <-results; a != 7 || b != 7 {
		t.Fatalf("results %d %d", a, b)
	}
	if v, err := c.Do(context.Background(), "k", load); err != nil || v != 7 || loads.Load() != 1 {
		t.Fatalf("cached call: %d %v after %d loads", v, err, loads.Load())
	}
}

func TestDoDoesNotCacheFailures(t *testing.T) {
	c := New[string, int](Options{TTL: time.Minute})
	boom := errors.New("boom")
	if _, err := c.Do(context.Background(), "k", func(context.Context) (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if v, err := c.Do(context.Background(), "k", func(context.Context) (int, error) { return 5, nil }); err != nil || v != 5 {
		t.Fatalf("retry %d %v", v, err)
	}
}

func TestFlightRunsOnceForConcurrentCallersAndOutlivesTheFirst(t *testing.T) {
	tr := newTracker(t)
	group := NewFlight[string, int](tr.spawn, time.Minute)
	var runs atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	fn := func(context.Context) (int, error) {
		runs.Add(1)
		started <- struct{}{}
		<-release
		return 42, nil
	}
	first, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := group.Do(first, "key", fn)
		firstDone <- err
	}()
	<-started
	secondDone := make(chan int, 1)
	go func() {
		value, err := group.Do(context.Background(), "key", fn)
		if err != nil {
			value = -1
		}
		secondDone <- value
	}()
	// The caller that started the flight leaves; the other must still get the answer.
	for group.Waiting("key") < 2 {
		time.Sleep(time.Millisecond)
	}
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("the departed caller got %v", err)
	}
	close(release)
	if value := <-secondDone; value != 42 {
		t.Fatalf("the remaining caller got %d", value)
	}
	if runs.Load() != 1 {
		t.Fatalf("the work ran %d times", runs.Load())
	}
}

func TestFlightIsCancelledWhenEveryCallerHasLeft(t *testing.T) {
	tr := newTracker(t)
	group := NewFlight[string, int](tr.spawn, time.Minute)
	cancelled := make(chan struct{})
	started := make(chan struct{})
	fn := func(ctx context.Context) (int, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return 0, ctx.Err()
	}
	caller, leave := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := group.Do(caller, "key", fn)
		done <- err
	}()
	<-started
	leave()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("abandoned work kept running")
	}
	// A later caller starts a fresh execution instead of joining the cancelled one.
	value, err := group.Do(context.Background(), "key", func(context.Context) (int, error) { return 7, nil })
	if err != nil || value != 7 {
		t.Fatalf("fresh call: %d %v", value, err)
	}
}

func TestFlightIsBoundedByItsTimeout(t *testing.T) {
	tr := newTracker(t)
	group := NewFlight[string, int](tr.spawn, 20*time.Millisecond)
	_, err := group.Do(context.Background(), "key", func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a flight outlived its timeout: %v", err)
	}
}

func TestFlightRefusedDuringShutdown(t *testing.T) {
	tr := newTracker(t)
	tr.stop()
	group := NewFlight[string, int](tr.spawn, time.Minute)
	if _, err := group.Do(context.Background(), "key", func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, ErrRefused) {
		t.Fatalf("work started during shutdown: %v", err)
	}
}

func TestFlightIsCancelledByItsOwnersShutdown(t *testing.T) {
	tr := newTracker(t)
	group := NewFlight[string, int](tr.spawn, time.Minute)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := group.Do(context.Background(), "key", func(ctx context.Context) (int, error) {
			close(started)
			<-ctx.Done()
			return 0, ctx.Err()
		})
		done <- err
	}()
	<-started
	tr.stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
