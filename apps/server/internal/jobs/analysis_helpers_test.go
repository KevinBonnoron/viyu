package jobs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// fakeReader is a series.EpisodeReader a test scripts. The default reads nothing.
type fakeReader struct {
	calls   atomic.Int32
	episode func(ctx context.Context, urls []string) (int, error)
}

func (r *fakeReader) Episode(ctx context.Context, urls []string) (int, error) {
	r.calls.Add(1)
	if r.episode == nil {
		return 0, nil
	}
	return r.episode(ctx, urls)
}

// analysisWorld is a syncWorld with the analysis on top of it. Nothing runs until a test calls Run or
// analyseChannel.
type analysisWorld struct {
	*syncWorld
	catalogs *Catalogs
	analysis *Analysis
	series   *series.Store
	parser   *series.Parser

	mu       sync.Mutex
	listings map[string][]platform.Video
}

func newAnalysisWorld(t *testing.T, reader series.EpisodeReader, opts AnalysisOptions) *analysisWorld {
	t.Helper()
	w := &analysisWorld{syncWorld: newSyncWorld(t), listings: map[string][]platform.Video{}}
	w.withoutFirstPage()
	w.syncer.run = func(_ context.Context, _ int, channel platform.Channel, _ string, visit func(platform.Video) error) error {
		w.mu.Lock()
		videos := w.listings[channel.ID]
		w.mu.Unlock()
		for _, v := range videos {
			if err := visit(v); err != nil {
				return err
			}
		}
		return nil
	}
	w.catalogs = w.syncWorld.catalogs(CatalogOptions{})
	w.parser = series.NewParser(nil)
	w.series = series.NewStore(w.app, w.store, w.registry, w.parser)
	if opts.Debounce == 0 {
		opts.Debounce = time.Millisecond
	}
	if opts.Now == nil {
		opts.Now = w.clock.Now
	}
	w.analysis = NewAnalysis(w.app, w.registry, w.store, w.sources, w.series, w.parser, reader, w.catalogs, w.group.Go, opts)
	return w
}

// listing sets what the platform lists for a channel.
func (w *analysisWorld) listing(channel string, videos ...platform.Video) {
	w.mu.Lock()
	w.listings[channel] = videos
	w.mu.Unlock()
}

// synced makes the platform list the videos of the channel, has the catalog synchronized and returns the
// channel record.
func (w *analysisWorld) synced(t *testing.T, channel string, videos ...platform.Video) *core.Record {
	t.Helper()
	w.listing(channel, videos...)
	if _, err := w.catalogs.Snapshot(publicKey(channel), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	if entry := waitCatalog(t, w.catalogs, publicKey(channel)); entry.Status != Complete {
		t.Fatalf("catalog of %s: %+v", channel, entry)
	}
	return w.findChannel(t, alpha, channel)
}

// titled lists videos of a channel with the given titles, ids "<channel>-<i>", undated.
func titled(channel string, titles ...string) []platform.Video {
	videos := make([]platform.Video, len(titles))
	for i, title := range titles {
		videos[i] = platform.Video{ID: fmt.Sprintf("%s-%d", channel, i+1), Platform: alpha, ChannelID: channel, ChannelName: "Channel " + channel, Title: title,
			Thumbnail: fmt.Sprintf("https://yt3.googleusercontent.com/%s-%d=s0", channel, i+1)}
	}
	return videos
}

// dated gives the videos of a list publication days, the first one the oldest.
func dated(videos []platform.Video) []platform.Video {
	for i := range videos {
		videos[i].Published = time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC).Format(platform.DayLayout)
	}
	return videos
}

// analyse runs the analysis of a channel on the calling goroutine.
func (w *analysisWorld) analyse(channel *core.Record) {
	w.analysis.analyseChannel(context.Background(), channel, true)
}

// storedSeries reads the stored series of a channel, with the thumbnails' episodes applied.
func (w *analysisWorld) storedSeries(t *testing.T, channel *core.Record) []series.Series {
	t.Helper()
	all, err := w.series.ForChannels([]string{channel.Id})
	if err != nil {
		t.Fatal(err)
	}
	list := all[channel.Id]
	w.analysis.ApplyEpisodes(list)
	return list
}
