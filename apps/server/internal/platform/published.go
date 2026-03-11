package platform

import (
	"sort"
	"strings"
	"time"
)

// The publication of a video is stored in a single date field, as a UTC timestamp. A time of exactly
// 00:00:00.000Z means that only the day is known; any other time is the exact publication time.
// Video.Published mirrors that: "YYYY-MM-DD" for a day, an RFC3339 UTC timestamp for an exact time, ""
// when unknown. This file is the only place that knows the rule.
const (
	// DayLayout is the layout of a publication known to the day.
	DayLayout = "2006-01-02"
	// StoredLayout is the layout PocketBase stores dates in, which also sorts as text.
	StoredLayout = "2006-01-02 15:04:05.000Z"
	dayStored    = "2006-01-02 00:00:00.000Z"
)

// PublishedFromStored converts a stored date to Video.Published.
func PublishedFromStored(stored time.Time) string {
	stored = stored.UTC()
	switch {
	case stored.IsZero():
		return ""
	case stored.Equal(stored.Truncate(24 * time.Hour)):
		return stored.Format(DayLayout)
	}
	return stored.Format(time.RFC3339Nano)
}

// ParsePublished reads a Video.Published value (or a stored date); the zero time when it is not a date.
func ParsePublished(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, StoredLayout, DayLayout} {
		if date, err := time.Parse(layout, value); err == nil {
			return date
		}
	}
	return time.Time{}
}

// PublishedToStored converts Video.Published to the stored date; a value that is not a date is dropped.
func PublishedToStored(published string) string {
	date := ParsePublished(published)
	if date.IsZero() {
		return ""
	}
	return date.UTC().Format(StoredLayout)
}

// StartOfDayStored is the stored form of the first instant of the day of t: a publication known to the day
// is stored as that instant, so SQL that compares days needs it.
func StartOfDayStored(t time.Time) string { return t.UTC().Truncate(24 * time.Hour).Format(dayStored) }

// HasTime tells whether a publication is an exact time rather than a day.
func HasTime(published string) bool { return len(published) > len(DayLayout) }

// CarryPublished completes a publication with the one known for the same video: a missing one is taken
// whole, and a day is refined by the exact time of that same day.
func CarryPublished(published, known string) string {
	if published == "" {
		return known
	}
	if !HasTime(published) && HasTime(known) && strings.HasPrefix(known, published) {
		return known
	}
	return published
}

// ComparePublished orders two publications by instant, then by their text. Two values of the same shape
// are UTC and sort as text, which avoids parsing them.
func ComparePublished(a, b string) int {
	if len(a) == len(b) {
		return strings.Compare(a, b)
	}
	if ta, tb := ParsePublished(a), ParsePublished(b); !ta.Equal(tb) {
		if ta.After(tb) {
			return 1
		}
		return -1
	}
	return strings.Compare(a, b)
}

// SortByPublication orders videos newest first, keeping the order of videos published together.
func SortByPublication(videos []Video) {
	sort.SliceStable(videos, func(i, j int) bool { return ComparePublished(videos[i].Published, videos[j].Published) > 0 })
}
