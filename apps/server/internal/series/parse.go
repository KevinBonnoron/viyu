// Package series infers the series a channel's videos belong to from their titles, orders their episodes
// and stores them. Inference is pure (it reads titles, never the network) and requires an explicit
// delimiter or episode marker: there is no common-prefix fallback, so unrelated recurring topics never
// become an invented series.
package series

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Parsed is what one title says about its series.
type Parsed struct {
	Name    string
	Episode *float64
	Season  *int
}

var (
	pairedSeries   = regexp.MustCompile(`(?:^|\s)[-–—]\s*(.+?)\s*[-–—]\s*(?:\[[^\]]*\]\s*)*$`)
	dashSeparator  = regexp.MustCompile(`\s+[-–—]\s+`)
	trailingNote   = regexp.MustCompile(`\s*\[[^\]]*\]\s*$`)
	seasonCompact  = regexp.MustCompile(`(?i)(?:^|[\s|:–—(\[-])s([0-9]{1,2})[\s._-]*(?:episode|épisode|ep|ép|e)[\s._-]*([0-9]{1,3})\b`)
	seasonWords    = regexp.MustCompile(`(?i)(?:^|[\s|:–—-])(?:season|saison)\s*([0-9]{1,2})[\s.,:|–—-]*(?:episode|épisode|epis|épis|ep|ép)[\s.:-]*([0-9]{1,3})\b`)
	episodeMarker  = regexp.MustCompile(`(?i)(?:^|[\s|:–—(\[-])(?:episode|épisode|epis|épis|ep|ép|n[°º])[\s.:-]*(?:n[°ºo]\s*)?([0-9]{1,3}(?:\.[0-9]+)?)\b`)
	partMarker     = regexp.MustCompile(`(?i)(?:^|[\s|:–—(\[-])(?:partie|part|pt|chapitre|chapter)[\s.:-]*#?\s*([0-9]{1,3}(?:\.[0-9]+)?)\b`)
	hashEpisode    = regexp.MustCompile(`#\s*([0-9]{1,3}(?:\.[0-9]+)?)\b`)
	wrappedEpisode = regexp.MustCompile(`[\[(]([0-9]{1,3}(?:\.[0-9]+)?)[\])](?:\s*(?:[-–—|｜•/:]|$))`)
	// "#1 in the world", "n°1 mondial": a ranking, not an episode.
	rankSuffix = regexp.MustCompile(`(?i)^\s*(?:in\s+(?:the\s+)?(?:world|us|uk|usa|france)\b|worldwide|globally|mondial|dans\s+le\s+monde|en\s+france|on\s+(?:steam|twitch|youtube|the\s+charts?)\b)`)
	// "Top 10": a list's length, never a series.
	topList = regexp.MustCompile(`^top [0-9]+$`)
	// A discovery qualifier — (découverte), (first look)… — names the pilot of the series rather
	// than a series of its own: the creator's first video of a game he then numbers.
	pilotQualifier = regexp.MustCompile(`(?i)^(.*?)\s*\((?:découverte|first\s+look|d[ée]mo|preview|playtest|early\s+access)\)\s*$`)
	// A colon continues with the episode's own title ("#100 : 100 minutes."),
	// not with a second number of a range.
	numberRange = regexp.MustCompile(`(?i)^\s*(?:[-–—/&+.,]|\bet\b|\band\b)\s*[0-9]`)
)

// Parser reads titles. It is built once, holds no state that changes and is safe for concurrent use.
type Parser struct {
	// reserved are the normalized names no series may carry: the platforms' own names, which creators
	// put in titles as hashtags or channel plugs.
	reserved map[string]bool
}

// NewParser returns a parser that refuses the given names (the display names of the platforms) as series
// names, on top of the generic words that name no series ("gameplay", "live"…).
func NewParser(reservedNames []string) *Parser {
	p := &Parser{reserved: make(map[string]bool, len(reservedNames))}
	for _, name := range reservedNames {
		if normalized := normalizedName(name); normalized != "" {
			p.reserved[normalized] = true
		}
	}
	return p
}

