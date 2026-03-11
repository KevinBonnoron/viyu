package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackoffDoublesFromTheBaseUpToTheMaximumAndOnlyShortens(t *testing.T) {
	t.Parallel()
	b := Backoff{Base: 5 * time.Minute, Max: 6 * time.Hour, Random: func() float64 { return 0 }}
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 80 * time.Minute, 160 * time.Minute, 320 * time.Minute, 6 * time.Hour, 6 * time.Hour}
	for i, expected := range want {
		if got := b.Delay(i + 1); got != expected {
			t.Fatalf("pause after failure %d: %v, want %v", i+1, got, expected)
		}
	}
	jittered := newBackoff(0, 0)
	if jittered.Base != 5*time.Minute || jittered.Max != 6*time.Hour {
		t.Fatalf("defaults: %+v", jittered)
	}
	seen := map[time.Duration]bool{}
	for range 200 {
		got := jittered.Delay(20)
		if got > 6*time.Hour || got < time.Duration(float64(6*time.Hour)*(1-retryJitter))-time.Second {
			t.Fatalf("a pause outside [%v, %v]: %v", time.Duration(float64(6*time.Hour)*(1-retryJitter)), 6*time.Hour, got)
		}
		seen[got] = true
	}
	if len(seen) < 100 {
		t.Fatalf("the pauses are not spread: %d distinct values", len(seen))
	}
}

// movingClock is a clock a test moves; queues read it for their backoff and pause.
type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movingClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func exactBackoff(base, max time.Duration) *Backoff {
	return &Backoff{Base: base, Max: max, Random: func() float64 { return 0 }}
}

func TestAFailedJobSleepsWithoutHoldingASlotAndRunsAgainWhenDue(t *testing.T) {
	t.Parallel()
	clock := &movingClock{now: time.Unix(1000, 0)}
	var runs atomic.Int32
	var retried, finished atomic.Int32
	q := newTestQueue(t, Options[string, int, int]{
		Concurrency: 1, MaxUnfinished: 1, Now: clock.Now,
		Backoff:  exactBackoff(time.Minute, time.Hour),
		OnRetry:  func(string, Entry[int, int]) { retried.Add(1) },
		OnFinish: func(string, Entry[int, int]) { finished.Add(1) },
		Run: func(_ context.Context, key string, _ int) (int, error) {
			if key == "a" && runs.Add(1) <= 2 {
				return 0, errors.New("network down")
			}
			return 7, nil
		},
	})
	q.Submit("a", 1)
	waitUntil(t, "the first failure", func() bool { e, _ := q.Get("a"); return e.Failures == 1 })
	entry, _ := q.Get("a")
	if entry.Status != Pending || entry.Code != "failed" || !entry.RetryAt.Equal(clock.Now().Add(time.Minute)) {
		t.Fatalf("sleeping: %+v", entry)
	}
	if c := q.Counts(nil); c != (Counts{Total: 1, Pending: 1, Waiting: 1}) || q.Unfinished() != 0 {
		t.Fatalf("a sleeping job is pending and takes no slot: %+v unfinished=%d", c, q.Unfinished())
	}
	// With the one slot free, another job is admitted and runs while the first sleeps.
	if _, admission := q.Submit("b", 2); admission != Admitted {
		t.Fatalf("the sleeping job held the slot: %v", admission)
	}
	if entry := wait(t, q, "b"); entry.Status != Complete {
		t.Fatalf("b: %+v", entry)
	}
	poke(q)
	if runs.Load() != 1 {
		t.Fatalf("the job ran again before its pause ended: %d runs", runs.Load())
	}
	// Due: it fails once more and sleeps twice as long.
	clock.advance(time.Minute)
	poke(q)
	waitUntil(t, "the second failure", func() bool { e, _ := q.Get("a"); return e.Failures == 2 })
	entry, _ = q.Get("a")
	if entry.Status != Pending || !entry.RetryAt.Equal(clock.Now().Add(2*time.Minute)) {
		t.Fatalf("second failure: %+v", entry)
	}
	clock.advance(2 * time.Minute)
	poke(q)
	if entry := wait(t, q, "a"); entry.Status != Complete || entry.Out != 7 || entry.Failures != 2 || entry.Attempts != 1 || !entry.RetryAt.IsZero() {
		t.Fatalf("third run: %+v", entry)
	}
	if retried.Load() != 2 || finished.Load() != 2 {
		t.Fatalf("callbacks: retried=%d finished=%d", retried.Load(), finished.Load())
	}
}

