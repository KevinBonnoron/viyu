package series

import (
	"reflect"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// channelVideos completes the videos of a channel the way a stored catalog presents them.
func channelVideos(channel string, videos ...platform.Video) []platform.Video {
	out := make([]platform.Video, len(videos))
	copy(out, videos)
	for i := range out {
		if out[i].Platform == "" {
			out[i].Platform = "youtube"
		}
		if out[i].ChannelID == "" {
			out[i].ChannelID = channel
		}
		out[i].ChannelName = channel
	}
	return out
}

func ids(s Series) []string {
	out := make([]string, 0, len(s.Items))
	for _, item := range s.Items {
		out = append(out, item.Video.ID)
	}
	return out
}

func infer(videos ...platform.Video) []Series {
	return NewParser(nil).Infer(videos, nil)
}

func TestInferDecimalAndBracketedEpisodesOrder(t *testing.T) {
	t.Parallel()
	list := infer(channelVideos("UCtank",
		platform.Video{ID: "3", Title: "Scrap Mechanic - Tank partie 3 - Blindage", Published: "2021-04-24"},
		platform.Video{ID: "2", Title: "Scrap Mechanic - Tank partie 2 - Optimisation", Published: "2021-04-15"},
		platform.Video{ID: "1.5", Title: "Scrap Mechanic - Tank partie 1.5 - Présentation", Published: "2021-04-13"},
		platform.Video{ID: "1", Title: "Scrap Mechanic - Tank partie 1 - Les commandes", Published: "2021-04-09"},
	)...)
	if len(list) != 1 || !reflect.DeepEqual(ids(list[0]), []string{"1", "1.5", "2", "3"}) {
		t.Fatalf("decimal episodes not ordered: %+v", list)
	}
	brackets := infer(channelVideos("UCterraria",
		platform.Video{ID: "9", Title: "Terraria Challenger [09] - RIP Lunatic"},
		platform.Video{ID: "10", Title: "Terraria Challenger [10] - Suite"},
	)...)
	if len(brackets) != 1 || brackets[0].Title != "Terraria Challenger" || !reflect.DeepEqual(ids(brackets[0]), []string{"9", "10"}) {
		t.Fatalf("bracketed series not discovered: %+v", brackets)
	}
}

func TestInferOrderingAndMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, order string
		videos      []platform.Video
		want        []string
	}{
		{"numbered", "episode", []platform.Video{{ID: "ten", Title: "Outer Wilds #10", Published: "2026-01-01"}, {ID: "two", Title: "Outer Wilds Ep. 2", Published: "2026-01-03"}}, []string{"two", "ten"}},
		{"seasons", "episode", []platform.Video{{ID: "b", Title: "Outer Wilds S02E01"}, {ID: "a", Title: "Outer Wilds S01E10"}}, []string{"a", "b"}},
		{"dates and unknown", "publication", []platform.Video{{ID: "unknown-z", Title: "Third | Outer Wilds", Published: "not a date"}, {ID: "late", Title: "Second | Outer Wilds", Published: "2026-02-01"}, {ID: "unknown-a", Title: "Fourth | Outer Wilds"}, {ID: "early", Title: "First | Outer Wilds", Published: "2026-01-01T12:00:00Z"}}, []string{"early", "late", "unknown-a", "unknown-z"}},
		{"partial numbering", "publication", []platform.Video{{ID: "late", Title: "Outer Wilds #1", Published: "2026-02-01"}, {ID: "early", Title: "First | Outer Wilds", Published: "2026-01-01"}}, []string{"late", "early"}},
		{"duplicate episodes", "publication", []platform.Video{{ID: "late", Title: "Outer Wilds #1", Published: "2026-02-01"}, {ID: "early", Title: "Outer Wilds Ep 1", Published: "2026-01-01"}}, []string{"early", "late"}},
		{"partial seasons", "publication", []platform.Video{{ID: "late", Title: "Outer Wilds S01E02", Published: "2026-02-01"}, {ID: "early", Title: "Outer Wilds #3", Published: "2026-01-01"}}, []string{"late", "early"}},
		{"episode range", "publication", []platform.Video{{ID: "late", Title: "Outer Wilds #1-2", Published: "2026-02-01"}, {ID: "early", Title: "Outer Wilds #3", Published: "2026-01-01"}}, []string{"early", "late"}},
		{"multiple episodes", "publication", []platform.Video{{ID: "late", Title: "Outer Wilds #1 & Episode 2", Published: "2026-02-01"}, {ID: "early", Title: "Outer Wilds #3", Published: "2026-01-01"}}, []string{"early", "late"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			list := infer(channelVideos("UCtest", test.videos...)...)
			if len(list) != 1 {
				t.Fatalf("series=%+v", list)
			}
			if list[0].Order != test.order || !reflect.DeepEqual(ids(list[0]), test.want) {
				t.Fatalf("series=%+v ids=%v", list[0], ids(list[0]))
			}
			for _, item := range list[0].Items {
				for _, original := range test.videos {
					if item.Video.ID == original.ID && item.Video.Published != original.Published {
						t.Fatalf("invented date: %+v", item)
					}
				}
			}
		})
	}
}

