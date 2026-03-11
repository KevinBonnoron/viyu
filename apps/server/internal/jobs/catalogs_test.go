package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

func catalogStatus(t *testing.T, c *Catalogs, channel string) Sync {
	t.Helper()
	stored, err := c.Stored(alpha, channel)
	if err != nil {
		t.Fatal(err)
	}
	return stored.Sync
}

func storedStatusRow(t *testing.T, w *syncWorld, channel string) Status {
	t.Helper()
	row, err := w.app.FindFirstRecordByFilter("channel_catalogs", "platform = {:p} && platform_id = {:id}", dbx.Params{"p": alpha, "id": channel})
	if err != nil {
		t.Fatal(err)
	}
	return Status(row.GetString("status"))
}

// A public catalog is written in batches (one transaction each), shows what is loaded while it loads, and
// swaps generations only when the new listing is whole.
func TestPublicCatalogIsPersistedInBatchesAndSwapsGenerations(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{})
	var transactions []core.App
	w.app.OnRecordCreate("channel_catalog_entries").BindFunc(func(e *core.RecordEvent) error {
		if !slices.Contains(transactions, e.App) {
			transactions = append(transactions, e.App)
		}
		return e.Next()
	})
	reached, release := [3]chan struct{}{nil, make(chan struct{}), make(chan struct{})}, [3]chan struct{}{nil, make(chan struct{}), make(chan struct{})}
	w.syncer.run = func(ctx context.Context, call int, _ platform.Channel, cookieFile string, visit func(platform.Video) error) error {
		if cookieFile != "" {
			t.Errorf("a public catalog was listed with a cookie jar: %q", cookieFile)
		}
		list := fixtureVideos("chan", "chan", 250)
		if call == 2 {
			list = list[:200]
		}
		for i, v := range list {
			if err := visit(v); err != nil {
				return err
			}
			if i == 129 {
				close(reached[call])
				select {
				case <-release[call]:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return nil
	}
	first, err := c.Snapshot(publicKey("@chan"), SnapshotOptions{})
	if err != nil || !unfinished(first.Sync.Status) {
		t.Fatalf("first read: %+v %v", first, err)
	}
	<-reached[1]
	loading := catalogStatus(t, c, "chan")
	if loading.Status != Running || loading.Loaded != 130 || storedStatusRow(t, w, "chan") != Running {
		t.Fatalf("unfinished stream: %+v stored=%s", loading, storedStatusRow(t, w, "chan"))
	}
	// Polling never starts another extraction.
	for range 3 {
		if _, err := c.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	close(release[1])
	if entry := waitCatalog(t, c, publicKey("chan")); entry.Status != Complete {
		t.Fatalf("job: %+v", entry)
	}
	done := catalogStatus(t, c, "chan")
	if done.Status != Complete || done.Loaded != 250 || storedStatusRow(t, w, "chan") != Complete || w.syncer.calls.Load() != 1 {
		t.Fatalf("complete: %+v stored=%s runs=%d", done, storedStatusRow(t, w, "chan"), w.syncer.calls.Load())
	}
	// Initial page of 30, then the stream in batches of 100 (a first page is not held back for a full batch).
	if len(transactions) != 4 {
		t.Fatalf("entries written in %d transactions", len(transactions))
	}
	snapshot, err := c.Snapshot(publicKey("chan"), SnapshotOptions{})
	if err != nil || snapshot.Channel == nil || snapshot.Channel.Name != "Channel chan" || len(snapshot.Videos) != 250 {
		t.Fatalf("snapshot: %+v %v", snapshot.Sync, err)
	}
	for _, v := range snapshot.Videos {
		if v.RecordID == "" || v.ChannelRecordID != snapshot.Channel.RecordID || v.Source != "" {
			t.Fatalf("video not stored as a public video of the channel: %+v", v)
		}
	}

	// A catalog that grew old is pending and synchronized again; the old generation stays until the new
	// listing is whole, so the catalog never shrinks while it loads.
	w.clock.advance(25 * time.Hour)
	stale := catalogStatus(t, c, "chan")
	if stale.Status != Pending || stale.Loaded != 250 {
		t.Fatalf("stale catalog: %+v", stale)
	}
	reload, err := c.Snapshot(publicKey("chan"), SnapshotOptions{})
	if err != nil || !unfinished(reload.Sync.Status) || reload.Sync.Loaded != 250 {
		t.Fatalf("stale snapshot: %+v %v", reload.Sync, err)
	}
	<-reached[2]
	midway := catalogStatus(t, c, "chan")
	if midway.Status != Running || midway.Loaded != 250 {
		t.Fatalf("the catalog shrank while its new generation was loading: %+v", midway)
	}
	close(release[2])
	waitCatalog(t, c, publicKey("chan"))
	shrunk := catalogStatus(t, c, "chan")
	if shrunk.Status != Complete || shrunk.Loaded != 200 || w.syncer.calls.Load() != 2 {
		t.Fatalf("after the second listing: %+v runs=%d", shrunk, w.syncer.calls.Load())
	}
	if videos, err := w.app.CountRecords("videos"); err != nil || videos != 250 {
		t.Fatalf("dropping entries must not delete videos: %d %v", videos, err)
	}
}

// A catalog that fails is not an error the user has to act on: the queue sleeps and tries again after a
// backoff, the partial catalog stays, and in the meantime it is pending.
func TestCatalogFailureIsRetriedAfterABackoffAndStaysPending(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{RetryBase: time.Minute, RetryMax: time.Hour})
	var fail atomic.Bool
	fail.Store(true)
	w.syncer.run = func(_ context.Context, _ int, _ platform.Channel, _ string, visit func(platform.Video) error) error {
		for _, v := range fixtureVideos("chan", "chan", 40)[30:35] {
			if err := visit(v); err != nil {
				return err
			}
		}
		if fail.Load() {
			return errors.New("fixture downloader detail: secret=1")
		}
		return nil
	}
	snapshot, err := c.Snapshot(publicKey("chan"), SnapshotOptions{})
	if err != nil || !unfinished(snapshot.Sync.Status) {
		t.Fatalf("%+v %v", snapshot.Sync, err)
	}
	key := publicKey("chan")
	waitUntil(t, "the catalog to fail once", func() bool { e, _ := c.queue.Get(key); return e.Failures == 1 })
	entry, _ := c.queue.Get(key)
	if entry.Status != Pending || entry.Code != "catalog" || strings.Contains(entry.Code, "secret") || !entry.RetryAt.After(w.clock.Now()) || entry.RetryAt.After(w.clock.Now().Add(time.Minute)) {
		t.Fatalf("job: %+v", entry)
	}
	// The listing's first page (30) and the five streamed videos are kept: partial results do not vanish.
	// The failure is not reported: the catalog is pending until its retry.
	if pending := catalogStatus(t, c, "chan"); pending.Status != Pending || pending.Loaded != 35 || storedStatusRow(t, w, "chan") != Failed {
		t.Fatalf("failed: %+v stored=%s", pending, storedStatusRow(t, w, "chan"))
	}
	for range 3 {
		if again, _ := c.Snapshot(key, SnapshotOptions{}); again.Sync.Status != Pending {
			t.Fatalf("polling reported a failure: %+v", again.Sync)
		}
	}
	poke(c.queue)
	if runs := w.syncer.calls.Load(); runs != 1 {
		t.Fatalf("a catalog was retried before its backoff ended: %d runs", runs)
	}
	// Once the backoff ended the queue runs it again, with no read asking for it.
	fail.Store(false)
	w.clock.advance(2 * time.Minute)
	poke(c.queue)
	if done := waitCatalog(t, c, key); done.Status != Complete || done.Failures != 1 {
		t.Fatalf("after the retry: %+v", done)
	}
	if done := catalogStatus(t, c, "chan"); done.Status != Complete || done.Loaded != 35 || w.syncer.calls.Load() != 2 {
		t.Fatalf("after the retry: %+v runs=%d", done, w.syncer.calls.Load())
	}
	// A complete catalog is served from storage by a restarted process, whose queue knows nothing of it.
	again := w.catalogs(CatalogOptions{})
	if got, err := again.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil || got.Sync.Status != Complete || w.syncer.calls.Load() != 2 {
		t.Fatalf("complete catalog was synchronized again: %+v %v", got.Sync, err)
	}
}

// The error a restart finds in storage says nothing of whether it can pass: the new process tries again.
func TestStoredCatalogFailureIsPendingAndTriedAgainAfterARestart(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{RetryBase: time.Hour})
	var fail atomic.Bool
	fail.Store(true)
	w.syncer.run = func(context.Context, int, platform.Channel, string, func(platform.Video) error) error {
		if fail.Load() {
			return errors.New("fixture downloader detail")
		}
		return nil
	}
	if _, err := c.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the catalog to fail once", func() bool { e, _ := c.queue.Get(publicKey("chan")); return e.Failures == 1 })
	if storedStatusRow(t, w, "chan") != Failed {
		t.Fatalf("stored: %s", storedStatusRow(t, w, "chan"))
	}
	fail.Store(false)
	restarted := w.catalogs(CatalogOptions{})
	if sync := catalogStatus(t, restarted, "chan"); sync.Status != Pending {
		t.Fatalf("a stored failure was reported: %+v", sync)
	}
	got, err := restarted.Snapshot(publicKey("chan"), SnapshotOptions{})
	if err != nil || !unfinished(got.Sync.Status) {
		t.Fatalf("restart: %+v %v", got.Sync, err)
	}
	if entry := waitCatalog(t, restarted, publicKey("chan")); entry.Status != Complete {
		t.Fatalf("after the restart: %+v", entry)
	}
}

// A catalog that cannot be listed because the channel is gone is not retried, and is not an error either.
func TestPermanentCatalogFailureIsNotRetriedNorReported(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{RetryBase: time.Minute})
	w.syncer.run = func(context.Context, int, platform.Channel, string, func(platform.Video) error) error {
		return fmt.Errorf("list: %w", apierr.RequestFailed().WithCause(&ytdlp.Error{Stderr: "ERROR: [youtube:tab] This channel does not exist."}))
	}
	key := publicKey("chan")
	if _, err := c.Snapshot(key, SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	entry := waitCatalog(t, c, key)
	if entry.Status != Failed || entry.Code != "unavailable" || !entry.RetryAt.IsZero() {
		t.Fatalf("job: %+v", entry)
	}
	if sync := catalogStatus(t, c, "chan"); sync.Status != Complete {
		t.Fatalf("a permanent failure was reported: %+v", sync)
	}
	if status, deferred, err := c.Ensure(key); err != nil || deferred || status != Failed {
		t.Fatalf("ensure: %v %v %v", status, deferred, err)
	}
	w.clock.advance(48 * time.Hour)
	poke(c.queue)
	for range 3 {
		if again, err := c.Snapshot(key, SnapshotOptions{}); err != nil || again.Sync.Status != Complete {
			t.Fatalf("polling: %+v %v", again.Sync, err)
		}
	}
	if runs := w.syncer.calls.Load(); runs != 1 {
		t.Fatalf("a permanent failure was retried: %d runs", runs)
	}
	// Only an explicit request starts it again.
	if _, err := c.Snapshot(key, SnapshotOptions{Retry: true}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the explicit retry", func() bool { return w.syncer.calls.Load() == 2 })
}

func TestCatalogChannelIDsAreCanonical(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{})
	for _, id := range []string{"@chan", "chan", "@chan"} {
		if _, err := c.Snapshot(publicKey(id), SnapshotOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	waitCatalog(t, c, publicKey("chan"))
	if rows, err := w.app.CountRecords("channel_catalogs"); err != nil || rows != 1 {
		t.Fatalf("one channel, %d catalogs (%v)", rows, err)
	}
	if runs := w.syncer.calls.Load(); runs != 1 {
		t.Fatalf("one channel synchronized %d times", runs)
	}
}

func TestRateLimitedCatalogIsRetriedAfterThePause(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{RateLimitPause: 5 * time.Millisecond})
	w.syncer.run = func(_ context.Context, call int, _ platform.Channel, _ string, _ func(platform.Video) error) error {
		if call < 3 {
			return &ytdlp.Error{Kind: ytdlp.KindRateLimited}
		}
		return nil
	}
	if _, err := c.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	entry := waitCatalog(t, c, publicKey("chan"))
	if entry.Status != Complete || entry.Attempts != 3 {
		t.Fatalf("job: %+v", entry)
	}
	w.syncer.calls.Store(0)
	w.clock.advance(48 * time.Hour)
	w.syncer.run = func(context.Context, int, platform.Channel, string, func(platform.Video) error) error {
		return &ytdlp.Error{Kind: ytdlp.KindRateLimited}
	}
	if _, err := c.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	// Out of attempts, a catalog that is still rate limited is not given up: it sleeps and is tried again.
	waitUntil(t, "the exhausted catalog to sleep", func() bool { e, _ := c.queue.Get(publicKey("chan")); return e.Failures == 1 })
	if entry, _ := c.queue.Get(publicKey("chan")); entry.Status != Pending || entry.Code != "rate_limited" || entry.Attempts != 3 || entry.RetryAt.IsZero() {
		t.Fatalf("exhausted: %+v", entry)
	}
}

// Finding H2: a platform that cannot list a catalog is not part of the synchronization, and never reports an
// error because of it.
func TestPlatformWithoutCatalogSyncIsRefusedNotFailed(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{})
	_, err := c.Snapshot(CatalogKey{Platform: gamma, ChannelID: "streamer"}, SnapshotOptions{})
	var invalid *apierr.Error
	if !errors.As(err, &invalid) || invalid.Code != apierr.CodeInvalidSourceArg {
		t.Fatalf("a platform without catalog sync was accepted: %v", err)
	}
	if _, err := c.Snapshot(CatalogKey{Platform: "unknown", ChannelID: "x"}, SnapshotOptions{}); err == nil {
		t.Fatal("an unknown platform was accepted")
	}
	channel := w.channel(t, gamma, "streamer", "Streamer", "")
	states, err := c.states([]*core.Record{channel})
	if err != nil || len(states) != 0 {
		t.Fatalf("a platform without catalog sync has a catalog state: %v %v", states, err)
	}
}

func TestCatalogQueueIsBoundedAndShutdownJoinsPendingWork(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{Concurrency: 1, MaxUnfinished: 3})
	started := make(chan struct{}, 8)
	w.syncer.run = func(ctx context.Context, _ int, _ platform.Channel, _ string, _ func(platform.Video) error) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	for _, id := range []string{"one", "two", "three", "four", "five"} {
		snapshot, err := c.Snapshot(publicKey(id), SnapshotOptions{})
		if err != nil {
			t.Fatal(err)
		}
		refused := id == "four" || id == "five"
		if refused != (snapshot.Sync.Status == Failed) {
			t.Fatalf("%s: unbounded pending work accepted or refused too early: %+v", id, snapshot.Sync)
		}
	}
	// The same refusal, seen by the background analysis, is a deferral and not a failure.
	status, deferred, err := c.Ensure(publicKey("six"))
	if err != nil || !deferred || status != Pending {
		t.Fatalf("ensure on a full queue: %v %v %v", status, deferred, err)
	}
	receive(t, started, "the first synchronization")
	if running := c.queue.Counts(nil).Running; running != 1 {
		t.Fatalf("%d synchronizations ran at once with a concurrency of one", running)
	}
	if c.Unfinished() != 3 {
		t.Fatalf("unfinished bound: %d", c.Unfinished())
	}
	stopped := make(chan struct{})
	go func() { w.group.Stop(); close(stopped) }()
	receive(t, stopped, "shutdown to join the running and queued jobs")
	if complete, err := w.app.CountRecords("channel_catalogs", dbx.HashExp{"status": string(Complete)}); err != nil || complete != 0 {
		t.Fatalf("a cancelled catalog was marked complete: %d %v", complete, err)
	}
}

