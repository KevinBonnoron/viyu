// Package stats aggregates what a profile watched: time and distinct videos by tag, category, channel, month
// and platform, from its watch history.
package stats

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

const (
	topTags       = 20
	topCategories = 10
	topChannels   = 10
	monthsKept    = 12
	// tagLength is the longest tag worth ranking: a sentence typed into a tag field says nothing about taste.
	tagLength = 40
	// queryBatch bounds how many ids one IN query looks up.
	queryBatch = 200
)

// periods are the windows a viewer can look at, in days; "all" has none.
var periods = map[string]int{"all": 0, "30d": 30, "90d": 90, "365d": 365}

// Entry is a tag or a category: Key is its normalized form (what a filter takes), Name how it is most often written.
type Entry struct {
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Count   int     `json:"count"`
}

// Channel is a channel the profile watched.
type Channel struct {
	Platform  string  `json:"platform"`
	ChannelID string  `json:"channelId"`
	RecordID  string  `json:"recordId,omitempty"`
	Name      string  `json:"name"`
	Thumbnail string  `json:"thumbnail"`
	Seconds   float64 `json:"seconds"`
	Count     int     `json:"count"`
}

// MarshalJSON addresses the channel by its record id: the platform-native id stays on the server, like
// everywhere else the API names a channel.
func (c Channel) MarshalJSON() ([]byte, error) {
	type plain Channel
	out := plain(c)
	if out.RecordID != "" {
		out.ChannelID = out.RecordID
	}
	return json.Marshal(out)
}

// Month is what was watched in a calendar month (UTC), "YYYY-MM".
type Month struct {
	Month   string  `json:"month"`
	Seconds float64 `json:"seconds"`
	Count   int     `json:"count"`
}

// Platform is what was watched on a platform.
type Platform struct {
	Platform string  `json:"platform"`
	Seconds  float64 `json:"seconds"`
	Count    int     `json:"count"`
}

// Totals counts the videos, the time and the distinct channels watched.
type Totals struct {
	Videos   int     `json:"videos"`
	Seconds  float64 `json:"seconds"`
	Channels int     `json:"channels"`
}

// Stats is what one profile watched: time and distinct videos, by tag, category, channel, month and platform.
type Stats struct {
	Period        string     `json:"period"`
	Totals        Totals     `json:"totals"`
	TopTags       []Entry    `json:"topTags"`
	TopCategories []Entry    `json:"topCategories"`
	TopChannels   []Channel  `json:"topChannels"`
	ByMonth       []Month    `json:"byMonth"`
	ByPlatform    []Platform `json:"byPlatform"`
}

// Service computes the statistics of the profiles. It is safe for concurrent use.
type Service struct {
	app      core.App
	store    *catalog.Store
	accounts *sources.Service
}

// New returns the service.
func New(app core.App, store *catalog.Store, accounts *sources.Service) *Service {
	return &Service{app: app, store: store, accounts: accounts}
}

