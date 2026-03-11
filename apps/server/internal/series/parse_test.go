package series

import (
	"strings"
	"testing"
)

// The tests below are the specification of title inference. They are written as a matrix of the forms
// French and English gaming and series channels really use — marker × position × delimiter × noise —
// plus the look-alikes that must never become a series. A title that a creator might write and that is
// not in a table is a gap in the specification, not a case the parser handles "obviously".

func number[T int | float64](n *T) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

// parseCase is what a title must parse to. episode 0 means the title carries no episode number
// (the title still names a series); season 0 means no season.
type parseCase struct {
	title   string
	name    string
	episode float64
	season  int
}

// themed groups cases under one comment-worthy heading, which becomes the subtest prefix.
type themed struct {
	theme string
	cases []parseCase
}

// noSeries is a title that must not name a series. carriesNumber is set when Number still finds an
// episode in it (a marker with no series name around it); every other negative must not carry one,
// because Number decides whether the thumbnail needs reading.
type noSeries struct {
	title         string
	carriesNumber bool
}

type themedNegatives struct {
	theme string
	cases []noSeries
}

const showName = "Outer Wilds"

// Delimiters creators put between the series label and the rest of the title.
var delimiters = []string{" - ", " – ", " — ", " | ", " ｜ ", " : ", " :: ", " • ", " / ", " "}

// marker is one way of writing "episode 5". bare is set for a number wrapped in brackets with no
// word: it is only a marker when a delimiter or the end of the title follows it.
type marker struct {
	text string
	bare bool
}

var episodeForms = []marker{
	// Episode words, French and English, with and without accents, case and dots.
	{"Episode 5", false}, {"Épisode 5", false}, {"ÉPISODE 5", false}, {"episode 5", false},
	{"Ep 5", false}, {"Ep. 5", false}, {"Ép. 5", false}, {"ép 5", false}, {"EP 5", false}, {"EP5", false}, {"Ep.5", false},
	{"Episode n°5", false}, {"épisode N° 5", false}, {"Episode 05", false}, {"Ep. 005", false},
	// Parts and chapters.
	{"Part 5", false}, {"PART 5", false}, {"Partie 5", false}, {"Part #5", false}, {"Pt 5", false}, {"Pt. 5", false}, {"PT5", false},
	{"Chapter 5", false}, {"Chapitre 5", false},
	// Numbers on their own.
	{"#5", false}, {"#05", false}, {"N°5", false}, {"n°5", false}, {"N° 5", false}, {"Nº5", false},
	// Wrapped markers.
	{"(5)", true}, {"[5]", true}, {"(05)", true}, {"[005]", true},
	{"(EP 5)", false}, {"[EP. 5]", false}, {"(ép. 5)", false}, {"(Episode 5)", false}, {"[Épisode 5]", false},
	{"[Part 5]", false}, {"(Partie 5)", false}, {"(Pt. 5)", false}, {"(#5)", false}, {"[#5]", false},
}

var seasonForms = []string{
	"S2E4", "S02E04", "s2e4", "S02 E04", "S2 E4", "S2.E4", "S02.E04", "S2 - E4", "S2 Ep 4", "S2 Ep. 4", "S2 Episode 4",
	"Saison 2 Épisode 4", "Saison 2 - Épisode 4", "Saison 2, Épisode 4", "Saison 2 : Épisode 4", "Saison 2 Ép. 4", "SAISON 2 ÉPISODE 4",
	"Season 2 Episode 4", "Season 2, Episode 4", "Season 2 - Episode 4", "Season 2 Ep 4", "Season 2 Ep. 4", "Season 2 | Episode 4",
	"(S2E4)", "[S02E04]",
}

// markerAfterName: "Outer Wilds <marker> <delimiter> The Next Planet" for every marker and delimiter.
func markerAfterName() []parseCase {
	var cases []parseCase
	for _, form := range episodeForms {
		for _, delimiter := range delimiters {
			if form.bare && delimiter == " " {
				continue // "Outer Wilds (5) The Next Planet": a bare wrapped number is not followed by a delimiter
			}
			cases = append(cases, parseCase{showName + " " + form.text + delimiter + "The Next Planet", showName, 5, 0})
		}
	}
	return cases
}

