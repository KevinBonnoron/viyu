package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

func outerWilds(channel string) []platform.Video {
	return dated(titled(channel, "Outer Wilds #1", "Outer Wilds #2", "Outer Wilds #3", "Another Game #1"))
}

func seriesTitles(list []series.Series) []string {
	titles := make([]string, len(list))
	for i, s := range list {
		titles[i] = fmt.Sprintf("%s:%d", s.Title, len(s.Items))
	}
	return titles
}

// The worker discovers a subscribed channel on its own, has its catalog synchronized and stores the series
// once the catalog completed. Stopping the group ends every goroutine.
func TestAnalysisStoresSeriesAfterTheCatalogCompletes(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.listing("chan", outerWilds("chan")...)
	channel := w.channel(t, alpha, "chan", "Channel chan", "")
	w.subscribe(t, channel)
	stored := make(chan string, 8)
	w.app.OnRecordAfterCreateSuccess("series").BindFunc(func(e *core.RecordEvent) error {
		stored <- e.Record.GetString("name")
		return e.Next()
	})
	w.group.Go(w.analysis.Run)
	if name := receive(t, stored, "the series to be stored"); name != "Outer Wilds" {
		t.Fatalf("series %q", name)
	}
	list := w.storedSeries(t, channel)
	if len(list) != 1 || list[0].Title != "Outer Wilds" || len(list[0].Items) != 3 {
		t.Fatalf("stored series: %v", seriesTitles(list))
	}
	if catalogs := w.catalogs.Unfinished(); catalogs != 0 {
		t.Fatalf("catalogs still synchronizing: %d", catalogs)
	}
	stopped := make(chan struct{})
	go func() { w.group.Stop(); close(stopped) }()
	receive(t, stopped, "shutdown to join the worker")
}

// A catalog that is complete and never analysed since the start reports pending, wakes its channel, and
// reads no series: nothing is inferred on a read.
func TestOverviewAnswersFromStorageAndNeverInfers(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	channel := w.synced(t, "chan", outerWilds("chan")...)
	w.subscribe(t, channel)
	var statements []string
	var mu sync.Mutex
	count := func(_ context.Context, _ time.Duration, statement string, _ *sql.Rows, _ error) {
		mu.Lock()
		statements = append(statements, statement)
		mu.Unlock()
	}
	w.watchQueries(count)

	overview, err := w.analysis.Overview([]*core.Record{channel})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Status != Pending || overview.Loaded != 4 || len(overview.Channels) != 1 {
		t.Fatalf("never analysed: %+v", overview)
	}
	got := overview.Channels[0]
	if got.ChannelID != channel.Id || got.ChannelName != "Channel chan" || got.Platform != alpha || got.Status != Pending || got.Loaded != 4 {
		t.Fatalf("channel status: %+v", got)
	}
	select {
	case <-w.analysis.signal.C():
	default:
		t.Fatal("the unanalysed channel was not woken")
	}
	if all, ids := w.analysis.wakes.take(); all || len(ids) != 1 || ids[0] != channel.Id {
		t.Fatalf("woken: %v %v", all, ids)
	}
	mu.Lock()
	for _, statement := range statements {
		if strings.Contains(statement, "`series`") || strings.Contains(statement, "`videos`") {
			t.Errorf("a read touched the series or the videos: %s", statement)
		}
	}
	mu.Unlock()
	if stored, err := w.app.CountRecords("series"); err != nil || stored != 0 {
		t.Fatalf("a read stored series: %d %v", stored, err)
	}

	w.analyse(channel)
	if list := w.storedSeries(t, channel); len(list) != 1 || len(list[0].Items) != 3 {
		t.Fatalf("worker: %v", seriesTitles(list))
	}
	overview, err = w.analysis.Overview([]*core.Record{channel})
	if err != nil || overview.Status != Complete || overview.Sync() != (Sync{Status: Complete, Loaded: 4}) {
		t.Fatalf("analysed: %+v %v", overview, err)
	}
}

