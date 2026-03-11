package app

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// libFeedVideo stores a video of a channel whose listing is current (the way a refresh leaves it), so that
// reading the feed has nothing to schedule, and returns its record id.
func libFeedVideo(t *testing.T, app core.App, v platform.Video) string {
	t.Helper()
	v.Platform = platform.First(v.Platform, "youtube")
	if v.Title == "" {
		v.Title = "A video of " + v.ChannelName
	}
	v.SubscriberChecked = true
	id := storedVideo(t, app, v)
	channel, err := app.FindRecordById("channels", libChannelRecordOf(t, app, id))
	if err != nil {
		t.Fatal(err)
	}
	libFresh(t, app, channel)
	return id
}

// libChannelRecordOf returns the channel record id of a stored video.
func libChannelRecordOf(t *testing.T, app core.App, videoID string) string {
	t.Helper()
	record, err := app.FindRecordById("videos", videoID)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("channel")
}

// libFollow subscribes the profile to the channel of a stored video.
func libFollow(t *testing.T, app core.App, profile *core.Record, videoID string) {
	t.Helper()
	libRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": libChannelRecordOf(t, app, videoID)})
}

func libFeedOf(t *testing.T, handler http.Handler, profileID, token string) []platform.Video {
	t.Helper()
	return libOK[[]platform.Video](t, request(t, handler, "/api/feed?profile="+profileID, token))
}

func libRecommendations(t *testing.T, handler http.Handler, profileID, video, token string) []string {
	t.Helper()
	return libVideoIDs(libOK[[]platform.Video](t, request(t, handler, "/api/recommendations?profile="+profileID+"&video="+video, token)))
}

func TestFeedChecksTheProfileIsolatesSubscriptionsAndReadsTheCacheOnly(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	app, handler := libApp(t, listing.option())
	owner, other := testUser(t, app, "feed-owner@example.test"), testUser(t, app, "feed-other@example.test")
	first, second, empty := libProfile(t, app, owner, "First"), libProfile(t, app, owner, "Second"), libProfile(t, app, owner, "Empty")
	foreign := libProfile(t, app, other, "Foreign")
	token, otherToken := tokenOf(t, owner), tokenOf(t, other)

	for _, test := range []struct {
		name, path, token string
		status            int
		code              string
	}{
		{"missing", "/api/feed", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"invalid", "/api/feed?profile=not-a-profile", token, http.StatusNotFound, "NOT_FOUND"},
		{"foreign", "/api/feed?profile=" + foreign.Id, token, http.StatusNotFound, "NOT_FOUND"},
		{"other account", "/api/feed?profile=" + first.Id, otherToken, http.StatusNotFound, "NOT_FOUND"},
		{"anonymous missing", "/api/feed", "", http.StatusUnauthorized, ""},
		{"anonymous profile", "/api/feed?profile=" + first.Id, "", http.StatusUnauthorized, ""},
	} {
		libFail(t, request(t, handler, test.path, test.token), test.status, test.code)
		if t.Failed() {
			t.Fatal(test.name)
		}
	}

	ids := map[string]string{}
	for _, v := range []platform.Video{
		{ID: "firstnew001", ChannelID: libChannelID("first"), ChannelName: "First", Published: "2026-10-02"},
		{ID: "firstold001", ChannelID: libChannelID("first"), ChannelName: "First", Published: "2026-10-01"},
		{ID: "secondnew01", ChannelID: libChannelID("second"), ChannelName: "Second", Published: "2026-10-02"},
		{ID: "secondold01", ChannelID: libChannelID("second"), ChannelName: "Second", Published: "2026-10-01"},
		{ID: "foreignvid1", ChannelID: libChannelID("foreign"), ChannelName: "Foreign", Published: "2026-10-03"},
	} {
		ids[v.ID] = libFeedVideo(t, app, v)
	}
	libFollow(t, app, first, ids["firstnew001"])
	libFollow(t, app, second, ids["secondnew01"])
	libFollow(t, app, foreign, ids["foreignvid1"])

	for _, test := range []struct {
		profile *core.Record
		want    []string
	}{
		{first, []string{ids["firstnew001"], ids["firstold001"]}},
		{second, []string{ids["secondnew01"], ids["secondold01"]}},
		{empty, []string{}},
		{first, []string{ids["firstnew001"], ids["firstold001"]}},
	} {
		res := request(t, handler, "/api/feed?profile="+test.profile.Id, token)
		if got := libVideoIDs(libOK[[]platform.Video](t, res)); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("profile %s feed=%v want=%v", test.profile.GetString("name"), got, test.want)
		}
		// Every video is addressed by its record id and never by the platform's.
		for native := range ids {
			if strings.Contains(res.Body.String(), native) {
				t.Fatalf("the feed shows the native id %s: %s", native, res.Body)
			}
		}
	}
	if listing.listings() != 0 {
		t.Fatalf("reading the feed started the downloader %d time(s) for channels with a current listing", listing.listings())
	}
}

