package series

import (
	"slices"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// seedCollection stores five videos and the series a collection makes of three of them, in an order that
// neither titles nor dates give.
func seedCollection(t *testing.T, f *fixture) (videos []platform.Video, channelID string, named []Series) {
	t.Helper()
	videos, channelID = f.seed(t, "UCcollect",
		platform.Video{ID: "a", Title: "Alpha", Published: "2026-01-01"},
		platform.Video{ID: "b", Title: "Beta", Published: "2026-01-02"},
		platform.Video{ID: "c", Title: "Gamma", Published: "2026-01-03"},
		platform.Video{ID: "d", Title: "Delta #1", Published: "2026-01-04"},
		platform.Video{ID: "e", Title: "Delta #2", Published: "2026-01-05"},
	)
	named, _ = FromCollections(videos, []platform.Collection{{Title: "Greek", VideoIDs: []string{"c", "a", "b"}}})
	if len(named) != 1 {
		t.Fatalf("named=%+v", named)
	}
	return videos, channelID, named
}

// A series a collection ordered is stored in that order, flagged as such, and read back unchanged: neither
// the titles (which would put Alpha first) nor the dates (Gamma last) are consulted.
func TestCollectionSeriesAreStoredAndReadBackInTheirOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID, named := seedCollection(t, f)
	list := Merge(named, f.parser.Infer(videos[3:], nil))
	if err := f.store.Save(channelID, true, list); err != nil {
		t.Fatal(err)
	}
	records := f.records(t, channelID)
	if len(records) != 2 || records[0].GetString("name") != "Delta" || records[0].GetBool("curated") || records[1].GetString("name") != "Greek" || !records[1].GetBool("curated") {
		t.Fatalf("records=%+v", records)
	}
	got, err := f.store.ForChannels([]string{channelID})
	if err != nil {
		t.Fatal(err)
	}
	var greek Series
	for _, s := range got[channelID] {
		if s.Title == "Greek" {
			greek = s
		}
	}
	if greek.Order != OrderCollection || !slices.Equal(ids(greek), []string{"c", "a", "b"}) || greek.VideoCount != 3 || greek.Items[0].Episode != nil {
		t.Fatalf("greek=%+v", greek)
	}
	one, _, err := f.store.Get(greek.ID)
	if err != nil || !slices.Equal(ids(one), []string{"c", "a", "b"}) || one.Order != OrderCollection {
		t.Fatalf("get=%+v %v", one, err)
	}
	// The thumbnails' numbers are applied to what is stored: not to a series a collection ordered.
	f.parser.WithEpisodes(got[channelID], map[string]int{"youtube:a": 1})
	for _, s := range got[channelID] {
		if s.Title == "Greek" && !slices.Equal(ids(s), []string{"c", "a", "b"}) {
			t.Fatalf("an episode number reordered the collection: %v", ids(s))
		}
	}
	if ok, err := f.store.MembershipMatches(channelID, list); err != nil || !ok {
		t.Fatalf("matches: %v %v", ok, err)
	}
}

// A collection that the streamer reorders rewrites the record (the order is part of what is stored), the same
// one twice does not, and a collection that goes away leaves the series to inference or to nothing.
func TestCollectionSeriesFollowTheCollectionOverTime(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID, named := seedCollection(t, f)
	if err := f.store.Save(channelID, true, named); err != nil {
		t.Fatal(err)
	}
	updates := countUpdates(f.app, "series")
	again, _ := FromCollections(videos, []platform.Collection{{Title: "Greek", VideoIDs: []string{"c", "a", "b"}}})
	if err := f.store.Save(channelID, true, again); err != nil || updates.Load() != 0 {
		t.Fatalf("an unchanged collection was saved: %d %v", updates.Load(), err)
	}
	reordered, _ := FromCollections(videos, []platform.Collection{{Title: "Greek", VideoIDs: []string{"a", "b", "c"}}})
	if err := f.store.Save(channelID, true, reordered); err != nil || updates.Load() != 1 {
		t.Fatalf("a reordered collection must be one update: %d %v", updates.Load(), err)
	}
	if ok, err := f.store.MembershipMatches(channelID, named); err != nil || ok {
		t.Fatalf("the old order still matches: %v %v", ok, err)
	}
	if got := f.records(t, channelID)[0].GetStringSlice("videos"); len(got) != 3 || got[0] != reordered[0].Items[0].Video.RecordID {
		t.Fatalf("stored order %v", got)
	}

	// A catalog still loading never rewrites it, even with an inferred series of the same name.
	clash := f.parser.Infer(channelVideos("UCcollect", platform.Video{ID: "a", Title: "Greek #1"}, platform.Video{ID: "b", Title: "Greek #2"}), nil)
	if err := f.store.Save(channelID, false, clash); err != nil {
		t.Fatal(err)
	}
	if records := f.records(t, channelID); len(records) != 1 || !records[0].GetBool("curated") || len(records[0].GetStringSlice("videos")) != 3 {
		t.Fatalf("an incomplete catalog rewrote a collection: %+v", records)
	}

	// The collection is gone and the complete catalog infers the same name: the series stays, no longer curated.
	vids, _ := f.seed(t, "UCcollect", platform.Video{ID: "g1", Title: "Greek #1"}, platform.Video{ID: "g2", Title: "Greek #2"})
	if err := f.store.Save(channelID, true, f.parser.Infer(vids, nil)); err != nil {
		t.Fatal(err)
	}
	records := f.records(t, channelID)
	if len(records) != 1 || records[0].GetBool("curated") || records[0].GetString("name") != "Greek" {
		t.Fatalf("records=%+v", records)
	}
	// And with neither, the complete catalog removes it.
	if err := f.store.Save(channelID, true, nil); err != nil || len(f.records(t, channelID)) != 0 {
		t.Fatalf("a series nobody names stayed: %v", err)
	}
}