// However many channels there are, a read costs the same few queries.
func TestOverviewQueryCountDoesNotGrowWithTheChannels(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	records := make([]*core.Record, 63)
	for i := range records {
		records[i] = w.channel(t, alpha, fmt.Sprintf("many%d", i), "Many", "")
		w.record(t, "channel_catalogs", map[string]any{"platform": alpha, "platform_id": records[i].GetString("platform_id"), "status": string(Complete), "completed_at": time.Now(), "channel": records[i].Id})
		w.analysis.marks.put(records[i].Id, Complete)
	}
	var queries atomic.Int32
	w.watchQueries(func(context.Context, time.Duration, string, *sql.Rows, error) { queries.Add(1) })
	measure := func(records []*core.Record) int32 {
		queries.Store(0)
		overview, err := w.analysis.Overview(records)
		if err != nil || overview.Status != Complete || len(overview.Channels) != len(records) {
			t.Fatalf("overview: %+v %v", overview, err)
		}
		return queries.Load()
	}
	few, many := measure(records[:3]), measure(records)
	if few > 8 || many != few {
		t.Fatalf("queries: %d for 3 channels, %d for %d", few, many, len(records))
	}
}

// Finding H2: a platform without catalog sync is not part of the analysis, and is never an error.
func TestPlatformWithoutCatalogSyncIsNeverAnalysedNorReportedAsError(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	twitchLike := w.channel(t, gamma, "streamer", "Streamer", "")
	w.subscribe(t, twitchLike)
	synced := w.synced(t, "chan", outerWilds("chan")...)
	w.subscribe(t, synced)
	w.analysis.marks.put(synced.Id, Complete)

	overview, err := w.analysis.Overview([]*core.Record{twitchLike, synced})
	if err != nil || len(overview.Channels) != 1 || overview.Channels[0].ChannelID != synced.Id || overview.Status == Failed {
		t.Fatalf("overview: %+v %v", overview, err)
	}
	status, err := w.analysis.ChannelStatus(twitchLike)
	if err != nil || status.Status != Complete || status.ChannelID != twitchLike.Id || status.Platform != gamma {
		t.Fatalf("channel status: %+v %v", status, err)
	}
	only, err := w.analysis.Overview([]*core.Record{twitchLike})
	if err != nil || only.Status != Complete || len(only.Channels) != 0 || only.Loaded != 0 {
		t.Fatalf("a platform that cannot list catalogs reported something: %+v %v", only, err)
	}
	targets, err := w.analysis.targets(pass{all: true})
	if err != nil || len(targets) != 1 || targets[0].Id != synced.Id {
		t.Fatalf("targets: %v %v", targets, err)
	}
	// Even asked for by id (a page view of the channel), it is not analysed.
	targets, err = w.analysis.targets(pass{ids: []string{twitchLike.Id}})
	if err != nil || len(targets) != 0 {
		t.Fatalf("a channel of a platform without catalogs was targeted: %v %v", targets, err)
	}
}

