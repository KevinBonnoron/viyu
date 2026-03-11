package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// contentFreshChannel stores a channel the way a complete listing does: its expiry is renewed, so reading
// the channel needs no refresh.
func contentFreshChannel(t *testing.T, app core.App, nativeID, name string) {
	t.Helper()
	channel := platform.Channel{ID: nativeID, Platform: "youtube", Name: name}
	if err := newStore(app).StoreChannelListing(&channel, ""); err != nil {
		t.Fatal(err)
	}
}

// A platform with a catalog sync has the series analysis woken by every subscription, which stores the
// channel's videos on its own: the feed tests subscribe to Twitch, whose channels are only ever listed by the
// refresh, so what the feed shows can only come from the store or from that refresh.
func TestFeedAnswersFromTheStoreAndTheRefreshFillsIt(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, contentTwitchDownloader)
	app, handler := newApp(t, option)
	user, token := viewerOf(t, handler)
	profile := profileOf(t, app, user)
	_, channelID := contentSubscribe(t, app, profile, "twitch", "channel", "")
	feedPath := "/api/feed?profile=" + profile.Id

	// Nothing is stored yet: the feed says so at once instead of running the downloader for the page.
	if videos := contentDecode[[]platform.Video](t, request(t, handler, feedPath, token)); len(videos) != 0 {
		t.Fatalf("the feed fetched in the request: %+v", videos)
	}
	res := send(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+profile.Id, token, "")
	if refreshed := contentDecode[map[string]int](t, res); refreshed["failed"] != 0 {
		t.Fatalf("refresh: %s", res.Body)
	}
	var videos []platform.Video
	waitFor(t, "the refreshed listing in the feed", func() bool {
		videos = contentDecode[[]platform.Video](t, request(t, handler, feedPath, token))
		return len(videos) == 1
	})
	videoID := recordIDOf(t, app, "videos", "123", "twitch")
	if videos[0].ID != videoID || videos[0].RecordID != videoID || videos[0].ChannelID != channelID || videos[0].Platform != "twitch" {
		t.Fatalf("feed: %+v", videos[0])
	}
	// The video is attributed to its actual channel, never to a channel made out of the video's own id.
	if _, err := newStore(app).FindChannel("twitch", "123"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a channel was made out of a video: %v", err)
	}

	// The channel page and the recommendations read what is stored.
	page := contentDecode[contentListing](t, request(t, handler, "/api/channel/"+channelID, ""))
	if page.Channel.ID != channelID || len(page.Videos) != 1 || page.Videos[0].ID != videoID || page.Videos[0].ChannelID != channelID {
		t.Fatalf("the channel link did not resolve the canonical owner: %+v", page)
	}
	if recommended := contentDecode[[]platform.Video](t, request(t, handler, "/api/recommendations?profile="+profile.Id+"&video="+videoID, token)); len(recommended) != 0 {
		t.Fatalf("the only video is the one being watched: %+v", recommended)
	}
}

func TestFeedShowsTheNewestVideosFirstWhateverTheRecommendationScore(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, "exit 1")
	app, handler := newApp(t, option)
	user, token := viewerOf(t, handler)
	profile := profileOf(t, app, user)
	// The old video shares fifty tags with what the profile liked: it scores highest, but is shown by date.
	oldID := storedVideo(t, app, platform.Video{ID: "oldvideo001", ChannelID: "UCold", ChannelName: "Old", Title: "old", Published: "2025-01-01", Tags: strings.Fields(strings.Repeat("fav ", 50))})
	newID := storedVideo(t, app, platform.Video{ID: "newvideo001", ChannelID: "UCnew", ChannelName: "New", Title: "new", Published: "2026-10-30"})
	unknownID := storedVideo(t, app, platform.Video{ID: "unknown0001", ChannelID: "UCunknown", ChannelName: "Unknown", Title: "unknown"})
	for _, channel := range []string{"UCold", "UCnew", "UCunknown"} {
		contentFreshChannel(t, app, channel, channel)
		contentSubscribe(t, app, profile, "youtube", channel, "")
	}
	reactions, err := app.FindCollectionByNameOrId("video_reactions")
	if err != nil {
		t.Fatal(err)
	}
	reaction := core.NewRecord(reactions)
	reaction.Load(map[string]any{"profile": profile.Id, "video": oldID, "reaction": "like"})
	if err := app.Save(reaction); err != nil {
		t.Fatal(err)
	}

	videos := contentDecode[[]platform.Video](t, request(t, handler, "/api/feed?profile="+profile.Id, token))
	if len(videos) != 3 || videos[0].ID != newID || videos[1].ID != oldID || videos[2].ID != unknownID {
		t.Fatalf("the feed is not newest first with unknown dates last: %+v", videos)
	}
}

