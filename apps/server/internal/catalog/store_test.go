package catalog

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/migrations"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestStoredRecordsAreOwnedByTheConnectedAccountsThatFetchedThemAndPublicOtherwise(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	first := testUser(t, app, "owners-first@example.test")
	second := testUser(t, app, "owners-second@example.test")
	check := func(label, id string, want ...string) {
		t.Helper()
		for name, record := range map[string]*core.Record{"video": mustFindVideo(t, s, alpha, id), "channel": mustFindChannel(t, s, alpha, "UC"+id)} {
			if got := record.GetStringSlice("owners"); strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s: %s owners = %v, want %v", label, name, got, want)
			}
		}
	}

	// A public fetch stores a public record.
	mustStore(t, s, listing(alpha, "public"), "")
	check("public fetch", "public")
	// A connected fetch of a new record makes it private to the account, and another connected account
	// that fetches it joins the owners.
	mustStore(t, s, listing(alpha, "private"), first.Id)
	check("connected create", "private", first.Id)
	mustStore(t, s, listing(alpha, "private"), second.Id)
	check("second connected account", "private", first.Id, second.Id)
	mustStore(t, s, listing(alpha, "private"), second.Id)
	check("same account again", "private", first.Id, second.Id)
	// A connected fetch never takes a public record private.
	mustStore(t, s, listing(alpha, "public"), first.Id)
	check("connected fetch of a public record", "public")
	// A public fetch makes a private record public: what it returned is public data.
	mustStore(t, s, listing(alpha, "private"), "")
	check("public fetch of a private record", "private")
}

func TestDescriptionsAreCutToTheSchemaLimitWithoutSplittingACharacter(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	description := strings.Repeat("é", migrations.DescriptionMax+10)
	v := listing(alpha, "long")
	v.Description = description
	mustStore(t, s, v, "")
	record := mustFindVideo(t, s, alpha, "long")
	if got := record.GetString("description"); len([]rune(got)) != migrations.DescriptionMax || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("description holds %d characters", len([]rune(got)))
	}
	info := platform.Channel{ID: "UClong", Platform: alpha, Name: "Long", Description: description}
	if err := s.StoreChannelListing(&info, ""); err != nil {
		t.Fatalf("a long channel description must not fail the save: %v", err)
	}
}

func TestRecordsAnotherAccountStoredPrivatelyAreNotVisible(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	first := testUser(t, app, "private-first@example.test")
	second := testUser(t, app, "private-second@example.test")
	mustStore(t, s, listing(alpha, "secret-one"), first.Id)
	video := mustFindVideo(t, s, alpha, "secret-one")
	channel := mustFindChannel(t, s, alpha, "UCsecret-one")
	for name, record := range map[string]*core.Record{"video": video, "channel": channel} {
		if VisibleTo(record, second.Id) || VisibleTo(record, "") || !VisibleTo(record, first.Id) {
			t.Errorf("%s visible to second=%v anonymous=%v owner=%v", name, VisibleTo(record, second.Id), VisibleTo(record, ""), VisibleTo(record, first.Id))
		}
	}
	if videos, err := s.CachedChannelVideos(channel.Id, second.Id); err != nil || len(videos) != 0 {
		t.Errorf("another account's listing: %v %v", videos, err)
	}
	if videos, err := s.CachedChannelVideos(channel.Id, first.Id); err != nil || len(videos) != 1 {
		t.Errorf("the owner's listing: %v %v", videos, err)
	}
}