func TestStatusReportsAFailedCatalogAsPendingAndTurnsOffWithThePlatform(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.syncer.run = func(context.Context, int, platform.Channel, string, func(platform.Video) error) error {
		return errors.New("boom")
	}
	channel := w.channel(t, alpha, "chan", "Channel chan", "")
	if _, err := w.catalogs.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the catalog to fail once", func() bool { e, _ := w.catalogs.queue.Get(publicKey("chan")); return e.Failures == 1 })
	w.analysis.marks.put(channel.Id, Complete)
	// The failure is tried again later: until then the channel is pending, never an error.
	status, err := w.analysis.ChannelStatus(channel)
	if err != nil || status.Status != Pending || status.Episodes != Pending {
		t.Fatalf("a failed catalog: %+v %v", status, err)
	}
	// While the worker is on the channel it is running, whatever its catalog says.
	w.analysis.marks.put(channel.Id, Running)
	if status, _ := w.analysis.ChannelStatus(channel); status.Status != Running {
		t.Fatalf("worker running: %+v", status)
	}
	// Nobody uses the platform: nothing will run, so nothing is pending.
	user := w.user(t)
	if err := w.sources.SetEnabled(user.Id, alpha, false); err != nil {
		t.Fatal(err)
	}
	other := w.user(t)
	if err := w.sources.SetEnabled(other.Id, alpha, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := w.analysis.ChannelStatus(channel); status.Status != Complete {
		t.Fatalf("platform off: %+v", status)
	}
}

// A channel that is gone cannot be listed however often it is asked: it is not retried, and since nothing
// is awaited it is not reported either.
func TestStatusDoesNotReportAPermanentCatalogFailure(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.syncer.run = func(context.Context, int, platform.Channel, string, func(platform.Video) error) error {
		return apierr.NotFound("Channel")
	}
	channel := w.channel(t, alpha, "chan", "Channel chan", "")
	if _, err := w.catalogs.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	if entry := waitCatalog(t, w.catalogs, publicKey("chan")); entry.Status != Failed || entry.Code != "unavailable" {
		t.Fatalf("catalog: %+v", entry)
	}
	// The catalog's listener wakes (and so marks pending) the channel after the job is finished: let it
	// run before the test chooses the worker's state, or it would overwrite it.
	for w.analysis.marks.get(channel.Id) != Pending {
		runtime.Gosched()
	}
	w.analysis.marks.put(channel.Id, Complete)
	status, err := w.analysis.ChannelStatus(channel)
	if err != nil || status.Status != Complete || status.Episodes != Complete || status.Dates != Complete {
		t.Fatalf("a permanent failure: %+v %v", status, err)
	}
	w.analysis.discover(context.Background(), pass{ids: []string{channel.Id}})
	if runs := w.syncer.calls.Load(); runs != 1 {
		t.Fatalf("a permanent failure was retried: %d runs", runs)
	}
}

func TestWakeCoalescesAndIsBounded(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	a := w.analysis
	for range 1000 {
		a.Wake("same")
	}
	if len(a.wakes.ids) != 1 || a.wakes.all || len(a.signal.C()) != 1 {
		t.Fatalf("not coalesced: ids=%d all=%v signals=%d", len(a.wakes.ids), a.wakes.all, len(a.signal.C()))
	}
	if a.marks.get("same") != Pending {
		t.Fatalf("a woken channel is pending: %q", a.marks.get("same"))
	}
	for i := range 129 {
		a.Wake(fmt.Sprintf("channel%d", i))
	}
	if !a.wakes.all || len(a.wakes.ids) != 0 {
		t.Fatal("overflow did not collapse to one full pass")
	}
}

func TestDeferredChannelsAreWokenOnceAQueueDrains(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	a := w.analysis
	a.deferred.add("deferred")
	a.refill(30, 32) // still crowded
	if a.wakes.has("deferred") {
		t.Fatal("woken while the queue is full")
	}
	a.refill(8, 32)
	if !a.wakes.has("deferred") || a.deferred.has("deferred") || len(a.signal.C()) != 1 {
		t.Fatal("deferred channel was not rescheduled once")
	}
	for i := range 129 {
		a.deferred.add(fmt.Sprintf("channel%d", i))
	}
	a.refill(0, 32)
	if !a.wakes.all {
		t.Fatal("deferred overflow did not become a full pass")
	}
}

func TestOCRSwitchAndAvailability(t *testing.T) {
	t.Parallel()
	titles := []string{"Outer Wilds #1", "Outer Wilds #2", "Alone | Outer Wilds"}
	episodeOf := func(t *testing.T, w *analysisWorld, channel *core.Record) *float64 {
		t.Helper()
		list := w.storedSeries(t, channel)
		if len(list) != 1 {
			t.Fatalf("series: %v", seriesTitles(list))
		}
		for _, item := range list[0].Items {
			if item.Video.Title == "Alone | Outer Wilds" {
				return item.Episode
			}
		}
		t.Fatal("unnumbered video not in the series")
		return nil
	}

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{episode: func(context.Context, []string) (int, error) { return 3, nil }}
		w := newAnalysisWorld(t, reader, AnalysisOptions{})
		channel := w.synced(t, "chan", dated(titled("chan", titles...))...)
		if _, err := series.SetOCREnabled(w.app, false); err != nil {
			t.Fatal(err)
		}
		w.analysis.OCRSettingChanged(false)
		w.analyse(channel)
		if jobs := w.analysis.ocr.Counts(nil).Total; jobs != 0 || reader.calls.Load() != 0 {
			t.Fatalf("OCR ran while disabled: jobs=%d reads=%d", jobs, reader.calls.Load())
		}
		if episodeOf(t, w, channel) != nil {
			t.Fatal("an episode came from nowhere")
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		t.Parallel()
		w := newAnalysisWorld(t, nil, AnalysisOptions{})
		channel := w.synced(t, "chan", dated(titled("chan", titles...))...)
		w.analysis.OCRSettingChanged(true)
		w.analyse(channel)
		if jobs := w.analysis.ocr.Counts(nil).Total; jobs != 0 {
			t.Fatalf("OCR jobs without a reader: %d", jobs)
		}
		if episodeOf(t, w, channel) != nil {
			t.Fatal("an episode came from nowhere")
		}
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		var gated atomic.Bool
		reader := &fakeReader{episode: func(ctx context.Context, urls []string) (int, error) {
			if gated.Load() {
				select {
				case <-release:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}
			if len(urls) == 0 || !strings.Contains(urls[0], "chan-3") {
				t.Errorf("read the thumbnails of another video: %v", urls)
			}
			return 3, nil
		}}
		w := newAnalysisWorld(t, reader, AnalysisOptions{OCRConcurrency: 1})
		channel := w.synced(t, "chan", dated(titled("chan", titles...))...)
		w.analysis.OCRSettingChanged(true)
		w.quiet()
		w.analyse(channel)
		key := workKey{channel: channel.Id, platform: alpha, video: "chan-3"}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if entry, err := w.analysis.ocr.Wait(ctx, key); err != nil || entry.Status != Complete || entry.Out != 3 {
			t.Fatalf("reading: %+v %v", entry, err)
		}
		// The finished job woke the channel; the pass after it applies the number.
		receive(t, w.analysis.signal.C(), "the finished reading to wake its channel")
		if !w.analysis.wakes.has(channel.Id) {
			t.Fatal("a finished reading did not wake its channel")
		}
		if episode := episodeOf(t, w, channel); episode == nil || *episode != 3 {
			t.Fatalf("episode: %v", episode)
		}
		// Disabling drops what is queued or running, and the results: they are not used while it is off.
		gated.Store(true)
		w.analysis.ocr.Restart(key, titled("chan", titles...)[2])
		w.analysis.OCRSettingChanged(false)
		if jobs := w.analysis.ocr.Counts(nil).Total; jobs != 0 {
			t.Fatalf("jobs after disabling: %d", jobs)
		}
		if episodeOf(t, w, channel) != nil {
			t.Fatal("a result was used while recognition is off")
		}
		if all, _ := w.analysis.wakes.take(); !all {
			t.Fatal("disabling did not wake every channel")
		}
		// Enabling it again rediscovers what was skipped.
		gated.Store(false)
		w.analysis.OCRSettingChanged(true)
		w.analyse(channel)
		if entry, err := w.analysis.ocr.Wait(ctx, key); err != nil || entry.Status != Complete {
			t.Fatalf("second reading: %+v %v", entry, err)
		}
	})
}

// seriesNeedingDates is a numbering that repeats with no dates: only the videos' own pages tell the runs apart.
func seriesNeedingDates() []platform.Video {
	return titled("chan", "Balatro #1", "Balatro #2", "Balatro #1", "Balatro #2")
}

func metadataOf(channel string) func(context.Context, string) (platform.Video, error) {
	return func(_ context.Context, id string) (platform.Video, error) {
		var index int
		if _, err := fmt.Sscanf(id, "chan-%d", &index); err != nil {
			return platform.Video{}, err
		}
		title := seriesNeedingDates()[index-1].Title
		return platform.Video{ID: id, Platform: alpha, ChannelID: channel, ChannelName: "Channel " + channel, Title: title, OriginalTitle: title, OriginalTitleVerified: true, Published: "2024-02-01", FullMetadata: true}, nil
	}
}

func metadataJob(channel *core.Record, id string) workKey {
	return workKey{channel: channel.Id, platform: alpha, video: id}
}

func waitMetadata(t *testing.T, w *analysisWorld, key workKey) Entry[platform.Video, struct{}] {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := w.analysis.metadata.Wait(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestMetadataIsFetchedOnlyWhereSeriesNeedDates(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.list.fakePlatform.metadata = metadataOf("chan")
	numbered := w.synced(t, "numbered", dated(titled("numbered", "Outer Wilds #1", "Outer Wilds #2"))...)
	w.analyse(numbered)
	if total := w.analysis.metadata.Counts(nil).Total; total != 0 {
		t.Fatalf("a fully numbered series fetched %d pages", total)
	}
	channel := w.synced(t, "chan", seriesNeedingDates()...)
	w.quiet()
	w.analyse(channel)
	if total := w.analysis.metadata.Counts(nil).Total; total != 4 {
		t.Fatalf("pages to fetch: %d", total)
	}
	for _, id := range []string{"chan-1", "chan-2", "chan-3", "chan-4"} {
		if entry := waitMetadata(t, w, metadataJob(channel, id)); entry.Status != Complete {
			t.Fatalf("%s: %+v", id, entry)
		}
	}
	record, err := w.store.FindVideo(alpha, "chan-1")
	if err != nil || catalogPublished(record) != "2024-02-01" {
		t.Fatalf("the page was not stored: %v %v", record, err)
	}
	receive(t, w.analysis.signal.C(), "the finished fetches to wake the channel")
	if !w.analysis.wakes.has(channel.Id) {
		t.Fatal("the finished fetches did not wake the channel")
	}
	// What is stored is as good as a fetch: a new pass asks for nothing more.
	before := w.analysis.metadata.Counts(nil)
	w.analyse(channel)
	if after := w.analysis.metadata.Counts(nil); after != before {
		t.Fatalf("a settled video was fetched again: %+v -> %+v", before, after)
	}
}

func catalogPublished(r *core.Record) string {
	return platform.PublishedFromStored(r.GetDateTime("published").Time())
}

func TestMetadataTransientFailureIsRetriedAfterABackoffAndReportedPending(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{RetryBase: time.Minute, RetryMax: time.Hour})
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	w.list.fakePlatform.metadata = func(ctx context.Context, id string) (platform.Video, error) {
		calls.Add(1)
		if fail.Load() {
			return platform.Video{}, errors.New("provider detail: token=secret")
		}
		return metadataOf("chan")(ctx, id)
	}
	channel := w.synced(t, "chan", seriesNeedingDates()...)
	w.analyse(channel)
	key := metadataJob(channel, "chan-1")
	waitUntil(t, "the fetches to fail once", func() bool { return w.analysis.metadata.Counts(nil).Waiting == 4 })
	entry, _ := w.analysis.metadata.Get(key)
	if entry.Status != Pending || entry.Code != "metadata" || strings.Contains(entry.Code, "secret") || entry.Failures != 1 || !entry.RetryAt.After(w.clock.Now()) || entry.RetryAt.After(w.clock.Now().Add(time.Minute)) {
		t.Fatalf("failure: %+v", entry)
	}
	// Nobody is told of an error: the dates are pending until the retry.
	if counts := w.analysis.metadata.Counts(nil); counts != (Counts{Total: 4, Pending: 4, Waiting: 4}) {
		t.Fatalf("counts: %+v", counts)
	}
	if status, err := w.analysis.ChannelStatus(channel); err != nil || status.Dates != Pending || status.Status == Failed {
		t.Fatalf("status: %+v %v", status, err)
	}
	// Neither another pass nor the queue asks the platform again before the backoff ended.
	fetched := calls.Load()
	w.analyse(channel)
	poke(w.analysis.metadata)
	if calls.Load() != fetched {
		t.Fatalf("a failed fetch was retried before its backoff ended: %d -> %d calls", fetched, calls.Load())
	}
	// Afterwards the queue runs them again on its own.
	fail.Store(false)
	w.clock.advance(2 * time.Minute)
	poke(w.analysis.metadata)
	waitAllMetadata(t, w, channel)
	if entry, _ := w.analysis.metadata.Get(key); entry.Status != Complete || entry.Failures != 1 {
		t.Fatalf("retry: %+v", entry)
	}
	if status, err := w.analysis.ChannelStatus(channel); err != nil || status.Dates != Complete {
		t.Fatalf("status after the retry: %+v %v", status, err)
	}
}

func TestMetadataPermanentFailureIsNotRetriedNorReported(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{RetryBase: time.Minute})
	var calls atomic.Int32
	w.list.fakePlatform.metadata = func(context.Context, string) (platform.Video, error) {
		calls.Add(1)
		return platform.Video{}, apierr.RequestFailed().WithCause(&ytdlp.Error{Stderr: "ERROR: [youtube] chan-1: Video unavailable. This video has been removed by the uploader"})
	}
	channel := w.synced(t, "chan", seriesNeedingDates()...)
	w.analyse(channel)
	waitAllMetadata(t, w, channel)
	key := metadataJob(channel, "chan-1")
	if entry, _ := w.analysis.metadata.Get(key); entry.Status != Failed || entry.Code != "unavailable" || !entry.RetryAt.IsZero() {
		t.Fatalf("failure: %+v", entry)
	}
	fetched := calls.Load()
	w.clock.advance(48 * time.Hour)
	poke(w.analysis.metadata)
	w.analyse(channel)
	if calls.Load() != fetched {
		t.Fatalf("a permanent failure was retried: %d -> %d calls", fetched, calls.Load())
	}
	if counts := w.analysis.metadata.Counts(nil); counts != (Counts{Total: 4, Error: 4}) {
		t.Fatalf("counts: %+v", counts)
	}
	// Nothing waits for it and nothing is reported: the series show what they have.
	if status, err := w.analysis.ChannelStatus(channel); err != nil || status.Dates != Complete || status.Status == Failed {
		t.Fatalf("status: %+v %v", status, err)
	}
}

// What comes back for another channel's video is not what the catalog lists: asking again would answer the
// same, so the fetch fails for good and nothing of it is stored.
func TestMetadataOfAnotherOwnerIsRejectedForGood(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{RetryBase: time.Minute})
	var calls atomic.Int32
	w.list.fakePlatform.metadata = func(ctx context.Context, id string) (platform.Video, error) {
		calls.Add(1)
		return metadataOf("somebody-else")(ctx, id)
	}
	channel := w.synced(t, "chan", seriesNeedingDates()...)
	w.analyse(channel)
	waitAllMetadata(t, w, channel)
	key := metadataJob(channel, "chan-1")
	if entry, _ := w.analysis.metadata.Get(key); entry.Status != Failed || entry.Code != "metadata" || !entry.RetryAt.IsZero() {
		t.Fatalf("a page of another owner was accepted: %+v", entry)
	}
	if record, err := w.store.FindVideo(alpha, "chan-1"); err != nil || !record.GetDateTime("published").Time().IsZero() {
		t.Fatalf("a page of another owner was stored: %v %v", record, err)
	}
	fetched := calls.Load()
	w.clock.advance(48 * time.Hour)
	poke(w.analysis.metadata)
	if calls.Load() != fetched {
		t.Fatalf("a rejected page was fetched again: %d -> %d", fetched, calls.Load())
	}
}