func TestInferSortsByLatestKnownPublication(t *testing.T) {
	t.Parallel()
	list := infer(channelVideos("UCtest",
		platform.Video{ID: "a1", Title: "Older #1", Published: "2024-01-01"},
		platform.Video{ID: "a2", Title: "Older #2", Published: "2024-02-01"},
		platform.Video{ID: "b1", Title: "Newest #1", Published: "2026-04-01T10:00:00+02:00", Thumbnail: "first"},
		platform.Video{ID: "b2", Title: "Newest #2", Published: "2025-01-01", Thumbnail: "second"},
		platform.Video{ID: "c1", Title: "Unknown #1", Published: "invalid"},
		platform.Video{ID: "c2", Title: "Unknown #2"},
	)...)
	if len(list) != 3 || list[0].Title != "Newest" || list[1].Title != "Older" || list[2].Title != "Unknown" {
		t.Fatalf("unexpected series order: %+v", list)
	}
	if list[0].LatestPublishedAt != "2026-04-01T08:00:00Z" || list[2].LatestPublishedAt != "" || list[0].Thumbnail != "first" || list[0].VideoCount != 2 {
		t.Fatalf("unexpected publication or thumbnail: %+v", list)
	}
}

func TestInferDeduplicatesKeepsStableIDsAndIsolatesCreators(t *testing.T) {
	t.Parallel()
	first := channelVideos("UCfirst", platform.Video{ID: "one", Title: "Outer Wilds #1"}, platform.Video{ID: "two", Title: "Outer Wilds #2"}, platform.Video{ID: "two", Title: "Outer Wilds #2"}, platform.Video{ID: "lonely", Title: "Other Game #1"})
	list := infer(first...)
	if len(list) != 1 || list[0].VideoCount != 2 {
		t.Fatalf("series=%+v", list)
	}
	id := list[0].ID
	grown := append(first, channelVideos("UCfirst", platform.Video{ID: "three", Title: "OUTER WILDS ep 3"})...)
	second := channelVideos("UCsecond", platform.Video{ID: "one", Title: "Outer Wilds #1"}, platform.Video{ID: "two", Title: "Outer Wilds #2"})
	updated := infer(append(grown, second...)...)
	if len(updated) != 2 {
		t.Fatalf("series=%+v", updated)
	}
	for _, s := range updated {
		if s.ChannelID == "UCfirst" && (s.ID != id || s.VideoCount != 3) {
			t.Fatalf("unstable series=%+v", s)
		}
		if s.ChannelID == "UCsecond" && s.ID == id {
			t.Fatal("creator collision")
		}
	}
	if groupID("youtube", "UCfirst", "Outer Wilds") == groupID("twitch", "UCfirst", "Outer Wilds") {
		t.Fatal("platform collision")
	}
	if groupID("youtube", "UCfirst", "Outer Wilds") != groupID("youtube", "UCfirst", "OUTER   wilds") {
		t.Fatal("spelling changed the identity")
	}
}