func TestListingsWithoutChannelAttributionAreStoredWithoutDetaching(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	mustStore(t, s, platform.Video{ID: "owned", Platform: alpha, ChannelID: "UCowner", ChannelName: "Owner", Title: "Owned"}, "")
	attached := mustFindVideo(t, s, alpha, "owned").GetString("channel")
	if attached == "" {
		t.Fatal("attributed video lost its channel")
	}

	// Flat search listings can omit the owner; they must not detach an attributed video from its channel.
	v := mustStore(t, s, platform.Video{ID: "owned", Platform: alpha, Title: "Flat listing without owner"}, "")
	if got := mustFindVideo(t, s, alpha, "owned").GetString("channel"); got != attached || v.ChannelRecordID != attached {
		t.Fatalf("flat listing detached the channel: %q (reported %q) want %q", got, v.ChannelRecordID, attached)
	}
	// An ownerless listing is still stored, without a channel, so it stays addressable by record id.
	stray := mustStore(t, s, platform.Video{ID: "stray", Platform: alpha, Title: "No owner at all"}, "")
	if stray.RecordID == "" || mustFindVideo(t, s, alpha, "stray").GetString("channel") != "" {
		t.Fatalf("unattributed listing: %+v", stray)
	}
}

// Clients read video records directly, so a single-title video must be stored with its title already
// verified: there is no translated title, and an unverified record would make them keep asking for an
// original that does not exist.
func TestSingleTitlePlatformVideosAreStoredTitleVerified(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	mustStore(t, s, platform.Video{ID: "3001", Platform: solo, ChannelID: "streamer", ChannelName: "Streamer", Title: "Only title"}, "")
	record := mustFindVideo(t, s, solo, "3001")
	if _, original := TitlesOf(record); !MetadataOf(record).OriginalTitleVerified || original != "Only title" {
		t.Fatalf("stored video: verified=%v original=%q", MetadataOf(record).OriginalTitleVerified, original)
	}

	// Other listings still wait for proof of their original title.
	mustStore(t, s, platform.Video{ID: "yt1", Platform: alpha, ChannelID: "UCyt", ChannelName: "YT", Title: "Maybe translated"}, "")
	if MetadataOf(mustFindVideo(t, s, alpha, "yt1")).OriginalTitleVerified {
		t.Fatal("a listing was marked verified")
	}
}