func TestMetadataRateLimitPausesTheQueueAndRetriesBoundedly(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{MetadataConcurrency: 1, RateLimitPause: 5 * time.Millisecond, MetadataAttempts: 3})
	var limited atomic.Int32
	w.list.fakePlatform.metadata = func(ctx context.Context, id string) (platform.Video, error) {
		if id == "chan-1" && limited.Add(1) <= 2 {
			return platform.Video{}, &ytdlp.Error{Kind: ytdlp.KindRateLimited}
		}
		return metadataOf("chan")(ctx, id)
	}
	channel := w.synced(t, "chan", seriesNeedingDates()...)
	w.analyse(channel)
	entry := waitMetadata(t, w, metadataJob(channel, "chan-1"))
	if entry.Status != Complete || entry.Attempts != 3 {
		t.Fatalf("rate limited fetch: %+v", entry)
	}

	// Always limited: bounded attempts, then the fetch sleeps instead of giving up.
	exhausted := newAnalysisWorld(t, nil, AnalysisOptions{RateLimitPause: time.Millisecond, MetadataAttempts: 3})
	exhausted.list.fakePlatform.metadata = func(context.Context, string) (platform.Video, error) {
		return platform.Video{}, fmt.Errorf("lookup: %w", &ytdlp.Error{Kind: ytdlp.KindRateLimited, Stderr: "HTTP Error 429 token=secret"})
	}
	other := exhausted.synced(t, "chan", seriesNeedingDates()...)
	exhausted.analyse(other)
	sleeping := metadataJob(other, "chan-1")
	waitUntil(t, "the exhausted fetch to sleep", func() bool { e, _ := exhausted.analysis.metadata.Get(sleeping); return e.Failures == 1 })
	if entry, _ := exhausted.analysis.metadata.Get(sleeping); entry.Status != Pending || entry.Code != "rate_limited" || entry.Attempts != 3 || entry.RetryAt.IsZero() {
		t.Fatalf("exhausted: %+v", entry)
	}
}