func TestInferUsesVerifiedOriginalTitlesAndRejectsPrivateSources(t *testing.T) {
	t.Parallel()
	list := infer(channelVideos("UCpublic",
		platform.Video{ID: "one", Title: "Translated title", OriginalTitle: "Quarantine Zone #1", OriginalTitleVerified: true},
		platform.Video{ID: "two", Title: "Other translation", OriginalTitle: "Quarantine Zone #2", OriginalTitleVerified: true},
		platform.Video{ID: "private", Title: "Quarantine Zone #3", Source: "connected"},
		platform.Video{ID: "unverified", Title: "Unrelated", OriginalTitle: "Quarantine Zone #5"},
	)...)
	if len(list) != 1 || list[0].Title != "Quarantine Zone" || list[0].VideoCount != 2 {
		t.Fatalf("series=%+v", list)
	}
	if list[0].Items[0].Video.Title != "Translated title" || list[0].Items[0].Video.OriginalTitle != "Quarantine Zone #1" {
		t.Fatal("rewrote canonical video metadata")
	}
}

func TestInferReconcilesOriginalTitlesAndIsolatesChannels(t *testing.T) {
	t.Parallel()
	videos := append(channelVideos("UCfirst",
		platform.Video{ID: "one", Title: "Show | AGE OF EMPIRES IV AT THE GRAND REX #1", OriginalTitle: "Show | AGE OF EMPIRES IV AU GRAND REX #1", OriginalTitleVerified: true},
		platform.Video{ID: "two", Title: "Show | AGE OF EMPIRES IV AU GRAND REX #2", OriginalTitle: "Show | AGE OF EMPIRES IV AU GRAND REX #2", OriginalTitleVerified: true},
		platform.Video{ID: "other1", Title: "Show | AGE OF EMPIRES IV AU GRAND REX 2025 #1"},
		platform.Video{ID: "other2", Title: "Show | AGE OF EMPIRES IV AU GRAND REX 2025 #2"},
		platform.Video{ID: "private", Title: "Show | AGE OF EMPIRES IV AU GRAND REX #3", Source: "connected"},
		platform.Video{ID: "foreign", Title: "Show | AGE OF EMPIRES IV AU GRAND REX #4", ChannelID: "UCforeign"},
	), channelVideos("UCsecond", platform.Video{ID: "s1", Title: "Show | AGE OF EMPIRES IV AU GRAND REX #1"}, platform.Video{ID: "s2", Title: "Show | AGE OF EMPIRES IV AU GRAND REX #2"})...)
	list := infer(videos...)
	// The lonely foreign video forms no series; the others never merge across spellings or channels.
	if len(list) != 3 {
		t.Fatalf("unexpected series: %+v", list)
	}
	for _, s := range list {
		if s.VideoCount != 2 {
			t.Fatalf("merged unrelated or private videos: %+v", s)
		}
		if s.ID != groupID("youtube", s.ChannelID, s.Title) {
			t.Fatalf("unstable identity: %+v", s)
		}
	}
	if list[0].ID == list[1].ID || list[1].ID == list[2].ID {
		t.Fatal("series or creators share an identity")
	}
}

