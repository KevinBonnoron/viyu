package series

import (
	"slices"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func collectionVideos() []platform.Video {
	return channelVideos("UCcollect",
		platform.Video{ID: "a", Title: "Zelda #1", Published: "2026-01-01"},
		platform.Video{ID: "b", Title: "Zelda #2", Published: "2026-01-02"},
		platform.Video{ID: "c", Title: "Zelda #3", Published: "2026-01-03"},
		platform.Video{ID: "d", Title: "Anything", Published: "2026-01-04"},
		platform.Video{ID: "e", Title: "Other", Published: "2026-01-05"},
		platform.Video{ID: "private", Title: "Private", Source: "connected"},
	)
}

// A collection names a series and orders it; its videos leave inference, whose series carry on with the rest.
func TestFromCollectionsNamesAndOrdersSeriesAndClaimsTheirVideos(t *testing.T) {
	t.Parallel()
	named, rest := FromCollections(collectionVideos(), []platform.Collection{
		{Title: " Best of ", VideoIDs: []string{"d", "gone", "a", "d", "private", "e"}},
	})
	if len(named) != 1 {
		t.Fatalf("named=%+v", named)
	}
	got := named[0]
	if got.Title != "Best of" || got.Order != OrderCollection || got.Platform != "youtube" || got.ChannelID != "UCcollect" || got.VideoCount != 3 || got.LatestPublishedAt == "" {
		t.Fatalf("summary=%+v", got.Summary)
	}
	if !slices.Equal(ids(got), []string{"d", "a", "e"}) {
		t.Fatalf("collection order lost or a video that is gone/private/repeated kept: %v", ids(got))
	}
	for _, item := range got.Items {
		if item.Episode != nil || item.Season != nil {
			t.Fatalf("a collection numbers nothing: %+v", item)
		}
	}
	var left []string
	for _, v := range rest {
		left = append(left, v.ID)
	}
	if !slices.Equal(left, []string{"b", "c", "private"}) {
		t.Fatalf("rest=%v", left)
	}
	// What is left is inferred as before: two Zelda episodes still make a series.
	if list := NewParser(nil).Infer(rest, nil); len(list) != 1 || list[0].Title != "Zelda" || len(list[0].Items) != 2 {
		t.Fatalf("inferred=%+v", list)
	}
}

// A collection needs two of its videos in the catalog, and a name is one series.
func TestFromCollectionsKeepsOnlyWorthwhileDistinctCollections(t *testing.T) {
	t.Parallel()
	videos := collectionVideos()
	named, rest := FromCollections(videos, []platform.Collection{
		{Title: "One left", VideoIDs: []string{"a", "gone"}},
		{Title: "", VideoIDs: []string{"a", "b"}},
		{Title: "Zelda  Saga", VideoIDs: []string{"c", "b"}},
		{Title: "zelda saga", VideoIDs: []string{"a", "d"}},
		{Title: "Nothing", VideoIDs: nil},
	})
	if len(named) != 1 || named[0].Title != "Zelda  Saga" || !slices.Equal(ids(named[0]), []string{"c", "b"}) {
		t.Fatalf("named=%+v", named)
	}
	if len(rest) != len(videos)-2 {
		t.Fatalf("a collection that was not kept claimed videos: %d left of %d", len(rest), len(videos))
	}
	if named, rest := FromCollections(videos, nil); len(named) != 0 || len(rest) != len(videos) {
		t.Fatalf("no collections changed something: %+v %d", named, len(rest))
	}
}

// An inferred series called like a collection's gives way, and the result is sorted like any list.
func TestMergeLetsACollectionKeepItsName(t *testing.T) {
	t.Parallel()
	named, rest := FromCollections(collectionVideos(), []platform.Collection{{Title: "Zelda", VideoIDs: []string{"e", "d"}}})
	inferred := NewParser(nil).Infer(rest, nil)
	if len(inferred) != 1 || inferred[0].Title != "Zelda" {
		t.Fatalf("inferred=%+v", inferred)
	}
	merged := Merge(named, inferred)
	if len(merged) != 1 || merged[0].Order != OrderCollection {
		t.Fatalf("merged=%+v", merged)
	}
	if merged := Merge(nil, inferred); len(merged) != 1 || merged[0].Order == OrderCollection {
		t.Fatalf("without collections: %+v", merged)
	}
}

// Numbers read from thumbnails and publication lookups are for inferred series only.
func TestCollectionSeriesNeedNoNumbersNorDates(t *testing.T) {
	t.Parallel()
	videos := channelVideos("UCcollect", platform.Video{ID: "x", Title: "No date"}, platform.Video{ID: "y", Title: "Also none"})
	named, _ := FromCollections(videos, []platform.Collection{{Title: "Set", VideoIDs: []string{"y", "x"}}})
	if len(named) != 1 || len(ThumbnailCandidates(named)) != 0 || len(MissingPublications(named)) != 0 {
		t.Fatalf("named=%+v", named)
	}
	NewParser(nil).WithEpisodes(named, map[string]int{"youtube:x": 4})
	if !slices.Equal(ids(named[0]), []string{"y", "x"}) || named[0].Items[1].Episode != nil {
		t.Fatalf("an episode number reordered a collection: %+v", named[0])
	}
}