// markerAfterDelimiter: "Outer Wilds <delimiter> <marker>" — the marker closes the title.
func markerAfterDelimiter() []parseCase {
	var cases []parseCase
	for _, form := range episodeForms {
		for _, delimiter := range delimiters {
			cases = append(cases, parseCase{showName + delimiter + form.text, showName, 5, 0})
		}
	}
	return cases
}

// markerBeforeName: "<marker> | Outer Wilds". Only a pipe says the rest of the title is the series;
// after any other delimiter the rest is the episode's own title (see negatives).
func markerBeforeName() []parseCase {
	var cases []parseCase
	for _, form := range episodeForms {
		for _, delimiter := range []string{" | ", " ｜ "} {
			cases = append(cases, parseCase{form.text + delimiter + showName, showName, 5, 0})
		}
	}
	return cases
}

func seasonMatrix() []parseCase {
	var cases []parseCase
	for _, form := range seasonForms {
		cases = append(cases, parseCase{showName + " " + form, showName, 4, 2}, parseCase{form + " | " + showName, showName, 4, 2})
		for _, delimiter := range delimiters {
			if strings.TrimSpace(delimiter) == "" {
				continue
			}
			cases = append(cases,
				parseCase{showName + " " + form + delimiter + "The Next Planet", showName, 4, 2},
				parseCase{showName + delimiter + form, showName, 4, 2})
		}
	}
	return cases
}

// Noise creators add around the numbered title: it must neither hide the episode nor leak into the name.
var trailingNoise = []string{
	" 🔥", " 😱🔥", " ❤️", " [FR]", " [4K]", " [4K 60fps]", " (Let's Play)", " !don", " !don !sub", " !discord",
	" #shorts", " #gaming #fr", " 2024", " (2024)", " 12:30", " 1080p", " v1.2", " 3-0", " 5€", " $5", " Top 10",
}

var leadingNoise = []string{"🔥 ", "🎮 ", "🔴 ", "🔥🎮 ", "⚠️ "}

func noiseMatrix() []parseCase {
	var cases []parseCase
	for _, form := range []string{"#5", "Ep. 5", "Épisode 5", "Part 5", "N°5", "(5)", "[EP. 5]", "(ép. 5)"} {
		for _, noise := range trailingNoise {
			cases = append(cases, parseCase{showName + " " + form + " - The Next Planet" + noise, showName, 5, 0})
		}
		for _, noise := range leadingNoise {
			cases = append(cases, parseCase{noise + showName + " " + form + " - The Next Planet", showName, 5, 0})
		}
		for _, tag := range []string{" [FR]", " [4K]", " (Let's Play)", " 🔥", " !don", " #shorts"} {
			if form == "(5)" && !strings.Contains(tag, "[") {
				continue // a bare wrapped number needs a delimiter after it, a tag is not one
			}
			cases = append(cases, parseCase{showName + " " + form + tag, showName, 5, 0})
		}
	}
	return cases
}