// Parse reads the series a title names: its name and, when the title has them, the episode and the
// season. ok is false when the title names no series.
func (p *Parser) Parse(title string) (Parsed, bool) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Parsed{}, false
	}
	candidate := title
	explicit := false
	if match := pairedSeries.FindStringSubmatch(title); match != nil {
		candidate, explicit = match[1], true
	} else if parts := splitPipes(title); len(parts) > 1 {
		candidate, explicit = p.pipeLabel(parts), true
	} else if parts := dashSeparator.Split(title, -1); len(parts) > 1 {
		// A numbered suffix after a spaced dash is an explicit series label when a name stands before
		// its marker ("Show - Series #12"); a marker alone ("Show - Episode 12") leaves the series in
		// the text before the dash, which the whole title yields.
		// Shell operators such as :|: are not title delimiters.
		suffix := strings.TrimSpace(parts[len(parts)-1])
		if rest, number, _ := p.Number(suffix); number != nil && trimName(rest) != "" {
			candidate, explicit = suffix, true
		}
	}
	if explicit && onlyTags(candidate) {
		return Parsed{}, false
	}
	candidate = stripNotes(candidate)
	name, episode, season := p.Number(candidate)
	if episode == nil && explicit {
		// Paired delimiters often put the number before/after the series label.
		_, episode, season = p.Number(title)
	}
	if !explicit && episode == nil && name == candidate {
		return Parsed{}, false
	}
	name = trimName(name)
	for trailingNote.MatchString(name) {
		name = strings.TrimSpace(trailingNote.ReplaceAllString(name, ""))
	}
	// A discovery qualifier names the first video of the series, not a series of its own:
	// "… | Balatro (découverte)" is the pilot of "Balatro". It keeps no episode number.
	if episode == nil && season == nil {
		if pilot := pilotQualifier.FindStringSubmatch(name); pilot != nil {
			name = trimName(pilot[1])
		}
	}
	name = trimName(name)
	if !p.validName(name) {
		return Parsed{}, false
	}
	return Parsed{Name: name, Episode: episode, Season: season}, true
}

// pipeLabel picks the segment of a pipe-separated title that names the series. A segment that carries
// an episode marker with a name before it names the series ("Outer Wilds #5 | The Next Planet"). A
// marker standing alone belongs to the segment before it ("Series | Episode 7"), or, when it opens the
// title, to the last segment ("Episode 12 | Series"). Without any marker the last segment is the series
// ("Title | Series").
func (p *Parser) pipeLabel(parts []string) string {
	last := len(parts) - 1
	for i := last; i >= 0; i-- {
		rest, _, _ := p.Number(parts[i])
		if rest == parts[i] {
			continue
		}
		if p.validName(trimName(rest)) {
			return strings.TrimSpace(parts[i])
		}
		// "Series | Season 2 | Episode 4": the marker is spread over the segments before it.
		for j := i - 1; j >= 0; j-- {
			joined := strings.Join(parts[j:i+1], " ")
			if rest, _, _ := p.Number(joined); p.validName(trimName(rest)) {
				return strings.TrimSpace(joined)
			}
		}
		break
	}
	return strings.TrimSpace(parts[last])
}

// stripNotes removes the bracketed tags that close a title ("[FR]", "[4K 60fps]") unless the tag is
// itself the episode marker ("[EP. 5]", "[5]").
func stripNotes(title string) string {
	for {
		note := trailingNote.FindString(title)
		if note == "" {
			return title
		}
		if rest, _, _ := readNumber(strings.TrimSpace(note)); rest != strings.TrimSpace(note) {
			return title
		}
		title = strings.TrimSpace(title[:len(title)-len(note)])
	}
}

// onlyTags reports whether a label is nothing but hashtags and !commands ("#shorts #fyp", "!don !sub").
func onlyTags(label string) bool {
	fields := strings.Fields(label)
	for _, field := range fields {
		if field[0] != '#' && field[0] != '!' {
			return false
		}
	}
	return len(fields) > 0
}

// splitPipes keeps compact title separators such as Title|Series, but not shell operators embedded in
// punctuation, such as :|:. Spaced and full-width pipes remain valid.
func splitPipes(title string) []string {
	var parts []string
	start := 0
	for i, r := range title {
		if r != '|' && r != '｜' {
			continue
		}
		before, _ := utf8.DecodeLastRuneInString(title[:i])
		after, _ := utf8.DecodeRuneInString(title[i+utf8.RuneLen(r):])
		if r == '|' && !unicode.IsSpace(before) && !unicode.IsSpace(after) &&
			!(unicode.IsLetter(before) || unicode.IsDigit(before)) {
			continue
		}
		if r == '|' && !unicode.IsSpace(before) && !unicode.IsSpace(after) &&
			!(unicode.IsLetter(after) || unicode.IsDigit(after)) {
			continue
		}
		parts = append(parts, title[start:i])
		start = i + utf8.RuneLen(r)
	}
	if len(parts) > 0 {
		parts = append(parts, title[start:])
	}
	return parts
}

// Number splits the explicit episode syntax off a title: what comes before it, the episode and the season.
// Only explicit syntax is numeric metadata; years, durations and bare trailing numbers are not. Wrapped
// numbers are restricted to three digits. A title with several markers or a range ("#1-2") has no number.
// Callers use it to tell whether a title already carries its episode number, so reading it from the
// thumbnail would be pointless.
func (p *Parser) Number(title string) (rest string, episode *float64, season *int) {
	body := stripNotes(title)
	if rest, episode, season = readNumber(body); rest == body {
		return title, nil, nil
	}
	return rest, episode, season
}

