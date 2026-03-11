package ocr

import (
	"image"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Word is one word the recognition engine found: where, how sure and in which line of which block of the
// page layout.
type Word struct {
	Text       string
	Confidence float64
	Block      int
	Paragraph  int
	Line       int
	Box        image.Rectangle
}

var (
	episodeLabel = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:episode|ep|#)[ .:_-]*([0-9]{1,3})(?:$|[^a-z0-9])`)
	digits       = regexp.MustCompile(`[0-9]+`)
	bareNumber   = regexp.MustCompile(`^[0-9]{1,3}$`)
	clock        = regexp.MustCompile(`[0-9]{1,4}\s*[:.]\s*[0-9]{1,4}(?:\s*:\s*[0-9]{1,2})?\b`)
	year         = regexp.MustCompile(`\b(?:19|20)[0-9]{2}\b`)
	duration     = regexp.MustCompile(`(?i)\b[0-9]+[ -]*(?:hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b`)
	quality      = regexp.MustCompile(`(?i)\b(?:[0-9]{3,4}p|[248]k|[0-9]+fps)\b`)
)

type wordSpan struct {
	start, end int
	word       Word
}

type line struct {
	text  string
	words []wordSpan
}

func cleanText(word string) string {
	return strings.TrimSpace(strings.ReplaceAll(word, "\u00a0", " "))
}

// lines regroups the words by their line of the page, left to right.
func lines(words []Word) []line {
	groups := make(map[[3]int][]Word)
	keys := make([][3]int, 0)
	for _, word := range words {
		word.Text = cleanText(word.Text)
		if word.Text == "" {
			continue
		}
		key := [3]int{word.Block, word.Paragraph, word.Line}
		if _, exists := groups[key]; !exists {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], word)
	}
	out := make([]line, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		sort.SliceStable(group, func(i, j int) bool { return group[i].Box.Min.X < group[j].Box.Min.X })
		var text strings.Builder
		l := line{}
		for _, word := range group {
			if text.Len() > 0 {
				text.WriteByte(' ')
			}
			start := text.Len()
			text.WriteString(word.Text)
			l.words = append(l.words, wordSpan{start: start, end: text.Len(), word: word})
		}
		l.text = text.String()
		out = append(out, l)
	}
	return out
}

func intersects(start, end int, spans [][]int) bool {
	for _, span := range spans {
		if start < span[1] && span[0] < end {
			return true
		}
	}
	return false
}

func confidence(l line, start, end int) float64 {
	sure := 100.0
	found := false
	for _, span := range l.words {
		if start < span.end && span.start < end {
			sure = min(sure, span.word.Confidence)
			found = true
		}
	}
	if !found {
		return 0
	}
	return sure
}

// parseEpisode reads the episode number off the words of a width x height thumbnail, 0 when there is none
// to trust. Context is mandatory: labeled episodes need a confident label and number; a bare digit must be
// the only recognized text: large and high-confidence. O/0, I/1 and other recognition ambiguities are never
// repaired into invented digits.
func parseEpisode(words []Word, width, height int) int {
	explicit, explicitTrusted, bare, numericCount := 0, false, 0, 0
	numberValue, wordCount := 0, 0
	conflict := false
	for _, l := range lines(words) {
		wordCount += len(l.words)
		excluded := make([][]int, 0)
		for _, pattern := range []*regexp.Regexp{clock, year, duration, quality} {
			excluded = append(excluded, pattern.FindAllStringIndex(l.text, -1)...)
		}
		for _, match := range episodeLabel.FindAllStringSubmatchIndex(l.text, -1) {
			start, end := match[2], match[3]
			if intersects(start, end, excluded) {
				continue
			}
			episode, _ := strconv.Atoi(l.text[start:end])
			if episode < 1 || episode > 999 {
				continue
			}
			if explicit != 0 && explicit != episode {
				conflict = true
			}
			explicit = episode
			// match[0] can include a separator from a neighboring word. Require
			// confidence for the actual label through the digit, not that separator.
			labelStart := match[0]
			for labelStart < start && !strings.ContainsRune("#eE", rune(l.text[labelStart])) {
				labelStart++
			}
			if confidence(l, labelStart, end) >= 75 {
				explicitTrusted = true
			}
		}
		for _, found := range digits.FindAllStringIndex(l.text, -1) {
			if intersects(found[0], found[1], excluded) {
				continue
			}
			numericCount++
			value, err := strconv.Atoi(l.text[found[0]:found[1]])
			if err != nil || value < 1 || value > 999 {
				conflict = true
				continue
			}
			if numberValue != 0 && numberValue != value {
				conflict = true
			}
			numberValue = value
		}
		if len(l.words) == 1 {
			word := l.words[0].word
			if bareNumber.MatchString(word.Text) && word.Confidence >= 92 && word.Box.Dy() >= max(32, height/8) && word.Box.Dx() >= 12 && word.Box.Min.In(image.Rect(0, 0, width, height)) && word.Box.Max.X <= width && word.Box.Max.Y <= height {
				// A small number at the bottom-right is usually a player duration
				// badge, even if OCR dropped its colon and remaining digits.
				if !(word.Box.Min.X >= width*2/3 && word.Box.Min.Y >= height*3/4) {
					value, _ := strconv.Atoi(word.Text)
					if value > 0 && value <= 999 {
						bare = value
					}
				}
			}
		}
	}
	if conflict {
		return 0
	}
	if explicit != 0 && explicitTrusted {
		return explicit
	}
	// A separate TOP/7, 7/MINUTES, or 7/2024 caption is not unambiguous.
	if bare != 0 && numericCount == 1 && wordCount == 1 {
		return bare
	}
	return 0
}
