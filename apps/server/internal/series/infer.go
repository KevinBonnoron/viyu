package series

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Item is a video of a series with the numbering the series knows for it.
type Item struct {
	Video   platform.Video `json:"video"`
	Episode *float64       `json:"episode"`
	Season  *int           `json:"season"`
}

// Summary is a series without its videos.
type Summary struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	ChannelID         string `json:"channelId"`
	ChannelRecordID   string `json:"channelRecordId"`
	ChannelName       string `json:"channelName"`
	Platform          string `json:"platform"`
	Thumbnail         string `json:"thumbnail"`
	VideoCount        int    `json:"videoCount"`
	LatestPublishedAt string `json:"latestPublishedAt"`
	Order             string `json:"order"`
}

// Series is a series with its videos, in the order Order names.
type Series struct {
	Summary
	Items []Item `json:"items"`

	// seasonsInferred remembers that the seasons come from a numbering that starts over (see
	// inferRestarts), not from the titles: unnumbered videos then sit by date between the seasons.
	seasonsInferred bool
}

// TitleOf is the title a video is read by: its verified original title, else the title as served. The
// listing title of a video that was never opened is not verified and may be a translation.
func TitleOf(v platform.Video) string {
	if v.OriginalTitleVerified && v.OriginalTitle != "" {
		return v.OriginalTitle
	}
	return v.Title
}

// groupID identifies a series by where it lives and what it is called: the spelling of the name does not
// change it.
func groupID(platformID, channelID, name string) string {
	sum := sha256.Sum256([]byte(platformID + "\x00" + channelID + "\x00" + normalizedName(name)))
	return hex.EncodeToString(sum[:])
}

func episodeKey(v platform.Video) string { return v.Platform + ":" + v.ID }

// Infer groups the videos by the series their titles name, per platform and channel, and summarizes them.
// Only series of at least two videos are kept. Videos with no id or platform, and those a connected source
// fetched privately (Source is set), never enter a public series. episodes are the numbers read from
// thumbnails, keyed platform+":"+native id; they only fill a number the title does not give. The result is
// sorted (see Sort); Series.ID is a hash that identifies the series until Store.Save replaces it with the
// record id.
func (p *Parser) Infer(videos []platform.Video, episodes map[string]int) []Series {
	groups := map[string]*Series{}
	seen := map[string]bool{}
	for _, video := range videos {
		if video.Source != "" || video.ID == "" || video.Platform == "" {
			continue
		}
		key := video.Platform + "\x00" + video.ChannelID + "\x00" + video.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		parsed, ok := p.Parse(TitleOf(video))
		if !ok {
			continue
		}
		id := groupID(video.Platform, video.ChannelID, parsed.Name)
		group := groups[id]
		if group == nil {
			group = &Series{Summary: Summary{ID: id, Title: parsed.Name, ChannelID: video.ChannelID, ChannelRecordID: video.ChannelRecordID, ChannelName: video.ChannelName, Platform: video.Platform}, Items: []Item{}}
			groups[id] = group
		} else if parsed.Name < group.Title {
			// Choosing a display spelling deterministically avoids map/listing
			// iteration order changing the visible series title.
			group.Title = parsed.Name
		}
		group.Items = append(group.Items, Item{Video: video, Episode: parsed.Episode, Season: parsed.Season})
	}
	list := make([]Series, 0, len(groups))
	for _, group := range groups {
		if len(group.Items) < 2 {
			continue
		}
		fillEpisodes(group, episodes)
		order(group)
		summarize(group)
		list = append(list, *group)
	}
	Sort(list)
	return list
}

// WithEpisodes fills the episode numbers read from thumbnails (keyed platform+":"+native id) into the
// videos whose title gives none, then orders the series again. A number that is not positive is not a
// reading. A series that comes from storage has no OCR numbers yet: this is how they join it. A series a
// collection ordered keeps its order, and its videos their lack of numbers.
func (p *Parser) WithEpisodes(list []Series, episodes map[string]int) {
	for i := range list {
		if list[i].Order == OrderCollection {
			continue
		}
		fillEpisodes(&list[i], episodes)
		order(&list[i])
		summarize(&list[i])
	}
}

func fillEpisodes(s *Series, episodes map[string]int) {
	if len(episodes) == 0 {
		return
	}
	for i := range s.Items {
		item := &s.Items[i]
		if episode := episodes[episodeKey(item.Video)]; item.Episode == nil && episode > 0 {
			number := float64(episode)
			item.Episode = &number
		}
	}
}

// summarize computes what a listing shows of an ordered series.
func summarize(s *Series) {
	s.VideoCount = len(s.Items)
	var latest time.Time
	s.Thumbnail = ""
	for _, item := range s.Items {
		if published := publication(item.Video.Published); published.After(latest) {
			latest = published
		}
		if s.Thumbnail == "" && item.Video.Thumbnail != "" {
			s.Thumbnail = item.Video.Thumbnail
		}
	}
	s.LatestPublishedAt = ""
	if !latest.IsZero() {
		s.LatestPublishedAt = latest.UTC().Format(time.RFC3339Nano)
	}
}

// MissingPublications lists the videos whose publication date is unknown in the series that need dates to be
// ordered: those where a number repeats, since only dates tell apart a numbering that starts over (a demo,
// then the released game). Fully numbered series with no repeats need nothing, and neither do the ones a
// collection ordered. Listing titles are used as supplied; only individual metadata gives dates.
func MissingPublications(list []Series) []platform.Video {
	var videos []platform.Video
	for _, s := range list {
		if s.Order == OrderCollection {
			continue
		}
		numbered, repeated := true, false
		seen := map[float64]bool{}
		for _, item := range s.Items {
			if item.Episode == nil {
				numbered = false
				break
			}
			// Without an explicit season, a repeated number may be a numbering that starts over; only the
			// dates tell the runs apart.
			repeated = repeated || (item.Season == nil && seen[*item.Episode])
			seen[*item.Episode] = true
		}
		// Fewer than four videos cannot hold two runs of two: repeated numbers there are tie-breakers,
		// not worth a request.
		if numbered && (!repeated || len(s.Items) < 4) {
			continue
		}
		for _, item := range s.Items {
			if publication(item.Video.Published).IsZero() {
				videos = append(videos, item.Video)
			}
		}
	}
	return videos
}

// ThumbnailCandidates lists the videos of the series that have no episode number: the ones whose number may
// still be read from a thumbnail. A series a collection ordered has no use for numbers.
func ThumbnailCandidates(list []Series) []platform.Video {
	var videos []platform.Video
	for _, s := range list {
		if s.Order == OrderCollection {
			continue
		}
		for _, item := range s.Items {
			if item.Episode == nil {
				videos = append(videos, item.Video)
			}
		}
	}
	return videos
}

// Sort orders series by their latest known publication, newest first, then by title and id.
func Sort(list []Series) {
	sort.Slice(list, func(i, j int) bool {
		a, b := publication(list[i].LatestPublishedAt), publication(list[j].LatestPublishedAt)
		if !a.Equal(b) {
			return a.After(b)
		}
		if list[i].Title != list[j].Title {
			return list[i].Title < list[j].Title
		}
		return list[i].ID < list[j].ID
	})
}
