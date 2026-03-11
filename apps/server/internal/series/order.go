package series

import (
	"sort"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// publication reads a Video.Published value; the zero time when it is unknown.
func publication(value string) time.Time { return platform.ParsePublished(value) }

// inferRestarts tells apart the runs of a series whose numbering starts over: a creator who plays a demo
// and then the released game, or a second playthrough, publishes "(02)" twice under one name. When the numbered
// videos, in publication order, go back to where a run began, each run becomes a season, so the episodes keep
// their own numbers and neither run is mixed into the other.
//
// It is deliberately cautious, because a repeated number can also be a re-upload or a mistake: it needs every
// numbered video to be dated, no explicit season anywhere, at least two runs of at least two episodes each, and no
// repeated number inside a run. Otherwise nothing changes and the publication order applies as before. A number
// that merely arrives out of order (a 5 before the 4) is not a restart: the new number must also be no higher than
// the first one of the run.
func inferRestarts(s *Series) {
	if s.seasonsInferred {
		return
	}
	var numbered []int
	known := map[float64]bool{}
	repeated := false
	for index, item := range s.Items {
		if item.Season != nil {
			return
		}
		if item.Episode == nil {
			continue
		}
		numbered = append(numbered, index)
		repeated = repeated || known[*item.Episode]
		known[*item.Episode] = true
	}
	if !repeated || len(numbered) < 4 {
		return
	}
	dates := make(map[int]time.Time, len(numbered))
	for _, index := range numbered {
		date := publication(s.Items[index].Video.Published)
		if date.IsZero() {
			return
		}
		dates[index] = date
	}
	sort.Slice(numbered, func(i, j int) bool {
		a, b := numbered[i], numbered[j]
		if !dates[a].Equal(dates[b]) {
			return dates[a].Before(dates[b])
		}
		if *s.Items[a].Episode != *s.Items[b].Episode {
			return *s.Items[a].Episode < *s.Items[b].Episode
		}
		return s.Items[a].Video.ID < s.Items[b].Video.ID
	})
	var runs [][]int
	current := []int{numbered[0]}
	for _, index := range numbered[1:] {
		episode, previous, first := *s.Items[index].Episode, *s.Items[current[len(current)-1]].Episode, *s.Items[current[0]].Episode
		if episode < previous && episode <= first {
			runs = append(runs, current)
			current = nil
		}
		current = append(current, index)
	}
	runs = append(runs, current)
	if len(runs) < 2 {
		return
	}
	for _, run := range runs {
		inRun := map[float64]bool{}
		for _, index := range run {
			if inRun[*s.Items[index].Episode] {
				return
			}
			inRun[*s.Items[index].Episode] = true
		}
		if len(run) < 2 {
			return
		}
	}
	for number, run := range runs {
		season := number + 1
		for _, index := range run {
			s.Items[index].Season = &season
		}
	}
	s.seasonsInferred = true
	// A pilot carries no number; dated within or beside a run's span it takes that run's season.
	span := make([][2]time.Time, len(runs))
	for number, run := range runs {
		span[number] = [2]time.Time{dates[run[0]], dates[run[0]]}
		for _, index := range run {
			if dates[index].Before(span[number][0]) {
				span[number][0] = dates[index]
			}
			if dates[index].After(span[number][1]) {
				span[number][1] = dates[index]
			}
		}
	}
	for index, item := range s.Items {
		if item.Episode != nil || item.Season != nil {
			continue
		}
		date := publication(item.Video.Published)
		if date.IsZero() {
			continue
		}
		season := 0
		for number := range runs {
			if !date.Before(span[number][0]) && !date.After(span[number][1]) {
				season = number + 1
				break
			}
		}
		if season == 0 {
			if !date.After(span[0][0]) {
				season = 1
			} else if !date.Before(span[len(span)-1][1]) {
				season = len(span)
			}
		}
		if season != 0 {
			s.Items[index].Season = &season
		}
	}
}

// order sorts the items of a series and says by what: OrderEpisode when every video has its own episode
// number (and seasons are all given or all absent), OrderPublication otherwise.
func order(s *Series) {
	inferRestarts(s)
	numbered, seasons := true, 0
	type slot struct {
		season  int
		episode float64
	}
	seen := map[slot]bool{}
	for _, item := range s.Items {
		if item.Episode == nil {
			numbered = false
			continue
		}
		season := 0
		if item.Season != nil {
			season = *item.Season
			seasons++
		}
		key := slot{season, *item.Episode}
		if seen[key] {
			numbered = false
		}
		seen[key] = true
	}
	// Mixing explicit seasons and missing seasons leaves their relative order
	// ambiguous. Do not invent a season or episode to resolve it.
	if seasons != 0 && seasons != len(s.Items) {
		numbered = false
	}
	s.Order = OrderPublication
	if numbered {
		s.Order = OrderEpisode
	}
	dates := make(map[string]time.Time, len(s.Items))
	for _, item := range s.Items {
		dates[item.Video.ID] = publication(item.Video.Published)
	}
	sort.Slice(s.Items, func(i, j int) bool {
		a, b := s.Items[i], s.Items[j]
		// Prefer every known season/episode even when another item is unnumbered
		// or duplicates a number. Never manufacture missing numbering: those items
		// retain chronological ordering after all numbered episodes.
		if a.Episode != nil && b.Episode != nil {
			if (a.Season == nil) != (b.Season == nil) {
				return a.Season != nil
			}
			if a.Season != nil && *a.Season != *b.Season {
				return *a.Season < *b.Season
			}
			if *a.Episode != *b.Episode {
				return *a.Episode < *b.Episode
			}
		} else if a.Episode != nil || b.Episode != nil {
			if s.seasonsInferred {
				if a.Season != nil && b.Season != nil && *a.Season != *b.Season {
					return *a.Season < *b.Season
				}
				return a.Episode == nil
			}
			return a.Episode != nil
		}
		ad, bd := dates[a.Video.ID], dates[b.Video.ID]
		if ad.IsZero() != bd.IsZero() {
			return !ad.IsZero()
		}
		if !ad.Equal(bd) {
			return ad.Before(bd)
		}
		return a.Video.ID < b.Video.ID
	})
}