// readNumber is Number on a title whose closing tags are already stripped.
func readNumber(title string) (rest string, episode *float64, season *int) {
	for _, pattern := range []*regexp.Regexp{seasonCompact, seasonWords} {
		if matches := pattern.FindAllStringSubmatchIndex(title, -1); len(matches) > 0 {
			match := matches[0]
			seasonNumber, _ := strconv.Atoi(title[match[2]:match[3]])
			episodeNumber, _ := strconv.ParseFloat(title[match[4]:match[5]], 64)
			if seasonNumber > 0 && episodeNumber > 0 {
				if len(matches) > 1 || numberRange.MatchString(title[match[1]:]) {
					return title[:match[0]], nil, nil
				}
				return title[:match[0]], &episodeNumber, &seasonNumber
			}
		}
	}
	var earliest []int
	count := 0
	primaryMatches := [][]int{}
	for _, pattern := range []*regexp.Regexp{episodeMarker, hashEpisode, wrappedEpisode} {
		for _, match := range pattern.FindAllStringSubmatchIndex(title, -1) {
			if markerPlausible(title, match) {
				primaryMatches = append(primaryMatches, match)
			}
		}
	}
	matches := [][]int{}
	partMatches := partMarker.FindAllStringSubmatchIndex(title, -1)
	for _, part := range partMatches {
		containsPrimary := false
		for _, primary := range primaryMatches {
			if primary[0] >= part[0] && primary[1] <= part[1] {
				containsPrimary = true
				break
			}
		}
		// "PART #1" is one marker; a later "Partie 2" after #34 is not.
		if containsPrimary || len(primaryMatches) == 0 {
			matches = append(matches, part)
		}
	}
	for _, primary := range primaryMatches {
		insidePart := false
		for _, part := range partMatches {
			if primary[0] >= part[0] && primary[1] <= part[1] {
				insidePart = true
				break
			}
		}
		if !insidePart {
			matches = append(matches, primary)
		}
	}
	for _, match := range matches {
		count++
		if earliest == nil || match[0] < earliest[0] {
			earliest = match
		}
	}
	if earliest != nil {
		episodeNumber, _ := strconv.ParseFloat(title[earliest[2]:earliest[3]], 64)
		if episodeNumber > 0 {
			if count > 1 || numberRange.MatchString(title[earliest[1]:]) {
				return title[:earliest[0]], nil, nil
			}
			return title[:earliest[0]], &episodeNumber, nil
		}
	}
	return title, nil, nil
}

// markerPlausible rejects the matches that are not episodes: a ranking ("#1 in the world") and the
// number after a language's hash sign ("C# 12").
func markerPlausible(title string, match []int) bool {
	if rankSuffix.MatchString(title[match[1]:]) {
		return false
	}
	if title[match[0]] == '#' && match[0] > 0 && match[0]+1 < len(title) {
		before, _ := utf8.DecodeLastRuneInString(title[:match[0]])
		after, _ := utf8.DecodeRuneInString(title[match[0]+1:])
		return !(unicode.IsLetter(before) && unicode.IsSpace(after))
	}
	return true
}

// trimName strips the delimiters, hashtag signs and emoji that surround a series name. A trailing '#'
// stays: it belongs to a name such as "C#".
func trimName(name string) string {
	name = strings.TrimLeftFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("|｜-–—:;#([•/", r) || decoration(r)
	})
	return strings.TrimRightFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("|｜-–—:;([•/", r) || decoration(r)
	})
}

// decoration reports the symbols and emoji creators put around titles: they never belong to a name.
// Only emoji pieces are listed, not every combining mark, so accents of a name survive.
func decoration(r rune) bool {
	return unicode.In(r, unicode.So, unicode.Sk) || r == '\uFE0F' || r == '\u200D' || r == '\u20E3'
}

// normalizedName folds a series name to the letters and digits that identify it: case, punctuation,
// apostrophes and spacing do not tell two spellings apart.
func normalizedName(name string) string {
	var normalized strings.Builder
	space := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && normalized.Len() > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(r)
			space = false
		} else if r != '\'' && r != '’' && r != '‘' {
			space = true
		}
	}
	return normalized.String()
}

func (p *Parser) validName(name string) bool {
	normalized := normalizedName(name)
	if len([]rune(normalized)) < 3 {
		return false
	}
	letter := false
	for _, r := range normalized {
		if unicode.IsLetter(r) {
			letter = true
			break
		}
	}
	if !letter {
		return false
	}
	if p.reserved[normalized] || topList.MatchString(normalized) {
		return false
	}
	switch normalized {
	case "gameplay", "lets play", "let s play", "playthrough", "walkthrough", "episode", "épisode", "episodes", "épisodes", "ep", "live", "stream", "vod", "gaming", "fr", "vf", "vostfr", "français", "english", "full game", "part", "partie", "highlights", "best of", "subscribe", "abonnez vous", "shorts", "short", "clips", "clip", "trailer", "bande annonce", "4k", "hd", "60fps":
		return false
	}
	return true
}
