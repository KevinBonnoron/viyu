package jobs

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
)

// Status is where a job stands. The values are what the API reports.
type Status string

const (
	Pending  Status = "pending"
	Running  Status = "running"
	Complete Status = "complete"
	Failed   Status = "error"
)

// Terminal tells whether the job will not run again: it completed, or it failed for good. A job that failed
// and waits for its next attempt is Pending.
func (s Status) Terminal() bool { return s == Complete || s == Failed }

// Merge combines the statuses of several jobs into the one that describes them all: something running
// outranks something pending, which outranks a failure, which outranks completion.
func Merge(a, b Status) Status {
	rank := func(s Status) int {
		switch s {
		case Running:
			return 3
		case Pending:
			return 2
		case Failed:
			return 1
		}
		return 0
	}
	if rank(b) > rank(a) {
		return b
	}
	if a == "" {
		return Complete
	}
	return a
}

// Counts tallies jobs by status.
type Counts struct {
	Total    int `json:"total"`
	Complete int `json:"complete"`
	Running  int `json:"running"`
	// Pending includes the jobs that failed and wait for their next attempt.
	Pending int `json:"pending"`
	// Error is the jobs that failed for good: the ones that cannot succeed (their video is gone, say), which
	// are not retried and which nothing waits for.
	Error int `json:"error"`
	// Waiting is the part of Pending that failed and sleeps until its next attempt: nothing is running for
	// it, so it neither holds a channel back from being analysed nor counts as work in progress.
	Waiting int `json:"-"`
}

// Add counts one job.
func (c *Counts) Add(status Status) {
	c.Total++
	switch status {
	case Complete:
		c.Complete++
	case Running:
		c.Running++
	case Pending:
		c.Pending++
	default:
		c.Error++
	}
}

// Merge adds another tally.
func (c *Counts) Merge(other Counts) {
	c.Total += other.Total
	c.Complete += other.Complete
	c.Running += other.Running
	c.Pending += other.Pending
	c.Error += other.Error
	c.Waiting += other.Waiting
}

// Entry is a copy of a job as the queue knows it.
type Entry[In, Out any] struct {
	Status Status
	// Code is the fixed code of a failure ("timeout", "cancelled", "unavailable", "rate_limited" or the
	// one Options.Code chose): provider and process errors may hold command lines, cookies or URLs, so
	// they never reach a job.
	Code     string
	In       In
	Out      Out
	Attempts int
	// Failures is how many times the job failed and was left to try again later (Options.Backoff).
	Failures int
	// RetryAt is when a failed job runs again; zero unless it is waiting for that.
	RetryAt time.Time
	Updated time.Time
}

// Admission is the answer of Submit.
type Admission int

const (
	// Admitted: the job is new (or was started again) and will run.
	Admitted Admission = iota
	// Existing: the key already has a job; its current state is returned.
	Existing
	// Refused: too many unfinished jobs, or the owner is shutting down. Nothing was queued.
	Refused
)

// Options configures a Queue.
type Options[K comparable, In, Out any] struct {
	// Concurrency is the number of jobs running at once; default 2.
	Concurrency int
	// MaxUnfinished bounds the pending and running jobs; Submit refuses beyond it. Default 32.
	MaxUnfinished int
	// MaxFinished bounds the finished jobs kept (the least recently read leave first). Default 256.
	MaxFinished int
	// MaxAttempts is how many times a job may run when Retry keeps asking for another turn; default 1.
	MaxAttempts int
	// Timeout bounds one run; zero is unbounded.
	Timeout time.Duration
	// Spawn owns the goroutines of the queue (required): their context is the owner's lifetime.
	Spawn cache.Spawner
	// Run executes a job.
	Run func(ctx context.Context, key K, in In) (Out, error)
	// Code names a failure with a fixed code; default "timeout", "cancelled" or "failed".
	Code func(err error) string
	// Retry says whether a failure is transient: the job is queued again after the returned pause, which
	// also holds back every other job of the queue (a rate limit concerns them all).
	Retry func(err error) (pause time.Duration, retry bool)
	// Backoff, when set, keeps a job that failed for another reason than a rate limit (see Retry) in the
	// queue: it sleeps for the pause Backoff.Delay gives for its number of failures, then runs again, for as
	// long as Final does not say the failure is permanent. The sleeping job counts as pending, takes no slot
	// of MaxUnfinished and no running slot, and runs under the queue's concurrency and pause like any other.
	Backoff *Backoff
	// Final says a failure is not worth another attempt (the video was removed, the key is never retried):
	// the job then fails for good. Nil means no failure is final. Only used with Backoff.
	Final func(key K, err error) bool
	// OnFinish is called, without a lock held, when a job completes or fails for good.
	OnFinish func(key K, entry Entry[In, Out])
	// OnRetry is called, without a lock held, when a job failed and was left to try again later.
	OnRetry func(key K, entry Entry[In, Out])
	// Now replaces the clock (tests).
	Now func() time.Time
}

