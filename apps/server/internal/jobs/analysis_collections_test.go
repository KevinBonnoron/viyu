package jobs

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// resync makes the platform list again: the stored catalog grows old, is read, and its new synchronization
// is waited for.
func (w *analysisWorld) resync(t *testing.T, channel string) {
	t.Helper()
	w.clock.advance(25 * time.Hour)
	if _, err := w.catalogs.Snapshot(publicKey(channel), SnapshotOptions{}); err != nil {
		t.Fatal(err)
	}
	if entry := waitCatalog(t, w.catalogs, publicKey(channel)); entry.Status != Complete {
		t.Fatalf("resync of %s: %+v", channel, entry)
	}
}

func (w *analysisWorld) collections(listed ...platform.Collection) {
	w.syncer.collections = func(context.Context, platform.Channel) ([]platform.Collection, error) { return listed, nil }
}
func seriesVideoIDs(items []series.Item) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.Video.ID
	}
	return ids
}

// The collections a channel made are read with its catalog and name series in their own order, which take
// their videos away from the series the titles name; a collection that goes away gives them back.
func TestCollectionsOfAChannelNameSeriesThatTakePrecedenceOverTitles(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.collections(platform.Collection{Title: "Best of", VideoIDs: []string{"chan-4", "chan-2", "chan-1"}})
	channel := w.synced(t, "chan", outerWilds("chan")...)
	w.analyse(channel)

	list := w.storedSeries(t, channel)
	if len(list) != 1 || list[0].Title != "Best of" || list[0].Order != "collection" || !slices.Equal(seriesVideoIDs(list[0].Items), []string{"chan-4", "chan-2", "chan-1"}) {
		t.Fatalf("the collection did not name a series: %v", seriesTitles(list))
	}

	// The next synchronization finds no collection: the titles name the series again.
	w.collections()
	w.resync(t, "chan")
	w.analyse(channel)
	if list := w.storedSeries(t, channel); len(list) != 1 || list[0].Title != "Outer Wilds" || len(list[0].Items) != 3 || list[0].Order == "collection" {
		t.Fatalf("after the collection went away: %v", seriesTitles(list))
	}
}

// A collection that cannot be read does not fail the catalog, and does not forget the collections the last
// complete synchronization read.
func TestACollectionsFailureKeepsWhatWasStored(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.collections(platform.Collection{Title: "Best of", VideoIDs: []string{"chan-1", "chan-4"}})
	channel := w.synced(t, "chan", outerWilds("chan")...)
	w.syncer.collections = func(context.Context, platform.Channel) ([]platform.Collection, error) {
		return nil, errors.New("twitch is down")
	}
	w.resync(t, "chan")
	w.analyse(channel)
	titles := map[string]bool{}
	for _, s := range w.storedSeries(t, channel) {
		titles[s.Title] = true
	}
	if !titles["Best of"] || !titles["Outer Wilds"] {
		t.Fatalf("series=%v", titles)
	}
}

// What a catalog still loading knows of the collections is not enough to name series from them.
func TestCollectionsAreAppliedOnlyToACompleteCatalog(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	w.collections(platform.Collection{Title: "Best of", VideoIDs: []string{"chan-4", "chan-2", "chan-1"}})
	channel := w.synced(t, "chan", outerWilds("chan")...)
	key, _ := w.catalogs.canonicalKey(channel)
	w.analysis.infer(context.Background(), channel, key, false)
	for _, s := range w.storedSeries(t, channel) {
		if s.Title == "Best of" {
			t.Fatalf("a series was named from collections of an incomplete catalog: %v", seriesTitles(w.storedSeries(t, channel)))
		}
	}
}

// Twitch VODs expire: the catalog the next synchronization finds is shorter, its entries go, and the series
// shrink with it, one that is left with a single video disappearing.
func TestSeriesShrinkWhenVideosLeaveTheCatalog(t *testing.T) {
	t.Parallel()
	w := newAnalysisWorld(t, nil, AnalysisOptions{})
	videos := dated(titled("chan", "Outer Wilds #1", "Outer Wilds #2", "Outer Wilds #3", "Balatro #1", "Balatro #2"))
	channel := w.synced(t, "chan", videos...)
	w.analyse(channel)
	if list := w.storedSeries(t, channel); len(list) != 2 {
		t.Fatalf("series=%v", seriesTitles(list))
	}

	w.listing("chan", videos[1], videos[2], videos[4])
	w.resync(t, "chan")
	w.analyse(channel)
	list := w.storedSeries(t, channel)
	if len(list) != 1 || list[0].Title != "Outer Wilds" || len(list[0].Items) != 2 {
		t.Fatalf("after two videos expired: %v", seriesTitles(list))
	}
	if records, err := w.app.FindRecordsByFilter("series", "channel = {:channel}", "", 0, 0, map[string]any{"channel": channel.Id}); err != nil || len(records) != 1 {
		t.Fatalf("stored series %d %v", len(records), err)
	}
	snapshot, err := w.catalogs.Snapshot(publicKey("chan"), SnapshotOptions{})
	if err != nil || len(snapshot.Videos) != 3 {
		t.Fatalf("catalog entries of the expired videos stayed: %d %v", len(snapshot.Videos), err)
	}
}