func TestAFailureThatIsFinalEndsTheJobForGood(t *testing.T) {
	t.Parallel()
	var runs atomic.Int32
	var retried atomic.Int32
	gone := errors.New("video removed")
	q := newTestQueue(t, Options[string, int, int]{
		Backoff: exactBackoff(time.Millisecond, time.Millisecond),
		Final:   func(_ string, err error) bool { return errors.Is(err, gone) },
		OnRetry: func(string, Entry[int, int]) { retried.Add(1) },
		Run: func(context.Context, string, int) (int, error) {
			runs.Add(1)
			return 0, gone
		},
	})
	q.Submit("a", 1)
	if entry := wait(t, q, "a"); entry.Status != Failed || entry.Failures != 0 || !entry.RetryAt.IsZero() {
		t.Fatalf("final failure: %+v", entry)
	}
	time.Sleep(20 * time.Millisecond)
	if runs.Load() != 1 || retried.Load() != 0 {
		t.Fatalf("a final failure was retried: runs=%d retried=%d", runs.Load(), retried.Load())
	}
	if c := q.Counts(nil); c != (Counts{Total: 1, Error: 1}) {
		t.Fatalf("counts: %+v", c)
	}
}

func TestASleepingJobWaitsForTheQueuesRateLimitPauseAndCanBeForgotten(t *testing.T) {
	t.Parallel()
	clock := &movingClock{now: time.Unix(1000, 0)}
	var aRuns, cRuns atomic.Int32
	q := newTestQueue(t, Options[string, int, int]{
		Concurrency: 2, MaxAttempts: 2, Now: clock.Now,
		Backoff: exactBackoff(time.Minute, time.Hour),
		Retry: func(err error) (time.Duration, bool) {
			if err.Error() == "429" {
				return time.Hour, true
			}
			return 0, false
		},
		Run: func(_ context.Context, key string, _ int) (int, error) {
			switch key {
			case "a":
				if aRuns.Add(1) == 1 {
					return 0, errors.New("network down")
				}
			case "limited":
				return 0, errors.New("429")
			case "c":
				cRuns.Add(1)
				return 0, errors.New("network down")
			}
			return 1, nil
		},
	})
	q.Submit("a", 0)
	waitUntil(t, "a to sleep", func() bool { e, _ := q.Get("a"); return e.Failures == 1 })
	q.Submit("c", 0)
	waitUntil(t, "c to sleep", func() bool { e, _ := q.Get("c"); return e.Failures == 1 })
	// A job that is rate limited pauses the whole queue, the sleeping ones included.
	q.Submit("limited", 0)
	waitUntil(t, "the queue to pause", func() bool { e, _ := q.Get("limited"); return e.Code == "rate_limited" })
	q.Forget(func(key string) bool { return key == "c" })
	if _, ok := q.Get("c"); ok {
		t.Fatal("a forgotten sleeping job is still known")
	}
	clock.advance(2 * time.Minute)
	poke(q)
	if entry, _ := q.Get("a"); entry.Status == Complete {
		t.Fatalf("a job ran again while the queue was paused: %+v", entry)
	}
	if aRuns.Load() != 1 {
		t.Fatalf("a ran during the pause: %d", aRuns.Load())
	}
	clock.advance(2 * time.Hour)
	poke(q)
	if entry := wait(t, q, "a"); entry.Status != Complete {
		t.Fatalf("a after the pause: %+v", entry)
	}
	if cRuns.Load() != 1 {
		t.Fatalf("a forgotten job ran again: %d", cRuns.Load())
	}
}