func TestFeedNeverWaitsForTheDownloaderAndShowsTheRefreshedListingNextTime(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	listing.mode(t, "hold")
	app, handler := libApp(t, listing.option())
	owner := testUser(t, app, "feed-refresh@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)
	// A subscribed channel nobody has listed yet: its listing is stale.
	libSubscribe(t, app, profile, "youtube", libChannelID("never"), "Never listed")

	// The answer does not wait for the listing, which the downloader cannot give yet: an empty cache is an
	// empty feed, not an error. The refresh is scheduled in the background.
	if videos := libFeedOf(t, handler, profile.Id, token); videos == nil || len(videos) != 0 {
		t.Fatalf("a feed of an empty cache is empty: %+v", videos)
	}
	waitFor(t, "the background refresh to start the downloader", func() bool { return listing.listings() == 1 })
	if again := libFeedOf(t, handler, profile.Id, token); len(again) != 0 {
		t.Fatalf("the feed still waits for the refresh: %+v", again)
	}
	// The channel already has a refresh in flight: asking again does not start another one.
	if got := listing.listings(); got != 1 {
		t.Fatalf("a refresh already running was started again: %d listings", got)
	}

	listing.mode(t, "")
	waitFor(t, "the refresh to cache the listing", func() bool {
		n, err := app.CountRecords("videos")
		return err == nil && n > 0
	})
	videos := libFeedOf(t, handler, profile.Id, token)
	if len(videos) != 1 || videos[0].Title != "Video of "+libChannelID("never") || videos[0].ID != videos[0].RecordID {
		t.Fatalf("the feed after the refresh: %+v", videos)
	}
	// Recommendations read the same cache and never wait either.
	if got := libVideoIDs(libOK[[]platform.Video](t, request(t, handler, "/api/recommendations?profile="+profile.Id+"&video=neverseen00000", token))); !reflect.DeepEqual(got, []string{videos[0].ID}) {
		t.Fatalf("recommendations after the refresh: %v", got)
	}

	// The explicit refresh route is the one that lists in the request itself.
	other := libSubscribe(t, app, profile, "youtube", libChannelID("explicit"), "Explicit")
	before := listing.listings()
	result := libOK[map[string]int](t, libDo(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+profile.Id, token, nil))
	if result["refreshed"] != 1 || listing.listings() != before+1 {
		t.Fatalf("explicit refresh: %v", result)
	}
	if n, err := app.CountRecords("videos", dbx.HashExp{"channel": libChannelOf(t, app, other).Id}); err != nil || n != 1 {
		t.Fatalf("the explicit refresh cached %d videos: %v", n, err)
	}
	if videos = libFeedOf(t, handler, profile.Id, token); len(videos) != 2 {
		t.Fatalf("both listings feed the profile: %+v", videos)
	}
}

func TestFeedLeavesOutPlatformsTheAccountTurnedOff(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "feed-activation@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)
	kept := libFeedVideo(t, app, platform.Video{ID: "keptvideo01", ChannelID: libChannelID("kept"), ChannelName: "Kept"})
	dropped := libFeedVideo(t, app, platform.Video{ID: "123456789", Platform: "twitch", ChannelID: "dropped", ChannelName: "Dropped"})
	libFollow(t, app, profile, kept)
	libFollow(t, app, profile, dropped)

	if got := libVideoIDs(libFeedOf(t, handler, profile.Id, token)); len(got) != 2 {
		t.Fatalf("both platforms feed the account by default: %v", got)
	}
	libFail(t, libActivate(t, handler, token, "twitch", false), http.StatusOK, "")
	if got := libVideoIDs(libFeedOf(t, handler, profile.Id, token)); !reflect.DeepEqual(got, []string{kept}) {
		t.Fatalf("a disabled platform must leave the feed: %v", got)
	}
}

func TestFeedLikesAreScopedToTheProfile(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "feed-ranking@example.test")
	first, second := libProfile(t, app, owner, "First"), libProfile(t, app, owner, "Second")
	token := tokenOf(t, owner)
	shared, seeds := libChannelID("shared"), libChannelID("seeds")
	blue := libFeedVideo(t, app, platform.Video{ID: "bluevideo01", ChannelID: shared, ChannelName: "Shared", Tags: []string{"blue"}, Published: "2026-01-01"})
	red := libFeedVideo(t, app, platform.Video{ID: "redvideo001", ChannelID: shared, ChannelName: "Shared", Tags: []string{"red"}, Published: "2026-01-01"})
	blueSeed := libFeedVideo(t, app, platform.Video{ID: "blueseed001", ChannelID: seeds, ChannelName: "Seeds", Tags: []string{"blue"}})
	redSeedOne := libFeedVideo(t, app, platform.Video{ID: "redseedone1", ChannelID: seeds, ChannelName: "Seeds", Tags: []string{"red"}})
	redSeedTwo := libFeedVideo(t, app, platform.Video{ID: "redseedtwo1", ChannelID: seeds, ChannelName: "Seeds", Tags: []string{"red"}})
	libFollow(t, app, first, blue)
	libFollow(t, app, second, blue)
	for _, like := range []struct {
		profile *core.Record
		video   string
	}{{first, blueSeed}, {second, redSeedOne}, {second, redSeedTwo}} {
		libRecord(t, app, "video_reactions", map[string]any{"profile": like.profile.Id, "video": like.video, "reaction": "like"})
	}
	if got := libVideoIDs(libFeedOf(t, handler, first.Id, token)); !reflect.DeepEqual(got, []string{blue, red}) {
		t.Fatalf("the likes of a sibling profile changed the ranking of the first: %v", got)
	}
	if got := libVideoIDs(libFeedOf(t, handler, second.Id, token)); !reflect.DeepEqual(got, []string{red, blue}) {
		t.Fatalf("the likes of the second profile did not decide its ranking: %v", got)
	}
}