func TestInferOrdersKnownEpisodesDespiteDuplicatesAndUnknownDates(t *testing.T) {
	t.Parallel()
	list := infer(channelVideos("UCindiemanche",
		platform.Video{ID: "39", Title: "VOIN et Miglorn - Indiemanche #39"},
		platform.Video{ID: "27", Title: "Chasing the Unseen - Indiemanche #27"},
		platform.Video{ID: "30b", Title: "Shadow of the Depth - Indiemanche #30", Published: "2024-05-10"},
		platform.Video{ID: "20", Title: "Death Roads: Tournament - Indiemanche #20", Published: "2023-11-22"},
		platform.Video{ID: "30a", Title: "Another Crab's Treasure - Indiemanche #30", Published: "2024-05-08"},
		platform.Video{ID: "21", Title: "Death Must Die - Indiemanche #21", Published: "2023-12-13"},
	)...)
	if len(list) != 1 {
		t.Fatalf("series=%+v", list)
	}
	if got, want := ids(list[0]), []string{"20", "21", "27", "30a", "30b", "39"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("known episode order=%v want %v", got, want)
	}
}

func TestInferUnnumberedSeriesGroupsChronologically(t *testing.T) {
	t.Parallel()
	list := infer(channelVideos("UCcreator",
		platform.Video{ID: "later", Title: "T'INQUIÈTES PAS, ÇA VA BIEN SE PASSER !! -Quarantine Zone: Miami Zero- [ÇA SE PASSE MAL]", Published: "2026-04-02"},
		platform.Video{ID: "earlier", Title: "VACANCES DE RÊVE EN TERRITOIRE ZOMBIE !! -Quarantine Zone: Miami Zero- [NOUVELLE ZONE]", Published: "2026-04-01"},
	)...)
	if len(list) != 1 || list[0].Title != "Quarantine Zone: Miami Zero" || list[0].Order != "publication" || !reflect.DeepEqual(ids(list[0]), []string{"earlier", "later"}) {
		t.Fatalf("series=%+v", list)
	}
	for _, item := range list[0].Items {
		if item.Episode != nil || item.Season != nil {
			t.Fatalf("invented metadata=%+v", item)
		}
	}
}

func TestEpisodesOnlyFillMissingTrustedTitleNumbers(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	videos := channelVideos("UCcreator",
		platform.Video{ID: "title", Title: "Outer Wilds #2", Published: "2026-01-01"},
		platform.Video{ID: "thumbnail", Title: "First | Outer Wilds", Published: "2026-01-02"},
	)
	// Through Infer…
	list := p.Infer(videos, map[string]int{"youtube:title": 99, "youtube:thumbnail": 1})
	// …and onto a series that was already inferred.
	later := p.Infer(videos, nil)
	p.WithEpisodes(later, map[string]int{"youtube:title": 99, "youtube:thumbnail": 1})
	for _, got := range [][]Series{list, later} {
		if got[0].Order != "episode" || !reflect.DeepEqual(ids(got[0]), []string{"thumbnail", "title"}) || number(got[0].Items[1].Episode) != 2 {
			t.Fatalf("series=%+v", got[0])
		}
	}
	unread := p.Infer(channelVideos("UCcreator", platform.Video{ID: "one", Title: "First | Outer Wilds"}, platform.Video{ID: "two", Title: "Second | Outer Wilds"}), nil)
	p.WithEpisodes(unread, map[string]int{"youtube:one": 0, "youtube:two": -1})
	if unread[0].Order != "publication" || unread[0].Items[0].Episode != nil || unread[0].Items[1].Episode != nil {
		t.Fatalf("untrusted OCR became metadata=%+v", unread[0])
	}
}

func TestThumbnailCandidatesAreTheVideosWithoutEpisode(t *testing.T) {
	t.Parallel()
	p := NewParser(nil)
	videos := channelVideos("UCcreator",
		platform.Video{ID: "numbered", Title: "Outer Wilds #2"},
		platform.Video{ID: "pilot", Title: "First | Outer Wilds"},
		platform.Video{ID: "second", Title: "Second | Outer Wilds"},
	)
	list := p.Infer(videos, nil)
	got := ThumbnailCandidates(list)
	if len(got) != 2 {
		t.Fatalf("candidates=%+v", got)
	}
	read := p.Infer(videos, map[string]int{"youtube:pilot": 1, "youtube:second": 3})
	if got := ThumbnailCandidates(read); len(got) != 0 {
		t.Fatalf("a number already read is not a candidate: %+v", got)
	}
}

