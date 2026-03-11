package series

import (
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// The ways a series is ordered (Summary.Order).
const (
	// OrderEpisode: every video has its own episode number.
	OrderEpisode = "episode"
	// OrderPublication: by date, where the titles do not number every video.
	OrderPublication = "publication"
	// OrderCollection: the order a collection of the channel gave; titles and dates play no part.
	OrderCollection = "collection"
)

// FromCollections builds the series a channel named itself: one per collection, called what the collection is
// called, with the videos of videos that it lists, in the order it lists them (the items carry no episode
// number: the collection's order is all there is). It also returns the videos that no series claimed, which are
// what inference is left with: a collection takes precedence over the titles for its videos.
//
// videos are those of one channel. Like an inferred series, a series needs two videos: a collection that
// leaves fewer in the catalog (the others expired, or are private) names none and claims nothing. A video a
// collection lists twice counts once, and of two collections with the same name (spelling aside) the first one
// stays.
func FromCollections(videos []platform.Video, collections []platform.Collection) (named []Series, rest []platform.Video) {
	byID := make(map[string]platform.Video, len(videos))
	for _, video := range videos {
		if video.Source == "" && video.ID != "" && video.Platform != "" {
			byID[video.ID] = video
		}
	}
	claimed := map[string]bool{}
	names := map[string]bool{}
	for _, collection := range collections {
		name := strings.TrimSpace(collection.Title)
		key := normalizedName(name)
		if key == "" || names[key] {
			continue
		}
		var items []Item
		listed := map[string]bool{}
		for _, id := range collection.VideoIDs {
			if video, ok := byID[id]; ok && !listed[id] {
				listed[id] = true
				items = append(items, Item{Video: video})
			}
		}
		if len(items) < 2 {
			continue
		}
		names[key] = true
		first := items[0].Video
		entry := Series{
			Summary: Summary{
				ID: groupID(first.Platform, first.ChannelID, name), Title: name,
				ChannelID: first.ChannelID, ChannelRecordID: first.ChannelRecordID, ChannelName: first.ChannelName,
				Platform: first.Platform, Order: OrderCollection,
			},
			Items: items,
		}
		summarize(&entry)
		named = append(named, entry)
		for id := range listed {
			claimed[id] = true
		}
	}
	rest = make([]platform.Video, 0, len(videos))
	for _, video := range videos {
		if !claimed[video.ID] {
			rest = append(rest, video)
		}
	}
	return named, rest
}

// Merge puts the series a channel named itself and the ones inferred from the rest of its videos together,
// sorted (see Sort). An inferred series called like a named one gives way: a name is one series.
func Merge(named, inferred []Series) []Series {
	taken := make(map[string]bool, len(named))
	for _, entry := range named {
		taken[normalizedName(entry.Title)] = true
	}
	list := append([]Series(nil), named...)
	for _, entry := range inferred {
		if !taken[normalizedName(entry.Title)] {
			list = append(list, entry)
		}
	}
	Sort(list)
	return list
}
