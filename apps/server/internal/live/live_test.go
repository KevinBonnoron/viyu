package live

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

// fake is a platform whose live lookups are scripted.
type fake struct {
	platform.Platform
	id     string
	stub   bool
	lookup func(channelID string) (platform.LiveStream, error)
	calls  atomic.Int32
}

func (f *fake) ID() string                          { return f.id }
func (f *fake) ValidLiveChannel(id string) bool     { return id != "" }
func (f *fake) ValidChannelID(id string) bool       { return id != "" }
func (f *fake) ValidVideoID(id string) bool         { return id != "" }
func (f *fake) SingleTitle() bool                   { return false }
func (f *fake) ChannelIDAliases(id string) []string { return []string{id} }
func (f *fake) LiveThumbnail(s platform.LiveStream) string {
	return "https://img.test/" + s.VideoID + ".jpg"
}
func (f *fake) EndLive(id string) platform.LiveEnd {
	end := platform.LiveEnd{ChannelIDs: []string{id}}
	if f.stub {
		end.StubVideoID = id
	}
	return end
}
func (f *fake) LiveStream(_ context.Context, id string) (platform.LiveStream, error) {
	f.calls.Add(1)
	return f.lookup(id)
}

func setup(t *testing.T, p *fake) (*Service, *catalog.Store, core.App) {
	t.Helper()
	app := testutil.NewApp(t)
	group := jobs.NewGroup()
	t.Cleanup(group.Stop)
	registry := platform.NewRegistry(p)
	store := catalog.New(app, registry, catalog.Options{})
	return New(Options{App: app, Platforms: registry, Store: store, Spawn: group.Go}), store, app
}

func TestALiveStreamIsStoredAddressedByRecordIDAndCached(t *testing.T) {
	t.Parallel()
	p := &fake{id: "twitch", lookup: func(id string) (platform.LiveStream, error) {
		return platform.LiveStream{Live: true, VideoID: "stream1", Title: "On air", Viewers: 12, Category: "Games"}, nil
	}}
	service, store, _ := setup(t, p)
	if _, err := store.UpsertChannel("twitch", "chan", "Channel", "", ""); err != nil {
		t.Fatal(err)
	}
	stream := service.Lookup(context.Background(), "twitch", "chan")
	if !stream.Live || stream.RecordID == "" {
		t.Fatalf("stream: %+v", stream)
	}
	record, err := store.FindVideo("twitch", "stream1")
	if err != nil || record.Id != stream.RecordID || !record.GetBool("live") {
		t.Fatalf("stored stream: %v %v", record, err)
	}
	if again := service.Lookup(context.Background(), "twitch", "chan"); again.RecordID != stream.RecordID || p.calls.Load() != 1 {
		t.Fatalf("a second lookup within the minute asked the platform again (%d calls)", p.calls.Load())
	}
	data, err := json.Marshal(stream)
	if err != nil || strings.Contains(string(data), "stream1") {
		t.Fatalf("the native video id leaked: %s %v", data, err)
	}
}

func TestAFailedLookupIsNotLiveAndNotCached(t *testing.T) {
	t.Parallel()
	var fail atomic.Bool
	fail.Store(true)
	p := &fake{id: "twitch"}
	p.lookup = func(string) (platform.LiveStream, error) {
		if fail.Load() {
			return platform.LiveStream{}, context.DeadlineExceeded
		}
		return platform.LiveStream{}, nil
	}
	service, _, _ := setup(t, p)
	if stream := service.Lookup(context.Background(), "twitch", "chan"); stream.Live {
		t.Fatalf("failed lookup: %+v", stream)
	}
	fail.Store(false)
	service.Lookup(context.Background(), "twitch", "chan")
	service.Lookup(context.Background(), "twitch", "chan")
	if p.calls.Load() != 2 {
		t.Fatalf("calls: %d, want the failure retried once and the answer cached", p.calls.Load())
	}
	if stream := service.Lookup(context.Background(), "unknown", "chan"); stream.Live {
		t.Fatal("an unknown platform is live")
	}
}

func TestAChannelFoundOverClearsItsLiveVideos(t *testing.T) {
	t.Parallel()
	live := atomic.Bool{}
	live.Store(true)
	p := &fake{id: "twitch"}
	p.lookup = func(string) (platform.LiveStream, error) {
		if live.Load() {
			return platform.LiveStream{Live: true, VideoID: "s1"}, nil
		}
		return platform.LiveStream{}, nil
	}
	service, store, _ := setup(t, p)
	if _, err := store.UpsertChannel("twitch", "chan", "Channel", "", ""); err != nil {
		t.Fatal(err)
	}
	service.Lookup(context.Background(), "twitch", "chan")
	live.Store(false)
	service.Purge()
	// The cached status outlives the stream by up to the TTL: expire it by looking up another key's worth of time.
	service.statuses.Delete("twitch:chan")
	service.Lookup(context.Background(), "twitch", "chan")
	record, err := store.FindVideo("twitch", "s1")
	if err != nil || record.GetBool("live") {
		t.Fatalf("the finished stream kept its live flag: %v %v", record, err)
	}
}

func TestSubscribedKeepsTheChannelsThatAreLiveMostWatchedFirst(t *testing.T) {
	t.Parallel()
	p := &fake{id: "twitch"}
	p.lookup = func(id string) (platform.LiveStream, error) {
		switch id {
		case "big":
			return platform.LiveStream{Live: true, VideoID: "vbig", Viewers: 100}, nil
		case "small":
			return platform.LiveStream{Live: true, VideoID: "vsmall", Viewers: 5}, nil
		}
		return platform.LiveStream{}, nil
	}
	service, store, _ := setup(t, p)
	var records []*core.Record
	for _, id := range []string{"idle", "small", "big"} {
		record, err := store.UpsertChannel("twitch", id, id, "", "")
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	streams, err := service.Subscribed(context.Background(), records)
	if err != nil || len(streams) != 2 || streams[0].ChannelID != records[2].Id || streams[1].ChannelID != records[1].Id {
		t.Fatalf("streams: %+v %v", streams, err)
	}
	if !strings.HasPrefix(streams[0].Thumbnail, "https://img.test/") {
		t.Fatalf("thumbnail: %q", streams[0].Thumbnail)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Subscribed(cancelled, records); err == nil {
		t.Fatal("a cancelled request was answered")
	}
}

func TestReconcileLooksOnlyAtChannelsWithAFlaggedVideo(t *testing.T) {
	t.Parallel()
	p := &fake{id: "twitch"}
	p.lookup = func(string) (platform.LiveStream, error) { return platform.LiveStream{}, nil }
	service, store, app := setup(t, p)
	quiet, err := store.UpsertChannel("twitch", "quiet", "Quiet", "", "")
	if err != nil {
		t.Fatal(err)
	}
	flagged, err := store.UpsertChannel("twitch", "flagged", "Flagged", "", "")
	if err != nil {
		t.Fatal(err)
	}
	video := platform.Video{ID: "old", Platform: "twitch", ChannelID: "flagged", ChannelName: "Flagged", Title: "t", Live: true}
	if err := store.StoreVideo(&video, ""); err != nil {
		t.Fatal(err)
	}
	service.Reconcile(context.Background(), []*core.Record{quiet, flagged})
	if p.calls.Load() != 1 {
		t.Fatalf("lookups: %d, want only the channel with a flagged video", p.calls.Load())
	}
	record, err := app.FindRecordById("videos", video.RecordID)
	if err != nil || record.GetBool("live") {
		t.Fatalf("flag not cleared: %v %v", record, err)
	}
}