func TestMetadataQueueIsBoundedAndRefilled(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	w := newAnalysisWorld(t, nil, AnalysisOptions{MetadataConcurrency: 1, MetadataQueueLimit: 2})
	w.list.fakePlatform.metadata = func(ctx context.Context, id string) (platform.Video, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return platform.Video{}, ctx.Err()
		}
		return metadataOf("chan")(ctx, id)
	}
	channel := w.synced(t, "chan", seriesNeedingDates()...)
	w.analyse(channel)
	if counts := w.analysis.metadata.Counts(nil); counts.Total != 2 {
		t.Fatalf("the queue admitted %d jobs beyond its bound of 2", counts.Total)
	}
	if !w.analysis.deferred.has(channel.Id) {
		t.Fatal("the channel that found the queue full was not remembered")
	}
	overview, err := w.analysis.Overview([]*core.Record{channel})
	if err != nil || overview.Metadata.Total != 2 || overview.Status != Pending && overview.Status != Running {
		t.Fatalf("overview: %+v %v", overview, err)
	}
	close(release)
	waitAllMetadata(t, w, channel)
	// The queue has room again: another pass asks for the rest.
	w.analyse(channel)
	if counts := w.analysis.metadata.Counts(nil); counts.Total != 4 {
		t.Fatalf("the deferred videos were not queued: %+v", counts)
	}
	waitAllMetadata(t, w, channel)
}