var parseMatrix = []themed{
	{"marker after the name", markerAfterName()},
	{"marker after a delimiter", markerAfterDelimiter()},
	{"marker before the name", markerBeforeName()},
	{"season and episode", seasonMatrix()},
	{"noise around a numbered title", noiseMatrix()},
	{"real titles", []parseCase{
		// French YouTube.
		{"Minecraft Hardcore - Saison 3 - Épisode 12 : Le Nether", "Minecraft Hardcore", 12, 3},
		{"Baldur's Gate 3 | Épisode 14 | Le camp des gobelins", "Baldur's Gate 3", 14, 0},
		{"Stardew Valley #112 - Année 3 - Printemps", "Stardew Valley", 112, 0},
		{"Zelda Breath of the Wild | EP 12 | Les 4 bêtes", "Zelda Breath of the Wild", 12, 0},
		{"Hollow Knight (Partie 20) | Boss final ! 🔥", "Hollow Knight", 20, 0},
		{"LA SUITE DE L'AVENTURE ! - Dark Souls 3 Ép. 07 !don", "Dark Souls 3", 7, 0},
		{"Fallout 4 #12 : Nuka World", "Fallout 4", 12, 0},
		{"Cyberpunk 2077 Ep. 5 - Les Aldecaldos", "Cyberpunk 2077", 5, 0},
		{"Half-Life: Alyx #3", "Half-Life: Alyx", 3, 0},
		{"Metal Gear Solid V: The Phantom Pain Ep 4", "Metal Gear Solid V: The Phantom Pain", 4, 0},
		// English YouTube.
		{"Let's Play Skyrim - Part 12 - The Dark Brotherhood", "Let's Play Skyrim", 12, 0},
		{"Elden Ring Walkthrough Part 5 | Radahn", "Elden Ring Walkthrough", 5, 0},
		{"Resident Evil 4 Remake - Chapter 5 Walkthrough", "Resident Evil 4 Remake", 5, 0},
		{"Minecraft 1.20 Survival Let's Play Ep 8", "Minecraft 1.20 Survival Let's Play", 8, 0},
		{"The Last of Us Part II #3", "The Last of Us Part II", 3, 0},
		{"The Witcher 3 100% Walkthrough Ep.34 [4K 60fps]", "The Witcher 3 100% Walkthrough", 34, 0},
		{"Dark Souls 3 (Part 12) [Blind]", "Dark Souls 3", 12, 0},
		{"Programming in C# Ep. 5", "Programming in C#", 5, 0},
		{"C# 12 Tutorial #3", "C# 12 Tutorial", 3, 0},
		// Twitch VODs.
		{"🔴 LIVE | Elden Ring Blind Run #12 | !sub !discord", "Elden Ring Blind Run", 12, 0},
		{"Elden Ring Blind Run #13 🔴 !sub #fr", "Elden Ring Blind Run", 13, 0},
		// A series with no number: the title says only which series it belongs to.
		{"RENDEZ-VOUS AVEC LA MORT | Castlevania: Belmont's Curse (07)", "Castlevania: Belmont's Curse", 7, 0},
		{"RENDEZ-VOUS|Castlevania (07)", "Castlevania", 7, 0},
		{"RENDEZ-VOUS｜Castlevania (07)", "Castlevania", 7, 0},
		{"Boss fight | Hollow Knight", "Hollow Knight", 0, 0},
		{"BOSS FIGHT ｜ Hollow Knight", "Hollow Knight", 0, 0},
		{"VACANCES DE RÊVE EN TERRITOIRE ZOMBIE !! -Quarantine Zone: Miami Zero- [NOUVELLE ZONE]", "Quarantine Zone: Miami Zero", 0, 0},
		{"T'INQUIÈTES PAS, ÇA VA BIEN SE PASSER !! -Quarantine Zone: Miami Zero- [ÇA SE PASSE MAL]", "Quarantine Zone: Miami Zero", 0, 0},
		{"A journey -Half-Life: Alyx- [VR]", "Half-Life: Alyx", 0, 0},
		{"Boss fight 🔥 | Hollow Knight 🎮", "Hollow Knight", 0, 0},
		// Regressions found in production.
		{"Running a fork-bomb on Linux :(){ :|: & };: - You Suck at Programming #016", "You Suck at Programming", 16, 0},
		{"Stepping through and debugging a fork-bomb `:(){ :|: & };:` - You Suck at Programming #017", "You Suck at Programming", 17, 0},
		{"Baldur's Gate (ép. 26) - Convergence des streams !don", "Baldur's Gate", 26, 0},
		{"Baldur's Gate (ép. 27) - La suite", "Baldur's Gate", 27, 0},
		{"FAKER [EP. 1] : The rise of a League of Legends genius.", "FAKER", 1, 0},
		{"FAKER [EP. 2] : The Generation of Miracles.", "FAKER", 2, 0},
		{"Outer Wilds (015) [FR]", "Outer Wilds", 15, 0},
		{"Outer Wilds [VR] #16", "Outer Wilds", 16, 0},
		{"Best of ZeratoR #100 : 100 minutes.", "Best of ZeratoR", 100, 0},
		{"Best of ZeratoR #100: 100 minutes.", "Best of ZeratoR", 100, 0},
		{"Trackmania Cup 2019 #34 : Soirée d'entrainement! Partie2", "Trackmania Cup 2019", 34, 0},
		{"Scrap Mechanic - Tank partie 1.5 - Présentation des prototypes", "Scrap Mechanic - Tank", 1.5, 0},
		{"Terraria Challenger [09] - RIP Lunatic", "Terraria Challenger", 9, 0},
		{"200 BOOSTER PART #1", "200 BOOSTER", 1, 0},
		{"200 BOOSTER PART #2", "200 BOOSTER", 2, 0},
		{"200 BOOSTER PARTIE 3", "200 BOOSTER", 3, 0},
	}},
	{"decimal episodes", []parseCase{
		{showName + " Ep 1.5", showName, 1.5, 0},
		{showName + " #2.5 - Bonus", showName, 2.5, 0},
		{showName + " Part 1.5", showName, 1.5, 0},
	}},
	{"ambiguous: a numbered suffix after a spaced dash is the series label", []parseCase{
		// "Game - Series #12" and "Series - Subtitle Ep 12" are the same shape: the numbered part wins,
		// as for "Running a fork-bomb on Linux - You Suck at Programming #016".
		{"The Witcher 3 - 100% Walkthrough Ep.34", "100% Walkthrough", 34, 0},
		{"Boss fight - Hollow Knight #5", "Hollow Knight", 5, 0},
	}},
	{"deliberately unsupported: the series is kept, the number is not", []parseCase{
		// A range or a fraction is several episodes or a total, not one episode: the title still names
		// the series but carries no number, so the thumbnail is read.
		{"200 BOOSTER PART #1-2", "200 BOOSTER", 0, 0},
		{showName + " #1-2", showName, 0, 0},
		{showName + " #1 - 2", showName, 0, 0},
		{showName + " #1 & Episode 2", showName, 0, 0},
		{showName + " Ep 1 et 2", showName, 0, 0},
		{showName + " Ep 1 and 2", showName, 0, 0},
		{showName + " Ep 3/10", showName, 0, 0},
		{showName + " Épisode 1+2", showName, 0, 0},
		{showName + " #5 & #6", showName, 0, 0},
	}},
}

