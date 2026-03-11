package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testQueue = Queue[string, int, int]

func newTestQueue(t *testing.T, opts Options[string, int, int]) *testQueue {
	t.Helper()
	group := NewGroup()
	t.Cleanup(group.Stop)
	opts.Spawn = group.Go
	return NewQueue(opts)
}

func wait(t *testing.T, q *testQueue, key string) Entry[int, int] {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entry, err := q.Wait(ctx, key)
	if err != nil {
		t.Fatalf("waiting for %s: %v", key, err)
	}
	return entry
}

func TestAJobRunsOnceAndKeepsItsResult(t *testing.T) {
	t.Parallel()
	var runs atomic.Int32
	q := newTestQueue(t, Options[string, int, int]{Run: func(_ context.Context, _ string, in int) (int, error) {
		runs.Add(1)
		return in * 2, nil
	}})
	if entry, admission := q.Submit("a", 21); admission != Admitted || entry.Status != Pending {
		t.Fatalf("submit: %+v %v", entry, admission)
	}
	if entry := wait(t, q, "a"); entry.Status != Complete || entry.Out != 42 || entry.Attempts != 1 {
		t.Fatalf("finished: %+v", entry)
	}
	if entry, admission := q.Submit("a", 1); admission != Existing || entry.Out != 42 {
		t.Fatalf("a finished job is a result to read: %+v %v", entry, admission)
	}
	if runs.Load() != 1 {
		t.Fatalf("runs: %d", runs.Load())
	}
}