// waitAllMetadata waits for every metadata job of the channel that exists.
func waitAllMetadata(t *testing.T, w *analysisWorld, channel *core.Record) {
	t.Helper()
	for _, id := range []string{"chan-1", "chan-2", "chan-3", "chan-4"} {
		if _, ok := w.analysis.metadata.Get(metadataJob(channel, id)); ok {
			waitMetadata(t, w, metadataJob(channel, id))
		}
	}
}

func TestOCRQueueIsBounded(t *testing.T) {
	t.Parallel()
	reader := &fakeReader{episode: func(ctx context.Context, _ []string) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}}
	w := newAnalysisWorld(t, reader, AnalysisOptions{OCRConcurrency: 1, OCRQueueLimit: 2})
	channel := w.synced(t, "chan", dated(titled("chan", "Outer Wilds #1", "Outer Wilds #2", "A | Outer Wilds", "B | Outer Wilds", "C | Outer Wilds"))...)
	w.analysis.OCRSettingChanged(true)
	w.analyse(channel)
	if counts := w.analysis.ocr.Counts(nil); counts.Total != 2 {
		t.Fatalf("OCR queue: %+v", counts)
	}
	if !w.analysis.deferred.has(channel.Id) {
		t.Fatal("a channel that found the OCR queue full was not remembered")
	}
	overview, err := w.analysis.Overview([]*core.Record{channel})
	if err != nil || overview.OCR.Total != 2 || overview.Status != Pending && overview.Status != Running {
		t.Fatalf("overview: %+v %v", overview, err)
	}
}