type job[K comparable, In, Out any] struct {
	key      K
	entry    Entry[In, Out]
	started  bool
	cancel   context.CancelFunc
	accessed time.Time
	done     chan struct{}
}

// Queue runs keyed jobs under bounds: a number running at once, a number unfinished, a number of finished
// ones remembered. Submitting a key that has a job returns that job instead of starting another.
type Queue[K comparable, In, Out any] struct {
	opts Options[K, In, Out]
	now  func() time.Time

	mu      sync.Mutex
	jobs    map[K]*job[K, In, Out]
	order   []K                    // unfinished jobs in submission order
	waiting map[K]*job[K, In, Out] // failed jobs asleep until their RetryAt; not in order
	paused  time.Time
	timerAt time.Time              // when the earliest armed timer fires; zero when none is
	outbox  []finished[K, In, Out] // jobs whose OnFinish (or OnRetry) has not run yet
}

type finished[K comparable, In, Out any] struct {
	key   K
	entry Entry[In, Out]
	retry bool // the job did not finish: it failed and waits for its next attempt
}

// NewQueue builds a Queue.
func NewQueue[K comparable, In, Out any](opts Options[K, In, Out]) *Queue[K, In, Out] {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if opts.MaxUnfinished <= 0 {
		opts.MaxUnfinished = 32
	}
	if opts.MaxFinished <= 0 {
		opts.MaxFinished = 256
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 1
	}
	if opts.Code == nil {
		opts.Code = defaultCode
	}
	q := &Queue[K, In, Out]{opts: opts, now: opts.Now, jobs: map[K]*job[K, In, Out]{}, waiting: map[K]*job[K, In, Out]{}}
	if q.now == nil {
		q.now = time.Now
	}
	return q
}

func defaultCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return "failed"
}

// Submit queues the job of key unless it has one. A finished job is not run again (it is a result to read);
// Restart does that.
func (q *Queue[K, In, Out]) Submit(key K, in In) (Entry[In, Out], Admission) {
	q.mu.Lock()
	if existing := q.jobs[key]; existing != nil {
		existing.accessed = q.now()
		entry := existing.entry
		q.mu.Unlock()
		return entry, Existing
	}
	entry, admitted := q.admitLocked(key, in)
	q.unlock()
	return entry, admitted
}

// Restart runs the job of key again with a new input: a failed job (retry), or a finished one that went
// stale. A job that is pending or running is left alone and returned as Existing.
func (q *Queue[K, In, Out]) Restart(key K, in In) (Entry[In, Out], Admission) {
	q.mu.Lock()
	if existing := q.jobs[key]; existing != nil {
		if !existing.entry.Status.Terminal() {
			existing.accessed = q.now()
			entry := existing.entry
			q.mu.Unlock()
			return entry, Existing
		}
		delete(q.jobs, key)
	}
	entry, admitted := q.admitLocked(key, in)
	q.unlock()
	return entry, admitted
}

// unlock releases the lock and then delivers what finished meanwhile to OnFinish, and what failed to be
// tried again to OnRetry.
func (q *Queue[K, In, Out]) unlock() {
	done := q.outbox
	q.outbox = nil
	q.mu.Unlock()
	for _, f := range done {
		switch {
		case f.retry && q.opts.OnRetry != nil:
			q.opts.OnRetry(f.key, f.entry)
		case !f.retry && q.opts.OnFinish != nil:
			q.opts.OnFinish(f.key, f.entry)
		}
	}
}

// admitLocked adds a pending job and starts what can run.
func (q *Queue[K, In, Out]) admitLocked(key K, in In) (Entry[In, Out], Admission) {
	if len(q.order) >= q.opts.MaxUnfinished {
		return Entry[In, Out]{Status: Pending, In: in}, Refused
	}
	now := q.now()
	j := &job[K, In, Out]{key: key, entry: Entry[In, Out]{Status: Pending, In: in, Updated: now}, accessed: now, done: make(chan struct{})}
	q.jobs[key] = j
	q.order = append(q.order, key)
	q.evictLocked()
	q.drainLocked()
	return j.entry, Admitted
}

