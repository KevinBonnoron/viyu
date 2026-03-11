// Package search runs video searches: on one platform (public or as a connected account) or on several at
// once, and stores what they found so every result can be addressed by record id.
package search

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Service searches the platforms. It holds no state of its own and is safe for concurrent use.
type Service struct {
	platforms *platform.Registry
	store     *catalog.Store
	log       *slog.Logger
}

// New builds a Service. A nil log discards.
func New(platforms *platform.Registry, store *catalog.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{platforms: platforms, store: store, log: log}
}

// Enabled lists, in registry order, the platforms an account uses (active maps a platform id to whether the
// account uses it, see sources.Service.Activations): the ones a search without a selected platform covers,
// so a platform the account turned off is never queried.
func (s *Service) Enabled(active map[string]bool) []platform.Platform {
	all := s.platforms.All()
	enabled := make([]platform.Platform, 0, len(all))
	for _, p := range all {
		if active[p.ID()] {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// One searches a single platform as session says: the zero Session is a public search, a connected one is
// private to its account and its results are owned by it. It returns the results that were stored.
func (s *Service) One(ctx context.Context, p platform.Platform, session platform.Session, query string, page int) (platform.SearchPage, error) {
	result, err := p.Search(ctx, session, query, page)
	if err != nil {
		return platform.SearchPage{}, apierr.From(err)
	}
	if page <= 1 {
		result.Results = withMatchingChannels(result.Results, query)
	}
	result.Results = s.keep(result.Results, session.UserID)
	result.Page = page
	return result, nil
}

// All searches the platforms at once, without credentials, and interleaves their results, so no platform
// buries another. A platform that fails is skipped as long as another answers; with nothing to search the
// page is empty. It returns the results that were stored.
func (s *Service) All(ctx context.Context, platforms []platform.Platform, query string, page int) (platform.SearchPage, error) {
	merged := platform.SearchPage{Results: []platform.Result{}, Page: page}
	if len(platforms) == 0 {
		return merged, nil
	}
	pages := make([]platform.SearchPage, len(platforms))
	failures := make([]error, len(platforms))
	var wg sync.WaitGroup
	for i, p := range platforms {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pages[i], failures[i] = p.Search(ctx, platform.Session{}, query, page)
		}()
	}
	wg.Wait()

	var firstFailure error
	failed := 0
	for i, failure := range failures {
		if failure == nil {
			merged.HasMore = merged.HasMore || pages[i].HasMore
			continue
		}
		failed++
		if firstFailure == nil {
			firstFailure = failure
		}
		safe := apierr.From(failure)
		merged.Failures = append(merged.Failures, platform.SearchFailure{Platform: platforms[i].ID(), Code: safe.Code, Message: safe.Message})
		s.log.Warn("Search failed for a platform", "platform", platforms[i].ID(), "error", failure)
	}
	if failed == len(platforms) {
		return platform.SearchPage{}, apierr.From(firstFailure)
	}
	var interleaved []platform.Result
	for position := 0; ; position++ {
		added := false
		for i := range platforms {
			if failures[i] == nil && position < len(pages[i].Results) {
				interleaved = append(interleaved, pages[i].Results[position])
				added = true
			}
		}
		if !added {
			break
		}
	}
	if page <= 1 {
		interleaved = withMatchingChannels(interleaved, query)
	}
	merged.Results = s.keep(interleaved, "")
	return merged, nil
}

// keep stores the results and returns the ones that were stored, in order: a result that could not be
// stored is dropped, never returned with its native id.
func (s *Service) keep(results []platform.Result, ownerID string) []platform.Result {
	kept, dropped, err := s.store.StoreResults(results, ownerID)
	if dropped > 0 || err != nil {
		s.log.Warn("Dropped search results that could not be stored", "dropped", dropped, "error", err)
	}
	return kept
}

// withMatchingChannels puts first the channel cards a platform's search did not return although the query names
// them: YouTube's search answers videos only, so looking for "mrchonks" listed mrchonks' videos but never the
// channel. A channel is added when its name and the query are the same once case, accents and punctuation are
// set aside, or one contains the other (four characters at least). Each channel is added once, and a channel
// the search already returned as a card is left where it is.
func withMatchingChannels(results []platform.Result, query string) []platform.Result {
	wanted := nameKey(query)
	if wanted == "" {
		return results
	}
	present := map[string]bool{}
	for _, result := range results {
		if result.Channel != nil {
			present[result.Channel.Platform+"\x00"+result.Channel.ID] = true
		}
	}
	var added []platform.Result
	for _, result := range results {
		video := result.Video
		if video == nil || video.ChannelID == "" || video.ChannelName == "" {
			continue
		}
		key := video.Platform + "\x00" + video.ChannelID
		if present[key] || !namesMatch(nameKey(video.ChannelName), wanted) {
			continue
		}
		present[key] = true
		added = append(added, platform.Result{Channel: &platform.ChannelCard{ID: video.ChannelID, Platform: video.Platform, Name: video.ChannelName, Thumbnail: video.ChannelThumbnail}})
	}
	if len(added) == 0 {
		return results
	}
	return append(added, results...)
}

func namesMatch(name, query string) bool {
	if name == "" {
		return false
	}
	if name == query {
		return true
	}
	shorter := min(len(name), len(query))
	return shorter >= 4 && (strings.Contains(name, query) || strings.Contains(query, name))
}

// nameKey reduces a name to its lower-case letters and digits, accents removed.
func nameKey(name string) string {
	var key strings.Builder
	for _, r := range norm.NFKD.String(name) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			key.WriteRune(unicode.ToLower(r))
		}
	}
	return key.String()
}