func TestChannelAvatarIsIndependentOfVideoArtworkAndNameChanges(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, fixtureDownloaderBody)
	app, handler := newApp(t, option)
	video := storedVideo(t, app, platform.Video{ID: "fixture-video", ChannelID: "UCfixture", ChannelName: "Fixture channel"})
	contentDecode[platform.Video](t, request(t, handler, "/api/video/"+video, ""))

	const avatar = "https://yt3.googleusercontent.com/fixture-avatar=s0"
	page := contentDecode[contentListing](t, request(t, handler, "/api/channel/"+recordIDOf(t, app, "channels", "UCfixture", "youtube"), ""))
	if page.Channel.Thumbnail != avatar || len(page.Videos) != 1 || page.Videos[0].ChannelThumbnail != avatar {
		t.Fatalf("the channel used video or banner artwork, or trusted a partial owner cache: %+v", page)
	}

	const updated = "https://yt3.googleusercontent.com/updated-avatar=s0"
	storedVideo(t, app, platform.Video{ID: "fixture-video", Title: "Flat listing", ChannelID: "UCfixture", ChannelName: "Fixture channel", ChannelThumbnail: updated})
	answer := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+video, ""))
	if answer.ChannelThumbnail != updated || answer.ChannelName != "Fixture channel" || answer.Thumbnail != "https://example.test/video.jpg" || answer.OriginalTitle != "Original fixture" {
		t.Fatalf("an avatar change was ignored because the name stayed the same, or damaged the video: %+v", answer)
	}
	if calls.count("watch?v=fixture-video") != 1 {
		t.Fatalf("the video page was fetched again: %q", calls.all())
	}
}

func TestChannelRefreshKeepsKnownDatesAndVerifiedTitlesWhenTheListingOmitsThem(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, `printf '%s\n' '{"_type":"playlist","id":"UCfixture","channel_id":"UCfixture","channel":"Creator","entries":[{"id":"unknown0001","title":"Unknown"},{"id":"original001","title":"Translated listing title"},{"id":"datedvideo1","title":"Recent"}]}'`)
	app, handler := newApp(t, option)
	storedVideo(t, app, platform.Video{ID: "datedvideo1", Title: "Recent", ChannelID: "UCfixture", ChannelName: "Creator", Published: "2026-10-06"})
	storedVideo(t, app, platform.Video{ID: "original001", Title: "Un titre français", OriginalTitle: "Un titre français", OriginalLanguage: "fr-FR", ChannelID: "UCfixture", ChannelName: "Creator", Published: "2025-01-01", FullMetadata: true})
	path := "/api/channel/" + recordIDOf(t, app, "channels", "UCfixture", "youtube")

	for _, phase := range []string{"fresh listing", "stored listing"} {
		videos := contentDecode[contentListing](t, request(t, handler, path, "")).Videos
		if len(videos) != 3 {
			t.Fatalf("%s: %+v", phase, videos)
		}
		dated, original, unknown := recordIDOf(t, app, "videos", "datedvideo1", "youtube"), recordIDOf(t, app, "videos", "original001", "youtube"), recordIDOf(t, app, "videos", "unknown0001", "youtube")
		if videos[0].ID != dated || videos[0].Published != "2026-10-06" || videos[1].ID != original || videos[1].Title != "Un titre français" || videos[1].OriginalLanguage != "fr-FR" || videos[2].ID != unknown || videos[2].Published != "" {
			t.Fatalf("%s lost known publication metadata or ordered unknown dates first: %+v", phase, videos)
		}
	}
	if calls.count("/channel/UCfixture/videos") != 1 {
		t.Fatalf("the second page view should have come from the store: %q", calls.all())
	}
}