// Whatever is running or waiting when the group stops is cancelled and joined.
func TestShutdownJoinsEveryGoroutine(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 8)
	reader := &fakeReader{episode: func(ctx context.Context, _ []string) (int, error) {
		started <- struct{}{}
		<-ctx.Done()
		return 0, ctx.Err()
	}}
	w := newAnalysisWorld(t, reader, AnalysisOptions{})
	w.list.fakePlatform.metadata = func(ctx context.Context, _ string) (platform.Video, error) {
		started <- struct{}{}
		<-ctx.Done()
		return platform.Video{}, ctx.Err()
	}
	channel := w.synced(t, "chan", append(seriesNeedingDates(), titled("chan", "Balatro | Unnumbered")...)...)
	w.subscribe(t, channel)
	w.analysis.OCRSettingChanged(true)
	w.group.Go(w.analysis.Run)
	receive(t, started, "a background job to start")
	receive(t, started, "a second background job to start")
	stopped := make(chan struct{})
	go func() { w.group.Stop(); close(stopped) }()
	receive(t, stopped, "the group to join every goroutine")
	if running := w.analysis.metadata.Counts(nil).Running + w.analysis.ocr.Counts(nil).Running; running != 0 {
		t.Fatalf("%d jobs still running after shutdown", running)
	}
	if w.group.Go(func(context.Context) { t.Error("work started after shutdown") }) {
		t.Fatal("a stopped group accepted work")
	}
}