// parseNegatives are titles that must not name a series.
var parseNegatives = []themedNegatives{
	{"years, dates and times", []noSeries{
		{"My holiday in 2026", false}, {"Travel (2024)", false}, {"A video (12:30)", false}, {"A video | 2026", false},
		{"Outer Wilds 2024", false}, {"Outer Wilds - 2024", false}, {"Outer Wilds [2024]", false}, {"Outer Wilds (2025) - Review", false},
		{"Stream du 12/05/2024", false}, {"Live à 20:30 - Elden Ring", false}, {"Elden Ring [20:30]", false}, {"Elden Ring (20h30)", false},
		{"Outer Wilds | 2024", false}, {"Outer Wilds #2024", false}, {"Outer Wilds (1999)", false},
	}},
	{"prices", []noSeries{
		{"Outer Wilds à 5€", false}, {"Outer Wilds (5€)", false}, {"Outer Wilds [5$]", false}, {"I bought a $5 game", false},
		{"Outer Wilds ($5)", false}, {"Les jeux à 5€ sur Steam", false}, {"Outer Wilds (5 €)", false},
	}},
	{"versions and resolutions", []noSeries{
		{"Minecraft 1.20.4 Update", false}, {"Patch v1.2 notes", false}, {"Outer Wilds v1.2", false}, {"Outer Wilds (v1.2)", false},
		{"Update 1.5", false}, {"Outer Wilds [v1.2]", false}, {"Elden Ring - Patch 1.10", false}, {"Version 2.0 is here", false},
		{"Outer Wilds 1080p", false}, {"Outer Wilds [4K]", false}, {"Elden Ring 4K 60fps", false}, {"Outer Wilds (1080p)", false},
		{"Elden Ring #4k", false}, {"Elden Ring #1080p", false},
	}},
	{"scores", []noSeries{
		{"PSG 3-0 Marseille", false}, {"PSG 3 - 0 Marseille", false}, {"Final score: 3-0", false}, {"Real Madrid 2-1 Barcelona | Highlights", false},
		{"Won 3-0 [FR]", false}, {"Team A 16-14 Team B", false}, {"Match 3-0 (BO5)", false},
	}},
	{"lists and rankings", []noSeries{
		{"Top 10 Games of 2024", false}, {"TOP 5 des jeux 2024", false}, {"10 jeux à faire en 2024", false}, {"Les 10 meilleurs jeux (2024)", false},
		{"Top 10 | Highlights", false}, {"Best games of 2024 | Top 10", false}, {"Top 100 Elden Ring moments", false},
		{"#1 in the world", false}, {"He is ranked #1 in the world", false}, {"Best player #1 in the world", false}, {"The #1 worldwide on Steam", false},
		{"Le joueur n°1 mondial", false}, {"N°1 mondial", false}, {"Le jeu #1 dans le monde", false}, {"Ils sont #1 en France", false},
		{"Elden Ring is #1 on Steam", false},
	}},
	{"hashtags", []noSeries{
		{"Funny moment #shorts", false}, {"Funny moment #gaming #fyp", false}, {"Funny moment | #shorts", false}, {"Funny moment | #gaming #fyp", false},
		{"Funny moment | #shorts #fyp", false}, {"Funny moment !don !sub", false}, {"Funny moment | !don !sub", false},
		{"Epic clutch #1v1", false}, {"Epic clutch #100DaysOfCode", false}, {"Funny moment #2024", false},
		{"Learn C# 12 in one hour", false}, {"What is new in C# 12", false},
	}},
	{"a lone number", []noSeries{
		{"A game 7", false}, {"Fallout 4", false}, {"Elden Ring 2", false}, {"Resident Evil 4 Remake", false}, {"5 Nights at Freddy's", false},
		{"Outer Wilds - 5", false}, {"Minecraft but 1 block", false}, {"I played 100 hours of Outer Wilds", false}, {"Outer Wilds (5 stars)", false},
		{"I survived 100 days in Minecraft", false}, {"Outer Wilds | 5", false}, {"Outer Wilds - 5 -", false},
	}},
	{"no delimiter, no marker", []noSeries{
		{"Outer Wilds", false}, {"", false}, {"   ", false}, {"🔥🔥🔥", false}, {"A video | GAMEPLAY", false}, {"A video | [NOUVELLE ZONE]", false},
		{"Outer Wilds | Gameplay", false}, {"Outer Wilds | Let's Play", false}, {"Outer Wilds | FR", false}, {"Outer Wilds | 4K", false},
		{"Outer Wilds | Shorts", false}, {"Outer Wilds | VOSTFR", false},
	}},
	{"deliberately unsupported: a delimiter other than a pipe or paired dashes names no series", []noSeries{
		// "Boss fight - Hollow Knight" and "Hollow Knight : Boss fight" are equally titles of one video,
		// and nothing tells which side is the game.
		{"Boss fight - Hollow Knight", false}, {"Boss fight – Hollow Knight", false}, {"Boss fight — Hollow Knight", false},
		{"Hollow Knight : Boss fight", false}, {"Hollow Knight :: Boss fight", false}, {"Hollow Knight • Boss fight", false},
		{"Hollow Knight / Boss fight", false}, {"Hollow Knight: Silksong", false},
	}},
	{"deliberately unsupported: a marker with no series name beside it", []noSeries{
		{"Episode 7", true}, {"Episode 7 | Gameplay", true}, {"#7", true}, {"Ep. 12", true}, {"(05)", true}, {"S02E05", true},
		// Before a delimiter other than a pipe the rest is the episode's own title, not the series.
		{"Episode 5 - The Next Planet", true}, {"Ep. 5 : The Next Planet", true}, {"#5 - Outer Wilds", true},
		{"#5 • Outer Wilds", true}, {"Part 5 / Outer Wilds", true}, {"Épisode 5 : Outer Wilds", true},
		{"[Ep. 5] Outer Wilds", true},
	}},
	{"deliberately unsupported: markers too ambiguous to number a series", []noSeries{
		// "Day 1" is a launch day and one day holds several videos; "Live 8" is a concert; a bare
		// "E5" is the E3 expo, E85 fuel, vitamin E; a bare number between dashes is a score or a list.
		{"Minecraft Hardcore - Day 5", false}, {"Minecraft Hardcore - Jour 5", false}, {"Palworld Day 1 gameplay", false},
		{"Elden Ring Live 12", false}, {"Outer Wilds E5", false}, {"Outer Wilds E12", false},
		{"Outer Wilds - 5 - The Next Planet", false}, {"Real - 3 - 1 - Barcelona", false},
		// Ratings and fractions look like episodes of a season.
		{"Outer Wilds « 3/10 »", false}, {"Outer Wilds [3/10]", false}, {"Starfield review: 7/10", false}, {"Starfield - 7/10 - honest review", false},
		// Spelled-out and roman numbers are part of game names ("Part II", "Chapter One").
		{"Outer Wilds Part Two", false}, {"Outer Wilds Episode II", false}, {"The Last of Us Part Two Review", false},
		// A season with no episode is not a number.
		{"Outer Wilds Season 2", false}, {"Outer Wilds Saison 2", false},
	}},
}