func connect(t *testing.T, w *syncWorld, userID, cookies string) {
	t.Helper()
	if _, err := w.sources.Connect(context.Background(), userID, alpha, cookies, ""); err != nil {
		t.Fatal(err)
	}
}

// secretOf reads what a connected listing runs as: the cookies of its own jar.
func secretOf(t *testing.T, cookieFile string) string {
	t.Helper()
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		t.Errorf("connected listing without its own jar: %q %v", cookieFile, err)
	}
	return strings.TrimSpace(string(data))
}

func connectedVideos(t *testing.T, c *Catalogs, key CatalogKey) Catalog {
	t.Helper()
	snapshot, err := c.Snapshot(key, SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestConnectedCatalogsAreIsolatedAndMemoryOnly(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{})
	w.withoutFirstPage()
	w.syncer.run = func(_ context.Context, _ int, channel platform.Channel, cookieFile string, visit func(platform.Video) error) error {
		prefix := "pub"
		if cookieFile != "" {
			prefix = secretOf(t, cookieFile)
		}
		for _, v := range fixtureVideos(channel.ID, prefix, 3) {
			if err := visit(v); err != nil {
				return err
			}
		}
		return nil
	}
	a, b := w.user(t), w.user(t)
	connect(t, w, a.Id, "first")
	connect(t, w, b.Id, "other")
	keyA, err := c.ConnectedKey(a.Id, alpha, "@chan")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := c.ConnectedKey(b.Id, alpha, "chan")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []CatalogKey{keyA, keyB} {
		if snapshot := connectedVideos(t, c, key); !unfinished(snapshot.Sync.Status) {
			t.Fatalf("first read: %+v", snapshot.Sync)
		}
	}
	waitCatalog(t, c, mustResolve(t, c, keyA))
	waitCatalog(t, c, mustResolve(t, c, keyB))
	for key, prefix := range map[CatalogKey]string{keyA: "first", keyB: "other"} {
		snapshot := connectedVideos(t, c, key)
		if snapshot.Sync.Status != Complete || len(snapshot.Videos) != 3 || snapshot.Channel == nil || snapshot.Channel.RecordID == "" {
			t.Fatalf("connected snapshot: %+v", snapshot.Sync)
		}
		for _, v := range snapshot.Videos {
			if !strings.HasPrefix(v.ID, prefix+"-") || v.Source != alpha || v.RecordID == "" || v.ChannelRecordID == "" {
				t.Fatalf("one account saw another's video, or one that is not addressable: %+v", v)
			}
		}
	}
	// Connected results are stored like any video, owned by the account, and never become a public catalog.
	owned, err := w.store.FindVideo(alpha, "first-1")
	if err != nil || !slices.Equal(owned.GetStringSlice("owners"), []string{a.Id}) {
		t.Fatalf("connected video: %v %v", owned, err)
	}
	if count, err := w.app.CountRecords("channel_catalogs"); err != nil || count != 0 {
		t.Fatalf("a connected catalog persisted as public: %d %v", count, err)
	}
	// The public catalog of the same channel holds none of what a session listed.
	if _, err := c.Snapshot(publicKey("chan"), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	waitCatalog(t, c, publicKey("chan"))
	public, err := c.Stored(alpha, "chan")
	if err != nil || len(public.Videos) != 3 {
		t.Fatalf("public catalog: %+v %v", public.Sync, err)
	}
	for _, v := range public.Videos {
		if !strings.HasPrefix(v.ID, "pub-") || v.Source != "" {
			t.Fatalf("public catalog inherited connected results: %+v", v)
		}
	}
}

func mustResolve(t *testing.T, c *Catalogs, key CatalogKey) CatalogKey {
	t.Helper()
	resolved, _, err := c.resolve(key)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestConnectedCatalogIsBoundToTheConnectionRevision(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{})
	w.withoutFirstPage()
	w.syncer.run = func(_ context.Context, _ int, channel platform.Channel, cookieFile string, visit func(platform.Video) error) error {
		for _, v := range fixtureVideos(channel.ID, secretOf(t, cookieFile), 3) {
			if err := visit(v); err != nil {
				return err
			}
		}
		return nil
	}
	user := w.user(t)
	connect(t, w, user.Id, "first")
	old, err := c.ConnectedKey(user.Id, alpha, "chan")
	if err != nil {
		t.Fatal(err)
	}
	connectedVideos(t, c, old)
	waitCatalog(t, c, old)
	if err := c.ScopeCurrent(old); err != nil {
		t.Fatal(err)
	}
	// Reconnecting always makes a new connection: what the previous session listed is neither served nor kept.
	connect(t, w, user.Id, "second")
	var invalid *apierr.Error
	if err := c.ScopeCurrent(old); !errors.As(err, &invalid) || invalid.Code != apierr.CodeSessionInvalid {
		t.Fatalf("the replaced connection is still current: %v", err)
	}
	fresh, err := c.ConnectedKey(user.Id, alpha, "chan")
	if err != nil || fresh.Revision == old.Revision {
		t.Fatalf("revision did not change: %v %v", fresh, err)
	}
	if snapshot := connectedVideos(t, c, fresh); !unfinished(snapshot.Sync.Status) || slices.ContainsFunc(snapshot.Videos, func(v platform.Video) bool { return strings.HasPrefix(v.ID, "first-") }) {
		t.Fatalf("reconnect reused the previous authorization's catalog: %+v", snapshot.Sync)
	}
	waitCatalog(t, c, fresh)
	snapshot := connectedVideos(t, c, fresh)
	for _, v := range snapshot.Videos {
		if !strings.HasPrefix(v.ID, "second-") {
			t.Fatalf("previous connection leaked into reconnected results: %+v", v)
		}
	}
	c.mu.Lock()
	kept := len(c.connected)
	c.mu.Unlock()
	if kept != 1 {
		t.Fatalf("the catalog of the replaced connection was kept: %d", kept)
	}
	if err := w.sources.Disconnect(user.Id, alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ConnectedKey(user.Id, alpha, "chan"); err == nil {
		t.Fatal("a disconnected account got a connected key")
	}
}

// A connected job holds a cookie jar: when its connection goes away the job is cancelled and the jar removed,
// even though nobody asks for the catalog again.
func TestConnectedJobIsCancelledWhenItsConnectionEnds(t *testing.T) {
	t.Parallel()
	w := newSyncWorld(t)
	c := w.catalogs(CatalogOptions{ScopeInterval: 5 * time.Millisecond})
	jar, cancelled := make(chan string, 1), make(chan struct{})
	w.syncer.run = func(ctx context.Context, _ int, _ platform.Channel, cookieFile string, _ func(platform.Video) error) error {
		jar <- cookieFile
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}
	user := w.user(t)
	connect(t, w, user.Id, "session")
	key, err := c.ConnectedKey(user.Id, alpha, "chan")
	if err != nil {
		t.Fatal(err)
	}
	connectedVideos(t, c, key)
	path := receive(t, jar, "the connected listing")
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the job's jar: %v %v", info, err)
	}
	if err := w.sources.Disconnect(user.Id, alpha); err != nil {
		t.Fatal(err)
	}
	receive(t, cancelled, "the job to be cancelled")
	entry := waitCatalog(t, c, key)
	if entry.Status != Failed || entry.Code != "cancelled" {
		t.Fatalf("job: %+v", entry)
	}
	w.group.Stop()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the jar outlived the job: %v", err)
	}
	c.mu.Lock()
	kept := len(c.connected)
	c.mu.Unlock()
	if kept != 0 {
		t.Fatalf("a catalog of an ended connection stays in memory: %d", kept)
	}
}

// unfinished tells whether a synchronization has started and not ended: right after a start, a read finds it
// queued or already running.
func unfinished(s Status) bool { return s == Pending || s == Running }

// withoutFirstPage makes the first listing of a channel empty: the whole catalog then comes from the
// syncer alone.
func (w *syncWorld) withoutFirstPage() {
	w.list.fakePlatform.listing = func(_ context.Context, id string, _ int) (platform.Channel, []platform.Video, error) {
		canonical := strings.TrimPrefix(id, "@")
		return platform.Channel{ID: canonical, Platform: alpha, Name: "Channel " + canonical}, nil, nil
	}
}
