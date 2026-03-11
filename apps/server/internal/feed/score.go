package feed

import (
	"slices"
	"sort"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// anchor is the video a recommendations request is for; its channel and tags pull same-channel and
// same-topic candidates up.
type anchor struct {
	channelRecordID string
	tags            []string
}

// scored is a candidate with its score.
type scored struct {
	video platform.Video
	score float64
}

// scoreVideo ranks a discovery candidate: a base, the liked-tag frequencies, an optional anchor's shared tags
// and channel, and a recency boost for the last week.
func scoreVideo(v platform.Video, frequency map[string]int, a *anchor, now time.Time) float64 {
	score := 5.0
	for _, tag := range v.Tags {
		score += float64(frequency[tag] * 2)
	}
	if a != nil {
		for _, tag := range v.Tags {
			if slices.Contains(a.tags, tag) {
				score += 3
			}
		}
		if a.channelRecordID != "" && v.ChannelRecordID == a.channelRecordID {
			score += 10
		}
	}
	week := 7 * 24 * time.Hour
	cutoff := now.Add(-week)
	if published := platform.ParsePublished(v.Published); !published.IsZero() && published.After(cutoff) {
		score += published.Sub(cutoff).Seconds() / week.Seconds() * 10
	}
	return score
}

// top keeps the limit best candidates, in the order of their score, and presents them newest first.
func top(ranked []scored, limit int) []platform.Video {
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	results := make([]platform.Video, 0, min(limit, len(ranked)))
	for _, s := range ranked[:min(limit, len(ranked))] {
		results = append(results, s.video)
	}
	platform.SortByPublication(results)
	return results
}
