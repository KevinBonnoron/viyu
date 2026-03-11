package jobs

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// AnalysisOptions tunes Analysis; a zero field takes the default.
type AnalysisOptions struct {
	// Debounce batches bursts of wake-ups into one pass without postponing work indefinitely (default
	// 250 milliseconds).
	Debounce time.Duration
	// WakeLimit is how many channels a pass remembers before it collapses to a pass over every subscribed
	// channel (default 128).
	WakeLimit int
	// MetadataConcurrency, MetadataQueueLimit: the videos whose own page is fetched at once, and the
	// fetches pending or running at most (defaults 2 and 32).
	MetadataConcurrency int
	MetadataQueueLimit  int
	// MetadataTimeout bounds one fetch (default 45 seconds).
	MetadataTimeout time.Duration
	// MetadataAttempts is how many times a fetch runs when the platform rate-limits it (default 4).
	MetadataAttempts int
	// RateLimitPause holds the metadata queue back after a rate limit that did not say for how long
	// (default 30 seconds).
	RateLimitPause time.Duration
	// RetryBase and RetryMax bound the pause before a failed metadata fetch or thumbnail reading is tried
	// again: RetryBase after the first failure, doubling after each one, up to RetryMax (defaults 5 minutes
	// and 6 hours).
	RetryBase, RetryMax time.Duration
	// OCRConcurrency, OCRQueueLimit, OCRCacheLimit: the thumbnails read at once, the readings pending or
	// running at most, and the finished readings remembered (defaults 2, 32 and 256).
	OCRConcurrency int
	OCRQueueLimit  int
	OCRCacheLimit  int
	// OCRTimeout bounds the reading of one video (default 2 minutes).
	OCRTimeout time.Duration
	// Now replaces the clock (tests).
	Now func() time.Time
}