func (w *analysisWorld) watchQueries(log func(ctx context.Context, d time.Duration, statement string, rows *sql.Rows, err error)) {
	for _, db := range []dbx.Builder{w.app.ConcurrentDB(), w.app.NonconcurrentDB()} {
		db.(*dbx.DB).QueryLogFunc = log
	}
}

// quiet forgets the wake-ups that happened so far, so a test can tell what a job wakes.
func (w *analysisWorld) quiet() {
	w.analysis.take()
	select {
	case <-w.analysis.signal.C():
	default:
	}
}

// The page of an unsubscribed channel wakes its analysis, which never reaches it on its own.
func TestWokenChannelIsAnalysedWhetherOrNotItIsSubscribed(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	channel := w.synced(t, "chan", outerWilds("chan")...)
	if targets, err := w.analysis.targets(pass{all: true}); err != nil || len(targets) != 0 {
		t.Fatalf("a full pass reached an unsubscribed channel: %v %v", targets, err)
	}
	w.quiet()
	w.analysis.Wake(channel.Id)
	w.analysis.discover(context.Background(), w.analysis.take())
	if list := w.storedSeries(t, channel); len(list) != 1 || len(list[0].Items) != 3 {
		t.Fatalf("series: %v", seriesTitles(list))
	}
	if mark := w.analysis.marks.get(channel.Id); mark != Complete {
		t.Fatalf("mark after the pass: %q", mark)
	}
}

// A catalog that failed is tried again by the queue after its backoff, whatever pass comes by meanwhile, and
// the channel is analysed once it completes.
func TestFailedCatalogIsRetriedByTheQueueAndTheChannelAnalysedAfterwards(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	var fail atomic.Bool
	fail.Store(true)
	w.syncer.run = func(_ context.Context, _ int, _ platform.Channel, _ string, visit func(platform.Video) error) error {
		if fail.Load() {
			return errors.New("boom")
		}
		for _, v := range outerWilds("chan") {
			if err := visit(v); err != nil {
				return err
			}
		}
		return nil
	}
	channel := w.channel(t, alpha, "chan", "Channel chan", "")
	w.subscribe(t, channel)
	w.analysis.discover(context.Background(), pass{all: true})
	key := publicKey("chan")
	waitUntil(t, "the catalog to fail once", func() bool { e, _ := w.catalogs.queue.Get(key); return e.Failures == 1 })
	if status, err := w.analysis.ChannelStatus(channel); err != nil || status.Status != Pending || status.Episodes != Pending {
		t.Fatalf("a failed catalog is not an error, it is pending: %+v %v", status, err)
	}
	// No pass starts it again before its backoff ended.
	w.analysis.discover(context.Background(), pass{all: true})
	if runs := w.syncer.calls.Load(); runs != 1 {
		t.Fatalf("a failed catalog was retried before its backoff: %d runs", runs)
	}
	fail.Store(false)
	w.clock.advance(6 * time.Hour)
	poke(w.catalogs.queue)
	if entry := waitCatalog(t, w.catalogs, key); entry.Status != Complete || entry.Failures != 1 {
		t.Fatalf("retried catalog: %+v", entry)
	}
	receive(t, w.analysis.signal.C(), "the completed catalog to wake the channel")
	w.analysis.discover(context.Background(), w.analysis.take())
	if list := w.storedSeries(t, channel); len(list) != 1 {
		t.Fatalf("series: %v", seriesTitles(list))
	}
}