// normalizeTopic is the key of a tag or category: trimmed, inner whitespace collapsed, lower-cased. Accents are
// kept: "série" and "serie" are different words. Clients apply the same rule to filter by topic.
func normalizeTopic(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// tally is a running total that remembers how its name was written, to show the most common spelling.
type tally struct {
	seconds float64
	count   int
	names   map[string]int
}

type tallies map[string]*tally

func (t tallies) add(key, name string, seconds float64) {
	entry := t[key]
	if entry == nil {
		entry = &tally{names: map[string]int{}}
		t[key] = entry
	}
	entry.seconds += seconds
	entry.count++
	entry.names[name]++
}

// display is the most used spelling, the alphabetically first among equals so the answer never flips.
func (t *tally) display() string {
	best, bestCount := "", 0
	for name, count := range t.names {
		if count > bestCount || (count == bestCount && name < best) {
			best, bestCount = name, count
		}
	}
	return best
}

func (t tallies) top(limit int) []Entry {
	entries := make([]Entry, 0, len(t))
	for key, entry := range t {
		entries = append(entries, Entry{Key: key, Name: entry.display(), Seconds: entry.seconds, Count: entry.count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Seconds != entries[j].Seconds {
			return entries[i].Seconds > entries[j].Seconds
		}
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Key < entries[j].Key
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

// Compute answers what the profile watched in the period ("all", "30d", "90d" or "365d"; "" is "all"). An
// unknown period is apierr.Invalid. The caller has checked that the profile belongs to the account.
//
// Entries of a platform the account turned off are left out, as everywhere else. Months count a video once,
// in the month it was last watched, since a history entry keeps only its latest date.
func (s *Service) Compute(userID, profileID, period string) (Stats, error) {
	if period == "" {
		period = "all"
	}
	days, known := periods[period]
	if !known {
		return Stats{}, apierr.Invalid("Unknown period")
	}
	const message = "Failed to read statistics"
	active, err := s.accounts.Activations(userID)
	if err != nil {
		return Stats{}, err
	}
	filter, params := "profile = {:profile}", dbx.Params{"profile": profileID}
	if days > 0 {
		// An entry with no date is older than any window.
		filter += " && watched_at >= {:since}"
		params["since"] = time.Now().AddDate(0, 0, -days).UTC().Format(platform.StoredLayout)
	}
	entries, err := s.app.FindRecordsByFilter("watch_history", filter, "", 0, 0, params)
	if err != nil {
		return Stats{}, apierr.Internal(message, err)
	}
	stats, err := s.aggregate(entries, active, period)
	if err != nil {
		return Stats{}, apierr.Internal(message, err)
	}
	return stats, nil
}

// aggregate tallies history entries. A row is identified by its video, which is where its platform comes
// from; one whose video is gone, or whose platform the account turned off, is left out.
func (s *Service) aggregate(entries []*core.Record, active map[string]bool, period string) (Stats, error) {
	videos, err := s.store.HistoryVideos(entries)
	if err != nil {
		return Stats{}, err
	}
	kept := make([]*core.Record, 0, len(entries))
	for _, entry := range entries {
		video := videos[entry.GetString("video")]
		if video == nil {
			continue
		}
		if enabled, known := active[video.GetString("platform")]; known && !enabled {
			continue
		}
		kept = append(kept, entry)
	}

	stats := Stats{Period: period, TopTags: []Entry{}, TopCategories: []Entry{}, TopChannels: []Channel{}, ByMonth: []Month{}, ByPlatform: []Platform{}}
	tags, categories := tallies{}, tallies{}
	channels := map[string]*Channel{}
	months := map[string]*Month{}
	platforms := map[string]*Platform{}
	for _, entry := range kept {
		seconds := max(entry.GetFloat("duration_watched"), 0)
		stats.Totals.Videos++
		stats.Totals.Seconds += seconds

		video := videos[entry.GetString("video")]
		platformID := video.GetString("platform")
		if platforms[platformID] == nil {
			platforms[platformID] = &Platform{Platform: platformID}
		}
		platforms[platformID].Seconds += seconds
		platforms[platformID].Count++

		if watched := entry.GetDateTime("watched_at").Time(); !watched.IsZero() {
			key := watched.UTC().Format("2006-01")
			if months[key] == nil {
				months[key] = &Month{Month: key}
			}
			months[key].Seconds += seconds
			months[key].Count++
		}

		var videoTags []string
		_ = video.UnmarshalJSONField("tags", &videoTags)
		seen := map[string]bool{}
		for _, tag := range videoTags {
			key := normalizeTopic(tag)
			if key == "" || utf8.RuneCountInString(key) > tagLength || seen[key] {
				continue
			}
			seen[key] = true
			tags.add(key, strings.Join(strings.Fields(tag), " "), seconds)
		}
		if category := strings.Join(strings.Fields(video.GetString("category")), " "); category != "" {
			categories.add(normalizeTopic(category), category, seconds)
		}
		if channelID := video.GetString("channel"); channelID != "" {
			if channels[channelID] == nil {
				channels[channelID] = &Channel{RecordID: channelID}
			}
			channels[channelID].Seconds += seconds
			channels[channelID].Count++
		}
	}
	stats.Totals.Channels = len(channels)
	stats.TopTags, stats.TopCategories = tags.top(topTags), categories.top(topCategories)

	ranked, err := s.rankChannels(channels)
	if err != nil {
		return Stats{}, err
	}
	stats.TopChannels = ranked

	for _, month := range months {
		stats.ByMonth = append(stats.ByMonth, *month)
	}
	sort.Slice(stats.ByMonth, func(i, j int) bool { return stats.ByMonth[i].Month < stats.ByMonth[j].Month })
	if len(stats.ByMonth) > monthsKept {
		stats.ByMonth = stats.ByMonth[len(stats.ByMonth)-monthsKept:]
	}
	for _, p := range platforms {
		stats.ByPlatform = append(stats.ByPlatform, *p)
	}
	sort.Slice(stats.ByPlatform, func(i, j int) bool {
		if stats.ByPlatform[i].Seconds != stats.ByPlatform[j].Seconds {
			return stats.ByPlatform[i].Seconds > stats.ByPlatform[j].Seconds
		}
		return stats.ByPlatform[i].Platform < stats.ByPlatform[j].Platform
	})
	return stats, nil
}

// rankChannels names the watched channels (loaded in batches) and keeps the most watched ones.
func (s *Service) rankChannels(channels map[string]*Channel) ([]Channel, error) {
	ranked := make([]Channel, 0, len(channels))
	ids := make([]string, 0, len(channels))
	for id := range channels {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for chunk := range slices.Chunk(ids, queryBatch) {
		records, err := s.app.FindRecordsByIds("channels", chunk)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			channel := channels[record.Id]
			channel.Platform, channel.ChannelID = record.GetString("platform"), record.GetString("platform_id")
			channel.Name, channel.Thumbnail = record.GetString("name"), record.GetString("thumbnail")
			ranked = append(ranked, *channel)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Seconds != ranked[j].Seconds {
			return ranked[i].Seconds > ranked[j].Seconds
		}
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].RecordID < ranked[j].RecordID
	})
	return ranked[:min(len(ranked), topChannels)], nil
}