// evictLocked drops the least recently read finished jobs beyond the bound.
func (q *Queue[K, In, Out]) evictLocked() {
	for len(q.jobs)-len(q.order)-len(q.waiting) > q.opts.MaxFinished {
		var oldestKey K
		var oldest *job[K, In, Out]
		for key, j := range q.jobs {
			if j.entry.Status.Terminal() && (oldest == nil || j.accessed.Before(oldest.accessed)) {
				oldestKey, oldest = key, j
			}
		}
		if oldest == nil {
			return
		}
		delete(q.jobs, oldestKey)
	}
}

// drainLocked brings the jobs whose sleep ended back into the queue, starts pending jobs while there is
// room unless the queue is paused, and arms the timer for whatever has to happen next.
func (q *Queue[K, In, Out]) drainLocked() {
	q.wakeLocked()
	if until := q.paused; until.After(q.now()) {
		q.armTimerLocked(until)
	} else {
		q.startLocked()
	}
	if next, ok := q.nextWakeLocked(); ok {
		q.armTimerLocked(next)
	}
}

// wakeLocked queues again the failed jobs whose sleep is over. They are admitted whatever MaxUnfinished
// says: they were already admitted once, and refusing them would lose the retry.
func (q *Queue[K, In, Out]) wakeLocked() {
	now := q.now()
	for key, j := range q.waiting {
		if j.entry.RetryAt.After(now) {
			continue
		}
		delete(q.waiting, key)
		j.entry.RetryAt = time.Time{}
		j.entry.Attempts = 0
		j.entry.Updated = now
		q.order = append(q.order, key)
	}
}

// nextWakeLocked is when the first sleeping job is due.
func (q *Queue[K, In, Out]) nextWakeLocked() (time.Time, bool) {
	var next time.Time
	for _, j := range q.waiting {
		if next.IsZero() || j.entry.RetryAt.Before(next) {
			next = j.entry.RetryAt
		}
	}
	return next, !next.IsZero()
}

func (q *Queue[K, In, Out]) startLocked() {
	running := 0
	for _, key := range q.order {
		if q.jobs[key].started {
			running++
		}
	}
	// A job that cannot be started finishes, which edits q.order: walk a copy.
	for _, key := range slices.Clone(q.order) {
		if running >= q.opts.Concurrency {
			return
		}
		j := q.jobs[key]
		if j.started {
			continue
		}
		j.started = true
		if !q.opts.Spawn(func(ctx context.Context) { q.run(ctx, j) }) {
			// The owner is stopping: nothing will ever run this job.
			j.started = false
			q.finishLocked(j, Failed, "unavailable")
			continue
		}
		running++
	}
}

// armTimerLocked drains the queue again at the given time, unless a timer that fires no later is armed
// already. A timer that fires for nothing is harmless: it drains and arms the next one.
func (q *Queue[K, In, Out]) armTimerLocked(at time.Time) {
	if !q.timerAt.IsZero() && !at.Before(q.timerAt) {
		return
	}
	q.timerAt = at
	delay := at.Sub(q.now())
	if !q.opts.Spawn(func(ctx context.Context) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
		q.mu.Lock()
		if q.timerAt.Equal(at) {
			q.timerAt = time.Time{}
		}
		if ctx.Err() == nil {
			q.drainLocked()
		}
		q.unlock()
	}) {
		q.timerAt = time.Time{}
	}
}

func (q *Queue[K, In, Out]) run(ctx context.Context, j *job[K, In, Out]) {
	q.mu.Lock()
	if q.jobs[j.key] != j { // forgotten while it waited
		q.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	if q.opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, q.opts.Timeout)
	}
	j.cancel = cancel
	j.entry.Status = Running
	j.entry.Attempts++
	j.entry.Updated = q.now()
	key, in := j.key, j.entry.In
	q.mu.Unlock()

	out, err := q.opts.Run(runCtx, key, in)
	cancel()

	q.mu.Lock()
	j.cancel = nil
	if q.jobs[j.key] != j { // forgotten while it ran: nobody is waiting for the answer
		q.mu.Unlock()
		return
	}
	j.started = false
	switch {
	case err == nil:
		j.entry.Out = out
		q.finishLocked(j, Complete, "")
	case ctx.Err() == nil && q.pauseLocked(j, err):
		q.drainLocked()
		q.unlock()
		return
	case ctx.Err() == nil && q.sleepLocked(j, err):
		q.drainLocked()
		q.unlock()
		return
	default:
		q.finishLocked(j, Failed, q.opts.Code(err))
	}
	q.drainLocked()
	q.unlock()
}