func (o AnalysisOptions) withDefaults() AnalysisOptions {
	o.Debounce = positiveDuration(o.Debounce, 250*time.Millisecond)
	o.WakeLimit = positive(o.WakeLimit, 128)
	o.MetadataConcurrency = positive(o.MetadataConcurrency, 2)
	o.MetadataQueueLimit = positive(o.MetadataQueueLimit, 32)
	o.MetadataTimeout = positiveDuration(o.MetadataTimeout, 45*time.Second)
	o.MetadataAttempts = positive(o.MetadataAttempts, 4)
	o.RateLimitPause = positiveDuration(o.RateLimitPause, 30*time.Second)
	o.RetryBase = positiveDuration(o.RetryBase, defaultRetryBase)
	o.RetryMax = positiveDuration(o.RetryMax, defaultRetryMax)
	o.OCRConcurrency = positive(o.OCRConcurrency, 2)
	o.OCRQueueLimit = positive(o.OCRQueueLimit, 32)
	o.OCRCacheLimit = positive(o.OCRCacheLimit, 256)
	o.OCRTimeout = positiveDuration(o.OCRTimeout, 2*time.Minute)
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// markLimit bounds the channels the analysis remembers having analysed.
const markLimit = 1024

// workKey names a job of the analysis: a video of a channel (the record id of the channel the analysis
// works for).
type workKey struct{ channel, platform, video string }

// Analysis keeps the series of channels up to date. One coalescing worker (Run) discovers the work: for
// every channel it wakes for, it makes sure the catalog is synchronized, reads the stored catalog, infers
// the series, has the episode numbers of the unnumbered videos read from their thumbnails (when an
// administrator allowed it and the build can), has the dates series need fetched from the videos' own pages,
// and stores the series. The slow parts are jobs of bounded queues that wake the worker back when they
// finish.
//
// What clients poll (Overview, ChannelStatus) is answered from what is stored and from the counters of the
// queues; it never infers anything.
type Analysis struct {
	app       core.App
	platforms *platform.Registry
	store     *catalog.Store
	sources   *sources.Service
	series    *series.Store
	parser    *series.Parser
	reader    series.EpisodeReader
	catalogs  *Catalogs
	opts      AnalysisOptions

	signal   *Signal
	wakes    wakeSet
	deferred wakeSet // channels that found a queue full, woken again when jobs finish
	marks    marks
	ocrOn    atomic.Bool

	metadata *Queue[workKey, platform.Video, struct{}]
	ocr      *Queue[workKey, platform.Video, int]
}

// NewAnalysis builds the analysis. reader may be nil: thumbnail recognition is then unavailable, whatever the
// administrator chose. spawn owns every goroutine of the queues (jobs.Group.Go); Run is started by the owner.
func NewAnalysis(app core.App, platforms *platform.Registry, store *catalog.Store, sources *sources.Service, seriesStore *series.Store, parser *series.Parser, reader series.EpisodeReader, catalogs *Catalogs, spawn cache.Spawner, opts AnalysisOptions) *Analysis {
	opts = opts.withDefaults()
	a := &Analysis{
		app: app, platforms: platforms, store: store, sources: sources, series: seriesStore, parser: parser, reader: reader, catalogs: catalogs, opts: opts,
		signal:   NewSignal(),
		wakes:    wakeSet{limit: opts.WakeLimit},
		deferred: wakeSet{limit: opts.WakeLimit},
		marks:    marks{state: map[string]Status{}},
	}
	if enabled, err := series.OCREnabled(app); err == nil {
		a.ocrOn.Store(enabled)
	}
	a.metadata = NewQueue(Options[workKey, platform.Video, struct{}]{
		Concurrency:   opts.MetadataConcurrency,
		MaxUnfinished: opts.MetadataQueueLimit,
		MaxAttempts:   opts.MetadataAttempts,
		Timeout:       opts.MetadataTimeout,
		Spawn:         spawn,
		Now:           opts.Now,
		Code:          taskCode("metadata"),
		Retry:         rateLimitPause(opts.RateLimitPause),
		Backoff:       newBackoff(opts.RetryBase, opts.RetryMax),
		Final:         func(_ workKey, err error) bool { return permanentFailure(err) },
		Run:           a.fetchMetadata,
		OnFinish:      a.metadataFinished,
		OnRetry:       a.metadataRetrying,
	})
	a.ocr = NewQueue(Options[workKey, platform.Video, int]{
		Concurrency:   opts.OCRConcurrency,
		MaxUnfinished: opts.OCRQueueLimit,
		MaxFinished:   opts.OCRCacheLimit,
		Timeout:       opts.OCRTimeout,
		Spawn:         spawn,
		Now:           opts.Now,
		Code:          taskCode("ocr"),
		Backoff:       newBackoff(opts.RetryBase, opts.RetryMax),
		Final:         func(_ workKey, err error) bool { return permanentFailure(err) },
		Run:           a.readThumbnails,
		OnFinish:      a.ocrFinished,
		OnRetry:       a.ocrRetrying,
	})
	catalogs.OnChange(a.catalogChanged)
	return a
}

// Wake asks the worker to analyse a channel, given by record id. It never blocks: any number of wake-ups
// before the worker looks count as one, and past WakeLimit channels they collapse into one pass over every
// subscribed channel. A channel need not be subscribed (the page of an unsubscribed channel wakes its
// analysis): a woken channel is analysed whoever follows it.
func (a *Analysis) Wake(channelRecordID string) {
	if channelRecordID == "" {
		a.WakeAll()
		return
	}
	a.marks.pend(channelRecordID)
	a.wakes.add(channelRecordID)
	a.signal.Wake()
}

// WakeAll asks the worker for a pass over every subscribed channel.
func (a *Analysis) WakeAll() {
	a.wakes.add("")
	a.signal.Wake()
}

// OCRSettingChanged tells the analysis that an administrator turned thumbnail recognition on or off. Turning
// it off drops the readings that were queued (and the results, which are not used while it is off); either
// way every channel is analysed again, since enabling it also rediscovers what was skipped.
func (a *Analysis) OCRSettingChanged(enabled bool) {
	a.ocrOn.Store(enabled)
	if !enabled {
		a.ocr.Forget(func(workKey) bool { return true })
	}
	a.WakeAll()
}

// ocrReady tells whether thumbnails are read: the build has a reader and the administrator allowed it.
func (a *Analysis) ocrReady() bool { return a.reader != nil && a.ocrOn.Load() }

// catalogChanged wakes the channel whose catalog loaded more or finished, and refills what a full queue
// left out.
func (a *Analysis) catalogChanged(event CatalogEvent) {
	if record, err := a.store.FindChannel(event.Key.Platform, event.Key.ChannelID); err == nil {
		a.Wake(record.Id)
	}
	if event.Done {
		a.refill(a.catalogs.Unfinished(), a.catalogs.opts.MaxUnfinished)
	}
}

// refill wakes the channels that found a queue full once it drained far enough, so that they are served in
// batches rather than one wake-up per finished job.
func (a *Analysis) refill(unfinished, limit int) {
	if unfinished > limit/4 {
		return
	}
	all, ids := a.deferred.take()
	if all {
		a.WakeAll()
		return
	}
	for _, id := range ids {
		a.Wake(id)
	}
}

// Run is the worker. It starts with a pass over every subscribed channel and then waits for wake-ups; it
// returns when ctx ends.
func (a *Analysis) Run(ctx context.Context) {
	a.WakeAll()
	for a.signal.Wait(ctx) {
		if !a.settle(ctx) {
			return
		}
		a.discover(ctx, a.take())
	}
}

// settle waits the debounce and swallows the wake-ups that came meanwhile; false when ctx ended.
func (a *Analysis) settle(ctx context.Context) bool {
	timer := time.NewTimer(a.opts.Debounce)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	select {
	case <-a.signal.C():
	default:
	}
	return true
}

// pass is the work one wake-up of the worker asks for.
type pass struct {
	all bool
	ids []string
}

func (a *Analysis) take() pass {
	all, ids := a.wakes.take()
	return pass{all: all, ids: ids}
}

// discover analyses the channels of a pass.
func (a *Analysis) discover(ctx context.Context, p pass) {
	channels, err := a.targets(p)
	if err != nil {
		if ctx.Err() == nil {
			logging.Of(a.app).Warn("Failed to discover the channels to analyse", "error", err)
		}
		return
	}
	inUse := map[string]bool{}
	for _, channel := range channels {
		if ctx.Err() != nil {
			return
		}
		platformID := channel.GetString("platform")
		used, known := inUse[platformID]
		if !known {
			// The analysis is work for everyone: it stops only when every account turned the platform off.
			used = a.sources.PlatformInUse(platformID)
			inUse[platformID] = used
		}
		a.analyseChannel(ctx, channel, used)
	}
}

// targets lists the channels of a pass whose platform lists catalogs: every subscribed channel for a full
// pass, else the channels that were woken.
func (a *Analysis) targets(p pass) ([]*core.Record, error) {
	var platformIDs []any
	for _, syncable := range a.platforms.WithCatalogSync() {
		platformIDs = append(platformIDs, syncable.ID())
	}
	if len(platformIDs) == 0 {
		return nil, nil
	}
	ids := p.ids
	var channels []*core.Record
	if p.all {
		query := a.app.RecordQuery("channels").AndWhere(dbx.In("platform", platformIDs...)).
			AndWhere(dbx.NewExp("EXISTS (SELECT 1 FROM subscriptions WHERE subscriptions.channel = channels.id)")).OrderBy("channels.id")
		if err := query.All(&channels); err != nil {
			return nil, err
		}
	}
	for chunk := range slices.Chunk(ids, statusBatch) {
		values := make([]any, len(chunk))
		for i, id := range chunk {
			values[i] = id
		}
		var found []*core.Record
		query := a.app.RecordQuery("channels").AndWhere(dbx.In("platform", platformIDs...)).AndWhere(dbx.In("channels.id", values...)).OrderBy("channels.id")
		if err := query.All(&found); err != nil {
			return nil, err
		}
		channels = append(channels, found...)
	}
	return distinctRecords(channels), nil
}

// distinctRecords drops the records already seen, keeping the order.
func distinctRecords(records []*core.Record) []*core.Record {
	seen := make(map[string]struct{}, len(records))
	kept := records[:0]
	for _, record := range records {
		if _, dup := seen[record.Id]; !dup {
			seen[record.Id] = struct{}{}
			kept = append(kept, record)
		}
	}
	return kept
}

// wakeSet is a bounded set of channel ids to analyse; "all" stands for every subscribed channel.
type wakeSet struct {
	mu    sync.Mutex
	limit int
	all   bool
	ids   map[string]struct{}
}

// add remembers a channel; the empty id, and an overflow, mean every channel.
func (w *wakeSet) add(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.all {
		return
	}
	if _, known := w.ids[id]; known {
		return
	}
	if id == "" || len(w.ids) >= w.limit {
		w.all, w.ids = true, nil
		return
	}
	if w.ids == nil {
		w.ids = map[string]struct{}{}
	}
	w.ids[id] = struct{}{}
}

func (w *wakeSet) has(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, known := w.ids[id]
	return known
}

// take empties the set and returns what it held.
func (w *wakeSet) take() (all bool, ids []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	all = w.all
	for id := range w.ids {
		ids = append(ids, id)
	}
	w.all, w.ids = false, nil
	return all, ids
}

// marks remembers, per channel, where the analysis stands in this process: waiting for the worker
// (Pending), being analysed (Running) or done (Complete). A channel with no mark was never analysed since
// the start.
type marks struct {
	mu    sync.Mutex
	state map[string]Status
}

func (m *marks) get(id string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state[id]
}

// pend marks a channel as waiting, unless the worker is on it.
func (m *marks) pend(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state[id] != Running {
		m.set(id, Pending)
	}
}

func (m *marks) put(id string, status Status) {
	m.mu.Lock()
	m.set(id, status)
	m.mu.Unlock()
}

// set records a mark; the lock is held. Past the limit the marks of idle channels are forgotten, which at
// worst analyses them once more.
func (m *marks) set(id string, status Status) {
	if _, known := m.state[id]; !known && len(m.state) >= markLimit {
		for other, mark := range m.state {
			if mark != Running {
				delete(m.state, other)
			}
			if len(m.state) < markLimit {
				break
			}
		}
	}
	m.state[id] = status
}