func TestParseMatrix(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	for _, group := range parseMatrix {
		for _, test := range group.cases {
			t.Run(group.theme+"/"+test.title, func(t *testing.T) {
				t.Parallel()
				parsed, ok := p.Parse(test.title)
				if !ok || parsed.Name != test.name || float64Value(parsed.Episode) != test.episode || number(parsed.Season) != test.season {
					t.Fatalf("parsed=%+v episode=%v season=%d ok=%v, want name %q episode %v season %d",
						parsed, float64Value(parsed.Episode), number(parsed.Season), ok, test.name, test.episode, test.season)
				}
			})
		}
	}
}

func TestParseNegatives(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	for _, group := range parseNegatives {
		for _, test := range group.cases {
			t.Run(group.theme+"/"+test.title, func(t *testing.T) {
				t.Parallel()
				if parsed, ok := p.Parse(test.title); ok {
					t.Fatalf("inferred %+v", parsed)
				}
			})
		}
	}
}

// Number decides whether OCR must read the thumbnail, so it must find the number Parse found: a title
// whose episode Parse reads must not be sent to OCR, and a title with none must be.
func TestNumberAgreesWithParse(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	for _, group := range parseMatrix {
		for _, test := range group.cases {
			t.Run(group.theme+"/"+test.title, func(t *testing.T) {
				t.Parallel()
				_, episode, season := p.Number(test.title)
				if float64Value(episode) != test.episode || number(season) != test.season {
					t.Fatalf("Number found episode %v season %d, Parse reads episode %v season %d", float64Value(episode), number(season), test.episode, test.season)
				}
			})
		}
	}
	for _, group := range parseNegatives {
		for _, test := range group.cases {
			t.Run(group.theme+"/"+test.title, func(t *testing.T) {
				t.Parallel()
				if _, episode, season := p.Number(test.title); (episode != nil || season != nil) != test.carriesNumber {
					t.Fatalf("Number found episode %v season %v, carriesNumber=%v", episode, season, test.carriesNumber)
				}
			})
		}
	}
}