func TestRecommendationsCheckTheProfileAndTheVideoParameter(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "reco-owner@example.test"), testUser(t, app, "reco-other@example.test")
	profile, foreign := libProfile(t, app, owner, "Mine"), libProfile(t, app, other, "Foreign")
	token := tokenOf(t, owner)
	for _, test := range []struct {
		name, path, token string
		status            int
		code              string
	}{
		{"missing profile", "/api/recommendations?video=v", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"missing video", "/api/recommendations?profile=" + profile.Id, token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"invalid profile", "/api/recommendations?profile=not-a-profile&video=v", token, http.StatusNotFound, "NOT_FOUND"},
		{"foreign profile", "/api/recommendations?profile=" + foreign.Id + "&video=v", token, http.StatusNotFound, "NOT_FOUND"},
		{"anonymous", "/api/recommendations?profile=" + profile.Id + "&video=v", "", http.StatusUnauthorized, ""},
	} {
		libFail(t, request(t, handler, test.path, test.token), test.status, test.code)
		if t.Failed() {
			t.Fatal(test.name)
		}
	}
}

func TestRecommendationsAnchorOnTheCurrentVideoAndSkipWhatWasWatched(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	app, handler := libApp(t, listing.option())
	owner := testUser(t, app, "reco-anchor@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)
	watchedChannel, subscribed := libChannelID("watched"), libChannelID("subscribed")
	// The video being watched: its channel and tags anchor the ranking.
	current := libFeedVideo(t, app, platform.Video{ID: "currentvid1", ChannelID: watchedChannel, ChannelName: "Watched", Tags: []string{"space"}, Published: "2026-10-01"})
	// Candidates, all equally old: a same-topic one, a same-channel one (the channel is not subscribed, its
	// cached videos still count) and an off-topic one.
	sameTopic := libFeedVideo(t, app, platform.Video{ID: "sametopic01", ChannelID: subscribed, ChannelName: "Subscribed", Tags: []string{"space"}, Published: "2026-09-01"})
	sameChannel := libFeedVideo(t, app, platform.Video{ID: "samechan001", ChannelID: watchedChannel, ChannelName: "Watched", Published: "2026-09-01"})
	// Older than the others, so that the order without an anchor does not depend on storage order.
	offTopic := libFeedVideo(t, app, platform.Video{ID: "offtopic001", ChannelID: subscribed, ChannelName: "Subscribed", Tags: []string{"cooking"}, Published: "2026-08-01"})
	libFollow(t, app, profile, sameTopic)

	// The current video is excluded; the same-channel video ranks first, the shared-tag one before the off-topic one.
	if got := libRecommendations(t, handler, profile.Id, current, token); !reflect.DeepEqual(got, []string{sameChannel, sameTopic, offTopic}) {
		t.Fatalf("recommendations=%v want=%v", got, []string{sameChannel, sameTopic, offTopic})
	}
	// What the profile watched is not suggested again, and another profile's history is not its own.
	sibling := libProfile(t, app, owner, "Sibling")
	libRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": sameChannel, "watched_at": "2026-11-01 00:00:00.000Z"})
	if got := libRecommendations(t, handler, profile.Id, current, token); !reflect.DeepEqual(got, []string{sameTopic, offTopic}) {
		t.Fatalf("a watched video was suggested: %v", got)
	}
	libFollow(t, app, sibling, sameTopic)
	if got := libRecommendations(t, handler, sibling.Id, current, token); !reflect.DeepEqual(got, []string{sameChannel, sameTopic, offTopic}) {
		t.Fatalf("the history of another profile hid a video: %v", got)
	}
	// A video that was never stored anchors nothing: the answer is the generic ranking.
	if got := libRecommendations(t, handler, profile.Id, "neverseen00000", token); !reflect.DeepEqual(got, []string{sameTopic, offTopic}) {
		t.Fatalf("an unknown anchor: %v", got)
	}
	if listing.listings() != 0 {
		t.Fatalf("recommendations started the downloader %d time(s)", listing.listings())
	}
}

func TestRecommendationsRespectTheSettingToHideSubscriberOnlyVideos(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "reco-subscriber@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)
	streamer := libChannelID("streamer")
	current := libFeedVideo(t, app, platform.Video{ID: "currentvid1", ChannelID: streamer, ChannelName: "Streamer", Published: "2026-10-01"})
	public := libFeedVideo(t, app, platform.Video{ID: "publicvod001", ChannelID: streamer, ChannelName: "Streamer", Published: "2026-09-02"})
	restricted := libFeedVideo(t, app, platform.Video{ID: "subonlyvod1", ChannelID: streamer, ChannelName: "Streamer", Published: "2026-09-01", SubscriberOnly: true})
	libFollow(t, app, profile, current)
	settings := libRecord(t, app, "settings", map[string]any{"user": owner.Id, "hide_subscriber_only": true})

	if got := libRecommendations(t, handler, profile.Id, current, token); !reflect.DeepEqual(got, []string{public}) {
		t.Fatalf("a hidden subscriber-only VOD leaked: %v", got)
	}
	// Opting back in brings the restricted VOD back.
	settings.Set("hide_subscriber_only", false)
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	if got := libRecommendations(t, handler, profile.Id, current, token); !reflect.DeepEqual(got, []string{public, restricted}) {
		t.Fatalf("the subscriber-only VOD is missing after opting in: %v", got)
	}
}