// The real case: a demo played in 2023-24 numbered (02) to (04), then the released game restarting at (01).
func balatroVideos() []platform.Video {
	return []platform.Video{
		{ID: "g1", Title: "LE GOTY EST SORTI | Balatro (01)", Published: "2024-02-22"},
		{ID: "g2", Title: "LA BONNE AVENTURE | Balatro (02)", Published: "2024-02-27"},
		{ID: "g3", Title: "NOURRIR SON VAMPIRE | Balatro (03)", Published: "2024-02-28"},
		{ID: "g4", Title: "SOS FANTÔMES | Balatro (04)", Published: "2024-02-29"},
		{ID: "g5", Title: "LE PREMIER RUN INFINI ?! | Balatro (05)", Published: "2024-03-01"},
		{ID: "d2", Title: "C'EST LE BUS MAGIQUE | Balatro (02)", Published: "2023-11-27"},
		{ID: "d3", Title: "NOUVELLE DÉMO, NOUVELLE LOBO | Balatro (03)", Published: "2024-02-01"},
		{ID: "d4", Title: "LA DETTE CRÉE DE LA RICHESSE | Balatro (04)", Published: "2024-02-12"},
	}
}

func TestNumberingThatStartsOverBecomesTwoSeasonsInPublicationOrder(t *testing.T) {
	t.Parallel()
	list := infer(channelVideos("UCmistermv", balatroVideos()...)...)
	if len(list) != 1 {
		t.Fatalf("series=%+v", list)
	}
	s := list[0]
	if s.Order != "episode" || !reflect.DeepEqual(ids(s), []string{"d2", "d3", "d4", "g1", "g2", "g3", "g4", "g5"}) {
		t.Fatalf("order=%q ids=%v", s.Order, ids(s))
	}
	for _, item := range s.Items {
		season := number(item.Season)
		if (item.Video.ID[0] == 'd') != (season == 1) || (item.Video.ID[0] == 'g') != (season == 2) {
			t.Fatalf("%s has season %d", item.Video.ID, season)
		}
	}
	if number(s.Items[0].Episode) != 2 || number(s.Items[3].Episode) != 1 {
		t.Fatalf("episodes keep their own numbers: %+v", s.Items)
	}
}

func TestUnnumberedVideosJoinInferredSeasonsByDate(t *testing.T) {
	t.Parallel()
	before := append(balatroVideos(), platform.Video{ID: "first", Title: "LE TEASER | Balatro", Published: "2023-10-24"})
	list := infer(channelVideos("UCmistermv", before...)...)
	got := ids(list[0])
	if len(got) != 9 || got[0] != "first" || number(list[0].Items[0].Season) != 1 || number(list[0].Items[0].Episode) != 0 {
		t.Fatalf("a video dated before the first run opens its season: %v", got)
	}
	inGap := append(balatroVideos(), platform.Video{ID: "between", Title: "LE CLIP DU MILLION | Balatro", Published: "2024-02-15"})
	list = infer(channelVideos("UCmistermv", inGap...)...)
	for _, item := range list[0].Items {
		if item.Video.ID == "between" && item.Season != nil {
			t.Fatalf("a video dated between the runs stays unseasoned: %+v", item)
		}
	}
}

func TestDiscoveryPilotJoinsTheFirstSeasonOfItsSeries(t *testing.T) {
	t.Parallel()
	videos := append(balatroVideos(), platform.Video{ID: "first", Title: "POKER + ROGUELITE = OUI | Balatro (découverte)", Published: "2023-10-24"})
	list := infer(channelVideos("UCmistermv", videos...)...)
	if len(list) != 1 {
		t.Fatalf("the pilot must join the Balatro series: %+v", list)
	}
	if got, want := ids(list[0]), []string{"first", "d2", "d3", "d4", "g1", "g2", "g3", "g4", "g5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("season 1 opens with the pilot: %v", got)
	}
	if number(list[0].Items[0].Episode) != 0 || number(list[0].Items[0].Season) != 1 {
		t.Fatalf("the pilot keeps no number and takes season 1: %+v", list[0].Items[0])
	}
}