func TestConcurrencyAndUnfinishedBoundsHold(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var running, peak atomic.Int32
	q := newTestQueue(t, Options[string, int, int]{Concurrency: 2, MaxUnfinished: 3, Run: func(ctx context.Context, _ string, _ int) (int, error) {
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		defer running.Add(-1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return 0, nil
	}})
	for _, key := range []string{"a", "b", "c"} {
		if _, admission := q.Submit(key, 0); admission != Admitted {
			t.Fatalf("%s: %v", key, admission)
		}
	}
	if _, admission := q.Submit("d", 0); admission != Refused {
		t.Fatalf("a full queue admitted a job: %v", admission)
	}
	if q.Unfinished() != 3 {
		t.Fatalf("unfinished: %d", q.Unfinished())
	}
	close(release)
	for _, key := range []string{"a", "b", "c"} {
		wait(t, q, key)
	}
	if peak.Load() > 2 {
		t.Fatalf("%d jobs ran at once", peak.Load())
	}
	if _, admission := q.Submit("d", 0); admission != Admitted {
		t.Fatal("room freed by finished jobs was not reused")
	}
}

func TestFinishedJobsAreAnLRUAndFailuresCarryOnlyAFixedCode(t *testing.T) {
	t.Parallel()
	q := newTestQueue(t, Options[string, int, int]{MaxFinished: 2, Run: func(_ context.Context, key string, _ int) (int, error) {
		if key == "bad" {
			return 0, errors.New("yt-dlp --cookies /secret/jar failed")
		}
		return 1, nil
	}})
	for _, key := range []string{"bad", "a", "b"} {
		q.Submit(key, 0)
		wait(t, q, key)
	}
	if _, ok := q.Get("bad"); ok {
		t.Fatal("the least recently used finished job was kept")
	}
	if _, ok := q.Get("a"); !ok { // a read renews it
		t.Fatal("a was evicted early")
	}
	q.Submit("c", 0)
	wait(t, q, "c")
	if _, ok := q.Get("b"); ok {
		t.Fatal("b should be the least recently used")
	}
	q.Submit("bad", 0)
	entry := wait(t, q, "bad")
	if entry.Status != Failed || entry.Code != "failed" {
		t.Fatalf("failure: %+v", entry)
	}
}

func TestRestartRunsAFailedJobAgainButNeverAnUnfinishedOne(t *testing.T) {
	t.Parallel()
	var runs atomic.Int32
	release := make(chan struct{})
	q := newTestQueue(t, Options[string, int, int]{Run: func(ctx context.Context, _ string, _ int) (int, error) {
		if runs.Add(1) == 1 {
			return 0, errors.New("first run fails")
		}
		<-release
		return 7, nil
	}})
	q.Submit("a", 1)
	if entry := wait(t, q, "a"); entry.Status != Failed {
		t.Fatalf("first: %+v", entry)
	}
	if _, admission := q.Restart("a", 2); admission != Admitted {
		t.Fatalf("restart: %v", admission)
	}
	if entry, admission := q.Restart("a", 3); admission != Existing || entry.In != 2 {
		t.Fatalf("an unfinished job was restarted: %+v %v", entry, admission)
	}
	close(release)
	if entry := wait(t, q, "a"); entry.Status != Complete || entry.Out != 7 || entry.Attempts != 1 {
		t.Fatalf("second: %+v", entry)
	}
}

func TestATransientFailurePausesTheWholeQueueAndRetriesBoundedTimes(t *testing.T) {
	t.Parallel()
	var clock struct {
		sync.Mutex
		now time.Time
	}
	clock.now = time.Unix(1000, 0)
	now := func() time.Time { clock.Lock(); defer clock.Unlock(); return clock.now }
	var limited atomic.Int32
	var otherRan atomic.Bool
	q := newTestQueue(t, Options[string, int, int]{
		MaxAttempts: 2, Now: now,
		Run: func(_ context.Context, key string, _ int) (int, error) {
			if strings.HasPrefix(key, "other") {
				otherRan.Store(true)
				return 0, nil
			}
			limited.Add(1)
			return 0, errors.New("HTTP Error 429")
		},
		Retry: func(error) (time.Duration, bool) { return time.Hour, true },
	})
	q.Submit("a", 0)
	for q.Counts(nil).Pending == 0 || limited.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	// The pause holds back every job of the queue, not just the one that was limited.
	q.Submit("other", 0)
	time.Sleep(20 * time.Millisecond)
	if otherRan.Load() {
		t.Fatal("a job ran during the pause")
	}
	if entry, _ := q.Get("a"); entry.Status != Pending || entry.Code != "rate_limited" {
		t.Fatalf("during the pause: %+v", entry)
	}
	clock.Lock()
	clock.now = clock.now.Add(2 * time.Hour)
	clock.Unlock()
	// The timer armed for the pause fires on the real clock; wake the queue the way a later submission would.
	q.Submit("other-poke", 0)
	entry := wait(t, q, "a")
	if entry.Status != Failed || entry.Attempts != 2 || limited.Load() != 2 {
		t.Fatalf("bounded retries: %+v after %d runs", entry, limited.Load())
	}
	wait(t, q, "other")
	if !otherRan.Load() {
		t.Fatal("the queue never resumed")
	}
}

func TestForgetCancelsRunningJobsAndCountsFollowTheStatuses(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	q := newTestQueue(t, Options[string, int, int]{Concurrency: 1, Run: func(ctx context.Context, key string, _ int) (int, error) {
		if key == "slow" {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return 0, ctx.Err()
		}
		return 0, nil
	}})
	q.Submit("slow", 0)
	q.Submit("queued", 0)
	<-started
	if c := q.Counts(nil); c.Total != 2 || c.Running != 1 || c.Pending != 1 {
		t.Fatalf("counts: %+v", c)
	}
	q.Forget(func(key string) bool { return key == "slow" })
	<-cancelled
	if _, ok := q.Get("slow"); ok {
		t.Fatal("a forgotten job is still known")
	}
	if entry := wait(t, q, "queued"); entry.Status != Complete {
		t.Fatalf("the queue did not move on: %+v", entry)
	}
	if c := q.Counts(func(key string) bool { return key == "queued" }); c.Complete != 1 || c.Total != 1 {
		t.Fatalf("filtered counts: %+v", c)
	}
}

func TestOnFinishRunsOutsideTheLockAndATimeoutBecomesACode(t *testing.T) {
	t.Parallel()
	finished := make(chan Entry[int, int], 1)
	var q *testQueue
	q = newTestQueue(t, Options[string, int, int]{Timeout: 20 * time.Millisecond,
		Run: func(ctx context.Context, _ string, _ int) (int, error) { <-ctx.Done(); return 0, ctx.Err() },
		OnFinish: func(key string, entry Entry[int, int]) {
			q.Counts(nil) // would deadlock if OnFinish ran under the queue's lock
			finished <- entry
		},
	})
	q.Submit("a", 0)
	select {
	case entry := <-finished:
		if entry.Status != Failed || entry.Code != "timeout" {
			t.Fatalf("entry: %+v", entry)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OnFinish never ran")
	}
}

func TestAStoppedOwnerRefusesWorkAndFailsWhatWasQueued(t *testing.T) {
	t.Parallel()
	group := NewGroup()
	q := NewQueue(Options[string, int, int]{Spawn: group.Go, Run: func(context.Context, string, int) (int, error) { return 0, nil }})
	group.Stop()
	if _, admission := q.Submit("a", 0); admission != Admitted {
		t.Fatalf("submit: %v", admission)
	}
	if got, _ := q.Get("a"); got.Status != Failed || got.Code != "unavailable" {
		t.Fatalf("after the owner stopped: %+v", got)
	}
}

func TestMergePrefersWhatIsStillMoving(t *testing.T) {
	t.Parallel()
	cases := []struct{ a, b, want Status }{
		{Complete, Complete, Complete}, {Complete, Failed, Failed}, {Failed, Pending, Pending},
		{Pending, Running, Running}, {Running, Failed, Running}, {"", Complete, Complete},
	}
	for _, c := range cases {
		if got := Merge(c.a, c.b); got != c.want {
			t.Fatalf("Merge(%q,%q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