// pauseLocked holds the whole queue back and keeps the job for another turn when Retry calls the failure
// transient and the job has turns left. A rate limit concerns every job of the queue, not just this one.
func (q *Queue[K, In, Out]) pauseLocked(j *job[K, In, Out], err error) bool {
	if q.opts.Retry == nil || j.entry.Attempts >= q.opts.MaxAttempts {
		return false
	}
	pause, retry := q.opts.Retry(err)
	if !retry {
		return false
	}
	q.paused = q.now().Add(pause)
	j.entry.Status = Pending
	j.entry.Code = "rate_limited"
	j.entry.Updated = q.now()
	return true
}

// sleepLocked takes a failed job out of the queue until its next attempt, which is later the more often it
// failed. False when the queue does not retry this failure.
func (q *Queue[K, In, Out]) sleepLocked(j *job[K, In, Out], err error) bool {
	if q.opts.Backoff == nil || (q.opts.Final != nil && q.opts.Final(j.key, err)) {
		return false
	}
	now := q.now()
	j.entry.Failures++
	j.entry.Status = Pending
	j.entry.Code = q.opts.Code(err)
	j.entry.RetryAt = now.Add(q.opts.Backoff.Delay(j.entry.Failures))
	j.entry.Updated = now
	q.order = slices.DeleteFunc(q.order, func(key K) bool { return key == j.key })
	q.waiting[j.key] = j
	q.outbox = append(q.outbox, finished[K, In, Out]{key: j.key, entry: j.entry, retry: true})
	return true
}

// finishLocked moves a job to a terminal status.
func (q *Queue[K, In, Out]) finishLocked(j *job[K, In, Out], status Status, code string) {
	j.entry.Status, j.entry.Code, j.entry.Updated = status, code, q.now()
	q.order = slices.DeleteFunc(q.order, func(key K) bool { return key == j.key })
	close(j.done)
	q.outbox = append(q.outbox, finished[K, In, Out]{key: j.key, entry: j.entry})
	q.evictLocked()
}

// Get returns a copy of the job of key and counts the read as a use.
func (q *Queue[K, In, Out]) Get(key K) (Entry[In, Out], bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j := q.jobs[key]
	if j == nil {
		return Entry[In, Out]{}, false
	}
	j.accessed = q.now()
	return j.entry, true
}

// Counts tallies the jobs whose key matches (all of them when match is nil).
func (q *Queue[K, In, Out]) Counts(match func(K) bool) Counts {
	q.mu.Lock()
	defer q.mu.Unlock()
	var counts Counts
	for key, j := range q.jobs {
		if match != nil && !match(key) {
			continue
		}
		counts.Add(j.entry.Status)
		if j.entry.Status == Pending && !j.entry.RetryAt.IsZero() {
			counts.Waiting++
		}
	}
	return counts
}

// Unfinished is the number of pending and running jobs.
func (q *Queue[K, In, Out]) Unfinished() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.order)
}

// Forget drops the jobs whose key matches, cancelling the ones running; their results are never delivered.
func (q *Queue[K, In, Out]) Forget(match func(K) bool) {
	q.mu.Lock()
	for key, j := range q.jobs {
		if !match(key) {
			continue
		}
		if j.cancel != nil {
			j.cancel()
		}
		if !j.entry.Status.Terminal() {
			close(j.done)
		}
		delete(q.jobs, key)
		delete(q.waiting, key)
	}
	kept := q.order[:0]
	for _, key := range q.order {
		if q.jobs[key] != nil {
			kept = append(kept, key)
		}
	}
	q.order = kept
	q.drainLocked()
	q.unlock()
}

// Wait blocks until the job of key is finished (or forgotten) and returns its entry.
func (q *Queue[K, In, Out]) Wait(ctx context.Context, key K) (Entry[In, Out], error) {
	q.mu.Lock()
	j := q.jobs[key]
	q.mu.Unlock()
	if j == nil {
		return Entry[In, Out]{}, errors.New("no such job")
	}
	select {
	case <-j.done:
	case <-ctx.Done():
		return Entry[In, Out]{}, ctx.Err()
	}
	entry, _ := q.Get(key)
	return entry, nil
}