func TestNumberingRestartIsNotInferredWithoutDatesOrWithAmbiguity(t *testing.T) {
	t.Parallel()
	undated := balatroVideos()
	undated[0].Published = ""
	for name, videos := range map[string][]platform.Video{
		"a missing date": undated,
		// 03 before 02 is a video published out of order, not a new run.
		"out of order": {{ID: "a", Title: "Game #1", Published: "2026-01-01"}, {ID: "b", Title: "Game #3", Published: "2026-01-02"}, {ID: "c", Title: "Game #2", Published: "2026-01-03"}, {ID: "d", Title: "Game #4", Published: "2026-01-04"}},
		// A run of one is a re-upload or a typo, not a playthrough.
		"a run of one": {{ID: "a", Title: "Game #1", Published: "2026-01-01"}, {ID: "b", Title: "Game #2", Published: "2026-01-02"}, {ID: "c", Title: "Game #3", Published: "2026-01-03"}, {ID: "d", Title: "Game #1", Published: "2026-01-04"}},
		// The same number twice inside one run.
		"a repeat inside a run": {{ID: "a", Title: "Game #1", Published: "2026-01-01"}, {ID: "b", Title: "Game #2", Published: "2026-01-02"}, {ID: "c", Title: "Game #2", Published: "2026-01-03"}, {ID: "d", Title: "Game #3", Published: "2026-01-04"}},
	} {
		list := infer(channelVideos("UCcreator", videos...)...)
		if len(list) != 1 {
			t.Fatalf("%s: series=%+v", name, list)
		}
		for _, item := range list[0].Items {
			if item.Season != nil {
				t.Errorf("%s: a season was invented for %s", name, item.Video.ID)
			}
		}
	}
	explicit := infer(channelVideos("UCcreator",
		platform.Video{ID: "a", Title: "Game S01E01", Published: "2026-01-01"}, platform.Video{ID: "b", Title: "Game S01E02", Published: "2026-01-02"},
		platform.Video{ID: "c", Title: "Game S01E01", Published: "2026-01-03"}, platform.Video{ID: "d", Title: "Game S01E02", Published: "2026-01-04"})...)
	for _, item := range explicit[0].Items {
		if number(item.Season) != 1 {
			t.Fatalf("explicit seasons must stay as written: %+v", item)
		}
	}
}

func TestRepeatedNumbersAskForDatesOnlyWhereTwoRunsCouldExist(t *testing.T) {
	t.Parallel()
	undated := make([]platform.Video, 0, 8)
	for _, video := range balatroVideos() {
		video.Published = ""
		undated = append(undated, video)
	}
	list := infer(channelVideos("UCmistermv", undated...)...)
	if got := MissingPublications(list); len(got) != 8 {
		t.Fatalf("a series with repeated numbers and no dates must have them fetched: %d", len(got))
	}
	if got := MissingPublications(infer(channelVideos("UCmistermv", balatroVideos()...)...)); len(got) != 0 {
		t.Fatalf("known dates are not requested again: %+v", got)
	}
	// Listing titles are used as supplied, and nothing is marked as verified by asking.
	sufficient := infer(channelVideos("UCfirst",
		platform.Video{ID: "one", Title: "Show | Series #1"},
		platform.Video{ID: "two", Title: "Show | Series #2"},
		platform.Video{ID: "singleton", Title: "Show | Different series #1", Published: "2025-01-01"},
	)...)
	if got := MissingPublications(sufficient); len(got) != 0 {
		t.Fatalf("sufficient listing titles requested metadata: %+v", got)
	}
}
