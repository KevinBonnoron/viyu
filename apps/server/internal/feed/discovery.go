package feed

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/search"
)

const discoveryQueryLimit = 3

type discoveryKey struct{ platform, query string }
type discoveryHit struct {
	id string
	// Search position remains useful when flat listings have no tags or publication date.
	relevance float64
}

// Discovery searches public pages only. Successful pages, including empty ones, are shared for thirty
// minutes; concurrent requests share a lookup. Keys contain only platform and query, never credentials.
type Discovery struct {
	search *search.Service
	pages  *cache.TTL[discoveryKey, []string]
}

// NewDiscovery binds public discovery to the existing search/catalog pipeline and server lifetime.
func NewDiscovery(searcher *search.Service, spawn cache.Spawner) *Discovery {
	return &Discovery{search: searcher, pages: cache.New[discoveryKey, []string](cache.Options{
		TTL: 30 * time.Minute, Max: 256, Spawn: spawn, LoadTimeout: time.Minute,
	})}
}

func (d *Discovery) find(ctx context.Context, active map[string]bool, queries []string) ([]discoveryHit, error) {
	platforms := d.search.Enabled(active)
	type answer struct {
		ids []string
		err error
	}
	answers := make([]answer, len(queries)*len(platforms))
	var wg sync.WaitGroup
	for qi, query := range queries {
		for pi, p := range platforms {
			index := qi*len(platforms) + pi
			wg.Go(func() {
				answers[index].ids, answers[index].err = d.pages.Do(ctx, discoveryKey{p.ID(), query}, func(ctx context.Context) ([]string, error) {
					page, err := d.search.One(ctx, p, platform.Session{}, query, 1)
					if err != nil {
						return nil, err
					}
					ids := make([]string, 0, len(page.Results))
					for _, result := range page.Results {
						if result.Video != nil {
							ids = append(ids, result.Video.RecordID)
						}
					}
					return ids, nil
				})
			})
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var hits []discoveryHit
	var failures []error
	for _, answer := range answers {
		if answer.err != nil {
			failures = append(failures, answer.err)
			continue
		}
		for position, id := range answer.ids {
			hits = append(hits, discoveryHit{id: id, relevance: 12 / (1 + float64(position)/5)})
		}
	}
	if len(answers) > 0 && len(failures) == len(answers) {
		return nil, errors.Join(failures...)
	}
	return hits, nil
}

// Purge drops expired public search pages.
func (d *Discovery) Purge() { d.pages.Purge() }

// discoveryQueries uses tags corroborated by the source titles, not repeated descriptive metadata.
// A language or format tag alone is not an interest. Actual language-learning titles still support it.
// Categories and complete titles keep discovery useful when no tag names the video's subject.
func discoveryQueries(liked, candidates []platform.Video) []string {
	seeds := liked
	if len(seeds) == 0 {
		seeds = candidates
	}
	topics := map[string]int{}
	seen := map[string]bool{}
	normalized := map[string]string{}
	for _, video := range seeds {
		title := " " + topicWords(platform.First(video.OriginalTitle, video.Title)) + " "
		clear(seen)
		for _, tag := range video.Tags {
			topic, known := normalized[tag]
			if !known {
				topic = topicWords(tag)
				normalized[tag] = topic
			}
			if topic != "" && !seen[topic] && strings.Contains(title, " "+topic+" ") {
				topics[topic]++
				seen[topic] = true
			}
		}
	}
	if queries := topicQueries(topics); len(queries) > 0 {
		return queries
	}
	if len(liked) == 0 {
		for _, video := range seeds {
			if video.Category != "" {
				topics[video.Category]++
			}
		}
		if queries := topicQueries(topics); len(queries) > 0 {
			return queries
		}
	}
	var queries []string
	seenChannels := map[string]bool{}
	for _, video := range seeds {
		channel := video.Platform + ":" + video.ChannelID
		if video.ChannelID == "" {
			channel = video.RecordID
		}
		if seenChannels[channel] {
			continue
		}
		seenChannels[channel] = true
		queries = addQuery(queries, platform.First(video.OriginalTitle, video.Title))
		if len(queries) == discoveryQueryLimit {
			break
		}
	}
	return queries
}

// topicWords matches whole words/phrases across case and punctuation, not substrings such as "art" in
// "cartographie". Case variants and duplicate metadata tags contribute only one vote per source video.
func topicWords(text string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ")
}

func topicQueries(frequency map[string]int) []string {
	topics := make([]string, 0, len(frequency))
	for topic := range frequency {
		topics = append(topics, topic)
	}
	slices.SortFunc(topics, func(a, b string) int {
		if frequency[a] != frequency[b] {
			return frequency[b] - frequency[a]
		}
		return strings.Compare(a, b)
	})
	var queries []string
	for _, topic := range topics {
		queries = addQuery(queries, topic)
		if len(queries) == discoveryQueryLimit {
			break
		}
	}
	return queries
}

func addQuery(queries []string, query string) []string {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" || len(queries) >= discoveryQueryLimit {
		return queries
	}
	for _, previous := range queries {
		if strings.EqualFold(previous, query) {
			return queries
		}
	}
	return append(queries, query)
}

// visibleFeedVideos applies preferences before the limit, so hidden entries do not consume the selection.
// In-progress streams remain visible regardless of history or dismissal.
func (s *Service) visibleFeedVideos(userID, profileID string, videos []platform.Video) ([]platform.Video, error) {
	settings, err := s.app.FindFirstRecordByFilter("settings", "user = {:user}", dbx.Params{"user": userID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	hideWatched, hideSubscriberOnly := false, false
	if settings != nil {
		hideWatched = settings.GetBool("hide_watched")
		hideSubscriberOnly = settings.GetBool("hide_subscriber_only")
	}
	ids := make([]any, 0, len(videos))
	for _, video := range videos {
		ids = append(ids, video.RecordID)
	}
	hidden := map[string]bool{}
	for chunk := range slices.Chunk(ids, queryBatch) {
		var reactions []*core.Record
		if err := s.app.RecordQuery("video_reactions").
			AndWhere(dbx.HashExp{"profile": profileID, "reaction": "not_interested"}).
			AndWhere(dbx.In("video", chunk...)).All(&reactions); err != nil {
			return nil, err
		}
		for _, reaction := range reactions {
			hidden[reaction.GetString("video")] = true
		}
		if hideWatched {
			var history []*core.Record
			if err := s.app.RecordQuery("watch_history").
				AndWhere(dbx.HashExp{"profile": profileID, "watched": true}).
				AndWhere(dbx.In("video", chunk...)).All(&history); err != nil {
				return nil, err
			}
			for _, entry := range history {
				hidden[entry.GetString("video")] = true
			}
		}
	}
	visible := videos[:0]
	for _, video := range videos {
		if (hideSubscriberOnly && video.SubscriberOnly) || (!video.Live && hidden[video.RecordID]) {
			continue
		}
		visible = append(visible, video)
	}
	return visible, nil
}
