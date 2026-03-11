package catalog

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const fixtureAvatar = "https://yt3.googleusercontent.com/fixture-avatar=s176-c"

func TestSearchResultsGetKnownAvatarsAndUnknownChannelsAskForTheirProfile(t *testing.T) {
	t.Parallel()
	s, _, wakes := newStore(t)
	if _, err := s.UpsertChannel(alpha, "UCknown", "Known", fixtureAvatar, ""); err != nil {
		t.Fatal(err)
	}
	wakes.profiles.Store(0)
	given := "https://yt3.googleusercontent.com/given=s176-c"
	results := []platform.Result{
		{Channel: &platform.ChannelCard{ID: "UCknown", Platform: alpha, Name: "Known"}},
		{Channel: &platform.ChannelCard{ID: "UCnew", Platform: alpha, Name: "New"}},
		{Channel: &platform.ChannelCard{ID: "UCgiven", Platform: alpha, Name: "Given", Thumbnail: given}},
		{Video: &platform.Video{ID: "video", Platform: alpha, Title: "A video"}},
	}
	kept, dropped, err := s.StoreResults(results, "")
	if err != nil || dropped != 0 || len(kept) != 4 {
		t.Fatalf("kept %d dropped %d: %v", len(kept), dropped, err)
	}
	if got := kept[0].Channel.Thumbnail; got != fixtureAvatar {
		t.Fatalf("known avatar was not added to the result: %q", got)
	}
	if got := kept[1].Channel.Thumbnail; got != "" {
		t.Fatalf("unknown channel got an avatar: %q", got)
	}
	if got := kept[2].Channel.Thumbnail; got != given {
		t.Fatalf("a supplied avatar was replaced: %q", got)
	}
	if kept[3].Video == nil || kept[3].Video.ID != "video" || kept[3].Video.RecordID == "" {
		t.Fatalf("video result: %+v", kept[3])
	}
	for _, card := range []*platform.ChannelCard{kept[0].Channel, kept[1].Channel, kept[2].Channel} {
		if card.RecordID == "" {
			t.Fatalf("card %q was not stored", card.ID)
		}
	}
	if wakes.profiles.Load() == 0 {
		t.Fatal("registering an unknown channel did not ask for its profile")
	}
	// A card is not a listing: the registered channel stays expired.
	if year := mustFindChannel(t, s, alpha, "UCnew").GetDateTime("expires_at").Time().Year(); year >= 2000 {
		t.Fatalf("a channel created by a card must start expired: %d", year)
	}
	if _, err := json.Marshal(kept); err != nil {
		t.Fatalf("stored results must serialize: %v", err)
	}
}

func TestSearchResultsRecordWhoSearched(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	user := testUser(t, app, "searcher@example.test")
	results := []platform.Result{
		{Video: &platform.Video{ID: "found", Platform: alpha, Title: "Found", ChannelID: "UCfound", ChannelName: "Found"}},
		{Channel: &platform.ChannelCard{ID: "UCcard", Platform: alpha, Name: "Card"}},
	}
	if _, _, err := s.StoreResults(results, user.Id); err != nil {
		t.Fatal(err)
	}
	for _, record := range []*core.Record{mustFindVideo(t, s, alpha, "found"), mustFindChannel(t, s, alpha, "UCfound"), mustFindChannel(t, s, alpha, "UCcard")} {
		if owners := record.GetStringSlice("owners"); len(owners) != 1 || owners[0] != user.Id {
			t.Errorf("%s owners = %v", record.GetString("platform_id"), owners)
		}
	}
}

// A result that could not be stored is dropped, never returned with its native id.
func TestSearchResultsThatCannotBeStoredAreDropped(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	failOn := func(collection, nativeID string) {
		app.OnRecordCreate(collection).BindFunc(func(e *core.RecordEvent) error {
			if e.Record.GetString("platform_id") == nativeID {
				return errors.New("the database refused " + nativeID)
			}
			return e.Next()
		})
	}
	failOn("videos", "bad-video")
	failOn("channels", "UCbad-card")
	failOn("channels", "UCbad-owner")

	results := []platform.Result{
		{Video: &platform.Video{ID: "good-video", Platform: alpha, Title: "Good", ChannelID: "UCgood", ChannelName: "Good"}},
		{Video: &platform.Video{ID: "bad-video", Platform: alpha, Title: "Bad"}},
		{Channel: &platform.ChannelCard{ID: "UCbad-card", Platform: alpha, Name: "Bad card"}},
		{Video: &platform.Video{ID: "bad-owner-video", Platform: alpha, Title: "Bad owner", ChannelID: "UCbad-owner", ChannelName: "Bad owner"}},
		{Channel: &platform.ChannelCard{ID: "UCgood-card", Platform: alpha, Name: "Good card"}},
		{},
	}
	kept, dropped, err := s.StoreResults(results, "")
	if err == nil || dropped != 4 || len(kept) != 2 {
		t.Fatalf("kept %d dropped %d err %v", len(kept), dropped, err)
	}
	for _, id := range []string{"bad-video", "UCbad-card", "UCbad-owner"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("the error does not name %s: %v", id, err)
		}
	}
	if kept[0].Video == nil || kept[0].Video.ID != "good-video" || kept[1].Channel == nil || kept[1].Channel.ID != "UCgood-card" {
		t.Fatalf("the survivors or their order changed: %+v %+v", kept[0], kept[1])
	}
	if _, err := json.Marshal(kept); err != nil {
		t.Fatalf("what is returned must serialize: %v", err)
	}
	// Nothing that was dropped is serializable either.
	for _, result := range results {
		if result.Video != nil && result.Video.ID == "bad-video" {
			if result.Video.RecordID != "" {
				t.Errorf("a failed video kept a record id %q", result.Video.RecordID)
			}
			if _, err := json.Marshal(result.Video); !errors.Is(err, platform.ErrNotStored) {
				t.Errorf("a failed video must refuse to serialize: %v", err)
			}
		}
	}
	// The video of the failed channel was not stored detached.
	if _, err := s.FindVideo(alpha, "bad-owner-video"); err == nil {
		t.Error("a video whose channel failed was stored without it")
	}
}

func TestStoreVideosFailsPerItemAndStoresTheRest(t *testing.T) {
	t.Parallel()
	s, app, w := newStore(t)
	app.OnRecordCreate("videos").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("platform_id") == "bad" {
			return errors.New("boom")
		}
		return e.Next()
	})
	batch := []platform.Video{listing(alpha, "one"), listing(alpha, "bad"), listing(alpha, "two")}
	err := s.StoreVideos(batch, "")
	if err == nil || !strings.Contains(err.Error(), alpha+"/bad") {
		t.Fatalf("error = %v", err)
	}
	if batch[0].RecordID == "" || batch[2].RecordID == "" || batch[0].ChannelRecordID == "" {
		t.Fatalf("the others must be stored: %+v", batch)
	}
	if batch[1].RecordID != "" || batch[1].ChannelRecordID != "" {
		t.Fatalf("the failed video must keep no record id: %+v", batch[1])
	}
	if _, err := json.Marshal(batch[1]); !errors.Is(err, platform.ErrNotStored) {
		t.Fatalf("the failed video must not serialize: %v", err)
	}
	if w.publications.Load() == 0 {
		t.Fatal("videos without publication did not ask for it")
	}
}
