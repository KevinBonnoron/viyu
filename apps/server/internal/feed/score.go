package feed

import (
	"slices"
	"sort"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// anchor is the video a recommendations request is for.
type anchor struct {
	channelRecordID string
	tags            []string
	published       time.Time
}

// scored is a candidate with its contextual score and publication-period tier.
type scored struct {
	video      platform.Video
	score      float64
	samePeriod bool
}

// scoreVideo combines profile and anchor context. Without an anchor date, the usual recency boost applies.
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
	if a == nil || a.published.IsZero() {
		week := 7 * 24 * time.Hour
		cutoff := now.Add(-week)
		if published := platform.ParsePublished(v.Published); !published.IsZero() && published.After(cutoff) {
			score += published.Sub(cutoff).Seconds() / week.Seconds() * 10
		}
	}
	return score
}

// rankVideo computes the period tier and proximity once per candidate. A one-year window on either side
// takes precedence over the contextual score, which otherwise could be dominated by many profile likes.
func rankVideo(v platform.Video, frequency map[string]int, a *anchor, now time.Time) scored {
	result := scored{video: v, score: scoreVideo(v, frequency, a, now)}
	if a != nil && !a.published.IsZero() {
		if published := platform.ParsePublished(v.Published); !published.IsZero() {
			distance := published.Sub(a.published).Hours()
			if distance < 0 {
				distance = -distance
			}
			const yearHours = 365 * 24
			if distance < yearHours {
				result.samePeriod = true
				result.score += 30 * (1 - distance/yearHours)
			}
		}
	}
	return result
}

// topFeed preserves relevance while giving each channel a turn before repeating one. A single available
// channel can still fill the feed; publication is only a tie-breaker, not the final ordering.
func topFeed(ranked []scored, limit int) []platform.Video {
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].video.Live != ranked[j].video.Live {
			return ranked[i].video.Live
		}
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].video.Published > ranked[j].video.Published
	})
	groups := map[string]int{}
	var queues [][]int
	for i, candidate := range ranked {
		key := candidate.video.Platform + ":" + candidate.video.ChannelID
		// Unattributed videos are independent, not a synthetic channel that crowds out real ones.
		if candidate.video.ChannelID == "" {
			key = candidate.video.RecordID
		}
		group, exists := groups[key]
		if !exists {
			group = len(queues)
			groups[key] = group
			queues = append(queues, nil)
		}
		queues[group] = append(queues[group], i)
	}
	count := min(limit, len(ranked))
	results := make([]platform.Video, 0, count)
	for round := 0; len(results) < count; round++ {
		for _, queue := range queues {
			if round < len(queue) {
				results = append(results, ranked[queue[round]].video)
				if len(results) == count {
					break
				}
			}
		}
	}
	return results
}

// topRanked retains score order and optionally reserves its first slot for a continuation.
func topRanked(ranked []scored, limit int, prefix *platform.Video) []platform.Video {
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].samePeriod != ranked[j].samePeriod {
			return ranked[i].samePeriod
		}
		return ranked[i].score > ranked[j].score
	})
	count := min(limit, len(ranked))
	if prefix != nil {
		count = min(limit, len(ranked)+1)
	}
	results := make([]platform.Video, 0, count)
	if prefix != nil && limit > 0 {
		results = append(results, *prefix)
	}
	for _, s := range ranked[:min(limit-len(results), len(ranked))] {
		results = append(results, s.video)
	}
	return results
}