func float64Value(n *float64) float64 {
	if n == nil {
		return 0
	}
	return *n
}

func TestParseReservedNames(t *testing.T) {
	t.Parallel()
	title := "A video | Alpha Tube"
	if _, ok := NewParser(nil).Parse(title); !ok {
		t.Fatal("without reserved names the series is accepted")
	}
	// The platform names the app passes in are not series: creators sign titles with them.
	if parsed, ok := NewParser([]string{"Alpha-Tube"}).Parse(title); ok {
		t.Fatalf("a platform's own name became a series: %+v", parsed)
	}
}

func TestParseDiscoveryQualifierNamesThePilot(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	for title, name := range map[string]string{
		"POKER + ROGUELITE = OUI | Balatro (découverte)":  "Balatro",
		"POKER + ROGUELITE = YES | Balatro (first look)":  "Balatro",
		"ON Y JOUE EN AVANT-PREMIÈRE | Balatro (preview)": "Balatro",
		"LE BALATRO DU SCRABBLE | Wordatro!":              "Wordatro!",
		"UNE BÊTA TORDUE | Balatro (beta)":                "Balatro (beta)",
	} {
		parsed, ok := p.Parse(title)
		if !ok || parsed.Name != name || parsed.Episode != nil {
			t.Fatalf("%q -> %+v %v, want name %q and no episode", title, parsed, ok, name)
		}
	}
}

// Number returns what comes before the marker as well as the numbers.
func TestNumberSplitsTheTitleAtItsMarker(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	for _, test := range []struct {
		title   string
		rest    string
		episode int
		season  int
	}{
		{"Outer Wilds #2", "Outer Wilds ", 2, 0},
		{"Show S02E05 - x", "Show", 5, 2},
		{"Just a title", "Just a title", 0, 0},
		{"Outer Wilds #1 & Episode 2", "Outer Wilds ", 0, 0},
		{"Travel (2024)", "Travel (2024)", 0, 0},
	} {
		rest, episode, season := p.Number(test.title)
		if rest != test.rest || number(episode) != test.episode || number(season) != test.season {
			t.Errorf("%q -> %q %v %v", test.title, rest, episode, season)
		}
	}
}