// Every save pushes an update to all realtime clients, which re-render everything that reads the
// collection. Storing an unchanged record, as a catalog crawl does once per video, must not write it.
func TestStoringUnchangedRecordsDoesNotRewriteThem(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	var channelUpdates, videoUpdates int
	app.OnRecordAfterUpdateSuccess("channels").BindFunc(func(e *core.RecordEvent) error {
		channelUpdates++
		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess("videos").BindFunc(func(e *core.RecordEvent) error {
		videoUpdates++
		return e.Next()
	})
	steady := func(id string) platform.Video {
		return platform.Video{ID: id, Platform: alpha, ChannelID: "UCsteady", ChannelName: "Steady", ChannelThumbnail: "https://yt3.googleusercontent.com/avatar=s900-c-k-c0x00ffffff-no-rj", Title: id}
	}
	mustStore(t, s, steady("first"), "")
	for _, id := range []string{"second", "third", "fourth"} {
		mustStore(t, s, steady(id), "")
	}
	for range 3 {
		mustStore(t, s, steady("first"), "")
	}
	if channelUpdates != 0 || videoUpdates != 0 {
		t.Fatalf("unchanged records were rewritten: %d channel updates, %d video updates", channelUpdates, videoUpdates)
	}

	renamed := steady("fifth")
	renamed.ChannelName = "Steady, renamed"
	mustStore(t, s, renamed, "")
	if channelUpdates != 1 {
		t.Fatalf("a renamed channel must be saved once, got %d updates", channelUpdates)
	}
	if got := mustFindChannel(t, s, alpha, "UCsteady").GetString("name"); got != "Steady, renamed" {
		t.Fatalf("name = %q", got)
	}
	retitled := steady("first")
	retitled.Title = "A new title"
	mustStore(t, s, retitled, "")
	if videoUpdates != 1 {
		t.Fatalf("a changed video must be saved once, got %d updates", videoUpdates)
	}
}

// The expiry of a stored record is renewed only when it is meaningfully stale (renewMargin), so a listing
// stored over and over writes the record at most once per margin.
func TestExpiryIsRenewedOnlyWhenMeaningfullyStale(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	updates := 0
	app.OnRecordAfterUpdateSuccess("videos").BindFunc(func(e *core.RecordEvent) error {
		updates++
		return e.Next()
	})
	mustStore(t, s, listing(alpha, "renewal"), "")
	set := func(expiry time.Time) {
		t.Helper()
		record := mustFindVideo(t, s, alpha, "renewal")
		record.Set("expires_at", expiry)
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
		updates = 0
	}

	set(time.Now().Add(recordTTL - renewMargin/2))
	mustStore(t, s, listing(alpha, "renewal"), "")
	if updates != 0 {
		t.Fatalf("a record renewed %v ago was rewritten", renewMargin/2)
	}
	set(time.Now().Add(recordTTL - 2*renewMargin))
	mustStore(t, s, listing(alpha, "renewal"), "")
	if updates != 1 {
		t.Fatalf("a stale record was not renewed: %d updates", updates)
	}
	if left := time.Until(mustFindVideo(t, s, alpha, "renewal").GetDateTime("expires_at").Time()); left < recordTTL-time.Minute {
		t.Fatalf("renewed expiry is %v away", left)
	}
	set(time.Now().Add(-time.Hour))
	mustStore(t, s, listing(alpha, "renewal"), "")
	if updates != 1 {
		t.Fatalf("an expired record was not renewed: %d updates", updates)
	}
}

func TestVerifiedMetadataSurvivesFlatListings(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	const original = "Les eaux magiques face à la science"
	mustStore(t, s, platform.Video{ID: "testvid", Platform: alpha, Title: "Magical waters versus science", OriginalTitle: "Magical waters versus science", ChannelID: "testvid", ChannelName: "Magical waters versus science"}, "")

	// The video's own page proves the original title and names the real owner.
	full := platform.Video{ID: "testvid", Platform: alpha, Title: original, OriginalTitle: original, ChannelID: "UCcreator", ChannelName: "DEFAKATOR", Published: "2026-03-04", Duration: 2529, FullMetadata: true, Chapters: []platform.Chapter{{Title: "One", StartTime: 1}}}
	mustStore(t, s, full, "")
	record := mustFindVideo(t, s, alpha, "testvid")
	if got := s.VideoOf(record); !got.OriginalTitleVerified || got.Title != original || got.ChannelID != "UCcreator" || got.ChannelName != "DEFAKATOR" || got.Published != "2026-03-04" || len(got.Chapters) != 1 {
		t.Fatalf("full metadata not stored: %+v", got)
	}

	// A later listing refresh must not replace verified original metadata.
	flat := mustStore(t, s, platform.Video{ID: "testvid", Platform: alpha, Title: "Translated listing title", ChannelID: "UCcreator", ChannelName: "DEFAKATOR", Duration: 1, Description: "flat"}, "")
	got := s.VideoOf(mustFindVideo(t, s, alpha, "testvid"))
	if got.Title != original || got.Duration != 2529 || got.Description != "" || got.Published != "2026-03-04" || len(got.Chapters) != 1 || flat.Title != original {
		t.Fatalf("listing refresh overwrote verified metadata: %+v (stored by the listing: %+v)", got, flat)
	}

	// Even after the verified metadata expired.
	record = mustFindVideo(t, s, alpha, "testvid")
	ExpireMetadata(record)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	mustStore(t, s, platform.Video{ID: "testvid", Platform: alpha, Title: "Translated after expiry", OriginalTitle: "Translated after expiry", ChannelID: "UCcreator", ChannelName: "DEFAKATOR"}, "")
	got = s.VideoOf(mustFindVideo(t, s, alpha, "testvid"))
	if !got.OriginalTitleVerified || got.OriginalTitle != original {
		t.Fatalf("an expired full cache let a flat translated listing replace its verified original title: %+v", got)
	}
}

func TestListingsMergeThumbnailsAndCountersWithWhatIsCached(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	likes, dislikes := 12.0, 0.0
	mustStore(t, s, platform.Video{ID: "merge", Platform: alpha, Title: "T", Thumbnails: []string{"https://example.test/a.jpg"}, LikeCount: &likes, DislikeCount: &dislikes, Creators: []string{"Ann"}}, "")
	mustStore(t, s, platform.Video{ID: "merge", Platform: alpha, Title: "T", Thumbnails: []string{"https://example.test/b.jpg", "https://example.test/a.jpg", ""}}, "")
	got := s.VideoOf(mustFindVideo(t, s, alpha, "merge"))
	if !slices.Equal(got.Thumbnails, []string{"https://example.test/a.jpg", "https://example.test/b.jpg"}) || got.Thumbnail != "https://example.test/a.jpg" {
		t.Fatalf("thumbnails = %v", got.Thumbnails)
	}
	if got.LikeCount == nil || *got.LikeCount != 12 || got.DislikeCount == nil || *got.DislikeCount != 0 || !slices.Equal(got.Creators, []string{"Ann"}) {
		t.Fatalf("counters or creators lost: %+v", got)
	}
}

func TestProviderReactionCountsDistinguishUnavailableFromZero(t *testing.T) {
	t.Parallel()
	zero := 0.0
	likes := 12993.0
	for _, fixture := range []struct {
		name               string
		likes, dislikes    *float64
		wantLikes          float64
		dislikesAvailable  bool
		wantDislikesIfAny  float64
		wantLikesAvailable bool
	}{
		{name: "hidden dislikes", likes: &likes, wantLikes: 12993, wantLikesAvailable: true},
		{name: "known zero counters", likes: &zero, dislikes: &zero, wantLikesAvailable: true, dislikesAvailable: true},
		{name: "no counters"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			s, _, _ := newStore(t)
			v := platform.Video{ID: "counts00001", Platform: alpha, ChannelID: "UCfixture", ChannelName: "Creator", FullMetadata: true, LikeCount: fixture.likes, DislikeCount: fixture.dislikes}
			mustStore(t, s, v, "")
			for phase := range 2 {
				// The second phase stores a flat listing, which carries no counters of its own.
				if phase == 1 {
					mustStore(t, s, platform.Video{ID: "counts00001", Platform: alpha, ChannelID: "UCfixture", ChannelName: "Creator"}, "")
				}
				got := s.VideoOf(mustFindVideo(t, s, alpha, "counts00001"))
				if (got.LikeCount != nil) != fixture.wantLikesAvailable || (got.DislikeCount != nil) != fixture.dislikesAvailable {
					t.Fatalf("phase %d lost provider counter availability: %+v", phase, got)
				}
				if got.LikeCount != nil && *got.LikeCount != fixture.wantLikes {
					t.Fatalf("phase %d changed likes: %v", phase, *got.LikeCount)
				}
				if got.DislikeCount != nil && *got.DislikeCount != 0 {
					t.Fatalf("phase %d changed an explicit zero dislike count", phase)
				}
			}
		})
	}
}

func TestPublicationTimeIsStoredToTheMillisecondAndNeverLostToADay(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	read := func(id string) string { return PublishedOf(mustFindVideo(t, s, alpha, id)) }
	mustStore(t, s, platform.Video{ID: "time", Platform: alpha, Published: "2023-11-14T22:13:20.123456789Z", FullMetadata: true}, "")
	// PocketBase stores dates to the millisecond.
	const exact = "2023-11-14T22:13:20.123Z"
	if got := read("time"); got != exact {
		t.Fatalf("store lost the exact time: %q", got)
	}
	mustStore(t, s, platform.Video{ID: "time", Platform: alpha, Published: "2023-11-14", FullMetadata: true}, "")
	if got := read("time"); got != exact {
		t.Fatalf("a refresh that knows only the day lost the exact time: %q", got)
	}
	mustStore(t, s, platform.Video{ID: "time", Platform: alpha, Published: "2023-11-15", FullMetadata: true}, "")
	if got := read("time"); got != "2023-11-15" {
		t.Fatalf("a changed date retained the stale time: %q", got)
	}
}

func TestListingRefinesAStoredDayButNeverReplacesAStoredTime(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	mustStore(t, s, platform.Video{ID: "listing-day", Platform: alpha, Published: "2023-11-14"}, "")
	// A listing knowing the exact time of the stored day refines it; one naming another day does not.
	for _, step := range []struct{ published, want string }{
		{"2023-11-14T22:13:20Z", "2023-11-14T22:13:20Z"},
		{"2023-11-15T01:00:00Z", "2023-11-14T22:13:20Z"},
		{"2023-11-16", "2023-11-14T22:13:20Z"},
		{"", "2023-11-14T22:13:20Z"},
	} {
		mustStore(t, s, platform.Video{ID: "listing-day", Platform: alpha, Published: step.published}, "")
		if got := PublishedOf(mustFindVideo(t, s, alpha, "listing-day")); got != step.want {
			t.Fatalf("listing %q: stored %q, want %q", step.published, got, step.want)
		}
	}
	// A day stored as midnight is a day, never an exact time.
	mustStore(t, s, platform.Video{ID: "listing-midnight", Platform: alpha, Published: "2023-11-14T00:00:00Z"}, "")
	if got := PublishedOf(mustFindVideo(t, s, alpha, "listing-midnight")); got != "2023-11-14" {
		t.Fatalf("midnight read back as %q", got)
	}
}

// Storing a channel profile (a search result card, the avatar backfill) is not caching a listing: a
// subscribed channel whose listing expired must still be refreshed afterwards.
func TestChannelProfileStoresDoNotRenewTheListingExpiry(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	expiry := func(id string) time.Time { return mustFindChannel(t, s, alpha, id).GetDateTime("expires_at").Time() }
	mustStore(t, s, platform.Video{ID: "kept", Platform: alpha, Title: "Kept", ChannelID: "UCfixture", ChannelName: "Fixture channel"}, "")
	if year := expiry("UCfixture").Year(); year >= 2000 {
		t.Fatalf("a channel created by a video must start expired: %d", year)
	}

	profile := platform.Channel{ID: "UCfixture", Platform: alpha, Name: "Renamed", Thumbnail: "https://yt3.googleusercontent.com/avatar=s176-c", SubscriberCount: 42}
	if err := s.StoreChannelProfile(&profile, ""); err != nil || profile.RecordID == "" {
		t.Fatalf("profile: %v %+v", err, profile)
	}
	record := mustFindChannel(t, s, alpha, "UCfixture")
	if record.GetString("name") != "Renamed" || record.GetFloat("subscriber_count") != 42 || expiry("UCfixture").Year() >= 2000 {
		t.Fatalf("a profile renewed the listing or was not stored: name=%q count=%v expiry=%v", record.GetString("name"), record.GetFloat("subscriber_count"), expiry("UCfixture"))
	}
	// A channel first seen by a profile starts expired too.
	brandNew := platform.Channel{ID: "UCbrandnew", Platform: alpha, Name: "Brand new"}
	if err := s.StoreChannelProfile(&brandNew, ""); err != nil || expiry("UCbrandnew").Year() >= 2000 {
		t.Fatalf("a channel created by a profile must start expired: %v %v", err, expiry("UCbrandnew"))
	}
	// A real listing renews it.
	if err := s.StoreChannelListing(&profile, ""); err != nil || time.Until(expiry("UCfixture")) < recordTTL-time.Minute {
		t.Fatalf("a listing must renew the expiry: %v %v", err, expiry("UCfixture"))
	}
	// And does not write it again right away.
	updates := 0
	app.OnRecordAfterUpdateSuccess("channels").BindFunc(func(e *core.RecordEvent) error {
		updates++
		return e.Next()
	})
	if err := s.StoreChannelListing(&profile, ""); err != nil || updates != 0 {
		t.Fatalf("an unchanged listing was written again: %v %d", err, updates)
	}
}

func TestVideoAndChannelIdentityAndAliasLookups(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	v := mustStore(t, s, platform.Video{ID: "ident", Platform: alpha, ChannelID: "@handle", ChannelName: "Handle"}, "")
	platformID, nativeID, err := s.VideoIdentity(v.RecordID)
	if err != nil || platformID != alpha || nativeID != "ident" {
		t.Fatalf("video identity: %q %q %v", platformID, nativeID, err)
	}
	platformID, nativeID, err = s.ChannelIdentity(v.ChannelRecordID)
	if err != nil || platformID != alpha || nativeID != "@handle" {
		t.Fatalf("channel identity: %q %q %v", platformID, nativeID, err)
	}
	if _, _, err := s.VideoIdentity("missing"); err == nil {
		t.Fatal("an unknown record has no identity")
	}
	// A channel stored under its handle is found with or without the "@"; another platform's is not.
	for _, id := range []string{"@handle", "handle"} {
		if record, err := s.FindChannel(alpha, id); err != nil || record.Id != v.ChannelRecordID {
			t.Errorf("FindChannel(%q) = %v %v", id, record, err)
		}
	}
	if _, err := s.FindChannel(solo, "handle"); err == nil {
		t.Error("a channel of another platform was found")
	}
	if record, err := s.Channel(v.ChannelRecordID); err != nil || record.Id != v.ChannelRecordID {
		t.Errorf("Channel(): %v %v", record, err)
	}
}

func TestCachedAnswerNeedsVerifiedFreshMetadataAndNeverAnswersALiveStream(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	mustStore(t, s, platform.Video{ID: "answer", Platform: alpha, Title: "T", OriginalTitle: "T", ChannelID: "UCa", ChannelName: "A", FullMetadata: true}, "")
	record := mustFindVideo(t, s, alpha, "answer")
	if v, ok := s.CachedAnswer(record, true); !ok || v.RecordID != record.Id || v.ChannelName != "A" {
		t.Fatalf("a verified fresh video must answer: %v %+v", ok, v)
	}
	meta := MetadataOf(record)
	meta.ChaptersLoaded = false
	SetMetadata(record, meta)
	if _, ok := s.CachedAnswer(record, true); ok {
		t.Fatal("chapters were wanted but never loaded")
	}
	if _, ok := s.CachedAnswer(record, false); !ok {
		t.Fatal("chapters were not wanted")
	}
	ExpireMetadata(record)
	if _, ok := s.CachedAnswer(record, false); ok {
		t.Fatal("expired metadata answered")
	}
	mustStore(t, s, platform.Video{ID: "answer", Platform: alpha, Title: "T", OriginalTitle: "T", Live: true, FullMetadata: true}, "")
	live := mustFindVideo(t, s, alpha, "answer")
	if _, ok := s.CachedAnswer(live, false); ok {
		t.Fatal("a stream in progress must never be answered from the cache")
	}

}

func TestVideoRecordsLoadsAnyNumberOfVideosOfOnePlatform(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	const count = queryBatch + 5
	batch := make([]platform.Video, count)
	ids := make([]string, 0, count+1)
	for i := range batch {
		batch[i] = platform.Video{ID: "bulk" + string(rune('a'+i/26)) + string(rune('a'+i%26)), Platform: alpha, Title: "t"}
		ids = append(ids, batch[i].ID)
	}
	if err := s.StoreVideos(batch, ""); err != nil {
		t.Fatal(err)
	}
	mustStore(t, s, platform.Video{ID: batch[0].ID, Platform: solo, Title: "other platform"}, "")
	ids = append(ids, "never-stored")
	found, err := s.VideoRecords(alpha, ids)
	if err != nil || len(found) != count || found["never-stored"] != nil {
		t.Fatalf("found %d of %d: %v", len(found), count, err)
	}
	if record := found[batch[0].ID]; record == nil || record.GetString("platform") != alpha {
		t.Fatalf("another platform's video answered: %v", record)
	}
}

func TestChannelOfIsNilForANamelessStub(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	stub, err := s.EnsureChannel("", alpha, "UCstub")
	if err != nil || stub == nil {
		t.Fatalf("stub: %v %v", stub, err)
	}
	if ChannelOf(stub) != nil {
		t.Fatal("a stub is no profile")
	}
	info := platform.Channel{ID: "UCstub", Platform: alpha, Name: "Named", Thumbnail: "https://yt3.googleusercontent.com/a=s176-c", Description: "d", SubscriberCount: 3}
	if err := s.StoreChannelProfile(&info, ""); err != nil {
		t.Fatal(err)
	}
	got := ChannelOf(mustFindChannel(t, s, alpha, "UCstub"))
	if got == nil || got.Name != "Named" || got.SubscriberCount != 3 || got.RecordID != info.RecordID || got.Thumbnail == "" {
		t.Fatalf("channel = %+v", got)
	}
}

func TestVideosOfLoadsOwnerChannelsInOneQuery(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	var batch []platform.Video
	for i := range 30 {
		batch = append(batch, platform.Video{ID: "v" + string(rune('a'+i)), Platform: alpha, ChannelID: "UC" + string(rune('a'+i%3)), ChannelName: "Channel " + string(rune('a'+i%3)), Title: "t"})
	}
	if err := s.StoreVideos(batch, ""); err != nil {
		t.Fatal(err)
	}
	records, err := app.FindRecordsByFilter("videos", "", "platform_id", 0, 0)
	if err != nil || len(records) != 30 {
		t.Fatal(len(records), err)
	}
	count := countChannelQueries(app, func() {
		presented := s.VideosOf(records)
		for i, v := range presented {
			if v.RecordID != records[i].Id || v.ChannelName == "" || v.ChannelID == "" || v.ChannelRecordID == "" {
				t.Fatalf("video %d: %+v", i, v)
			}
		}
		if _, err := json.Marshal(presented); err != nil {
			t.Fatalf("a stored video must serialize: %v", err)
		}
	})
	if count != 1 {
		t.Fatalf("VideosOf ran %d channel queries for 30 videos of 3 channels, want 1", count)
	}
}

func TestTitlesRoundTrip(t *testing.T) {
	t.Parallel()
	_, app, _ := newStore(t)
	collection, err := app.FindCollectionByNameOrId("videos")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ title, original, wantTitle, wantOriginal string }{
		{"Translated", "Original", "Translated", "Original"},
		{"Same", "Same", "Same", "Same"},
		{"Only", "", "Only", "Only"},
		{"", "Orig", "Orig", "Orig"},
		{"", "", "", ""},
	} {
		r := core.NewRecord(collection)
		SetTitles(r, tc.title, tc.original)
		if title, original := TitlesOf(r); title != tc.wantTitle || original != tc.wantOriginal {
			t.Errorf("SetTitles(%q,%q) reads back %q,%q", tc.title, tc.original, title, original)
		}
	}
}

func TestErrNotStoredIsNeverAvoidedByAFailedStore(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	v := platform.Video{ID: "", Platform: alpha, Title: "no id", RecordID: "stale"}
	err := s.StoreVideo(&v, "")
	if err == nil || v.RecordID != "" {
		t.Fatalf("a video that cannot be stored must fail and keep no record id: %v %+v", err, v)
	}
	if _, jsonErr := json.Marshal(v); !errors.Is(jsonErr, platform.ErrNotStored) {
		t.Fatalf("an unstored video must refuse to serialize: %v", jsonErr)
	}
}
