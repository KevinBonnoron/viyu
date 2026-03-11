package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestVideoAndChannelAreFetchedOnceAndThenServedFromTheStore(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, fixtureDownloaderBody)
	app, handler := newApp(t, option)
	contentDecode[contentPage](t, request(t, handler, "/api/search?q=fixture", ""))
	videoID, channelID := recordIDOf(t, app, "videos", "fixture-video", "youtube"), recordIDOf(t, app, "channels", "UCfixture", "youtube")

	// A search result is only a listing entry: the player fetches the video's own page.
	video := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+videoID, ""))
	if video.ID != videoID || video.RecordID != videoID || video.ChannelID != channelID || video.ChannelRecordID != channelID || video.Platform != "youtube" {
		t.Fatalf("the video lost its record ids: %+v", video)
	}
	if video.ChannelName != "Fixture channel" || len(video.Tags) != 1 || video.Comments != "list" || !video.OriginalTitleVerified {
		t.Fatalf("video metadata: %+v", video)
	}
	if calls.count("--dump-json", "watch?v=fixture-video") != 1 {
		t.Fatalf("the video page was fetched %d times: %q", calls.count("watch?v=fixture-video"), calls.all())
	}
	// A verified, fresh record answers by itself: asking the downloader again would only copy it.
	if cached := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+videoID, "")); !reflect.DeepEqual(cached, video) || calls.count("watch?v=fixture-video") != 1 {
		t.Fatalf("the stored video changed or was fetched again: %+v (%d fetches)", cached, calls.count("watch?v=fixture-video"))
	}

	listing := contentDecode[contentListing](t, request(t, handler, "/api/channel/"+channelID, ""))
	if listing.Channel.ID != channelID || listing.Channel.RecordID != channelID || len(listing.Videos) != 1 {
		t.Fatalf("channel: %+v", listing)
	}
	for _, listed := range listing.Videos {
		if listed.ID != videoID || listed.RecordID != videoID || listed.ChannelID != channelID || listed.ChannelRecordID != channelID {
			t.Fatalf("a listed video lost its record ids: %+v", listed)
		}
	}
	fetched := calls.count("/channel/UCfixture/videos")
	if fetched != 1 {
		t.Fatalf("the channel listing was fetched %d times", fetched)
	}
	if cached := contentDecode[contentListing](t, request(t, handler, "/api/channel/"+channelID, "")); len(cached.Videos) != 1 || cached.Videos[0].ID != videoID || cached.Channel.ID != channelID || calls.count("/channel/UCfixture/videos") != fetched {
		t.Fatalf("the cached channel: %+v (%d fetches)", cached, calls.count("/channel/UCfixture/videos"))
	}

	// Re-running the embedded history must neither reset records nor replay a destructive migration.
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	if recordIDOf(t, app, "videos", "fixture-video", "youtube") != videoID || recordIDOf(t, app, "channels", "UCfixture", "youtube") != channelID {
		t.Fatal("the migrations lost a stored record")
	}
}

func TestUnknownRecordsAreNotFound(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, fixtureDownloaderBody)
	_, handler := newApp(t, option)
	for _, path := range []string{"/api/video/doesnotexist1234", "/api/video/doesnotexist1234/url", "/api/video/doesnotexist1234/comments", "/api/channel/doesnotexist123", "/api/channel/doesnotexist123/live"} {
		contentExpectError(t, request(t, handler, path, ""), http.StatusNotFound, "NOT_FOUND")
	}
	// A platform id is not a record id: the route never guesses what it names.
	for _, path := range []string{"/api/video/dQw4w9WgXcQ", "/api/channel/UCfixture", "/api/stream/dQw4w9WgXcQ"} {
		if res := request(t, handler, path, ""); res.Code != http.StatusNotFound {
			t.Fatalf("%s answered a native id: %d %s", path, res.Code, res.Body)
		}
	}
	if len(calls.all()) != 0 {
		t.Fatalf("the downloader was asked about a record that does not exist: %q", calls.all())
	}
}

func TestPlayerRefreshesListingMetadataAndPreservesTheOriginalTitle(t *testing.T) {
	t.Parallel()
	const id, original = "testvid0001", "Les eaux magiques face à la science"
	option, calls := contentDownloader(t, `printf '%s\n' '{"id":"testvid0001","title":"`+original+`","fulltitle":"`+original+`","channel_id":"UCcreator","channel":"DEFAKATOR","upload_date":"20260304","duration":2529}'`)
	app, handler := newApp(t, option)
	// A flat listing that attributed the video to a channel named after the video itself.
	record := storedVideo(t, app, platform.Video{ID: id, Title: "Magical waters versus science", OriginalTitle: "Magical waters versus science", ChannelID: id, ChannelName: "Magical waters versus science"})

	video := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+record, ""))
	creator := recordIDOf(t, app, "channels", "UCcreator", "youtube")
	if !video.OriginalTitleVerified || video.Title != original || video.OriginalTitle != original || video.ChannelID != creator || video.ChannelName != "DEFAKATOR" || video.Published != "2026-03-04" {
		t.Fatalf("the player trusted incomplete or translated listing metadata: %+v", video)
	}

	// A later listing refresh must not replace verified original metadata, nor ask the downloader again.
	storedVideo(t, app, platform.Video{ID: id, Title: "Translated listing title", ChannelID: "UCcreator", ChannelName: "DEFAKATOR"})
	video = contentDecode[platform.Video](t, request(t, handler, "/api/video/"+record, ""))
	if video.Title != original || video.ChannelID != creator || video.ChannelName != "DEFAKATOR" || video.Published != "2026-03-04" || calls.count("watch?v="+id) != 1 {
		t.Fatalf("a listing refresh overwrote verified metadata: %+v (%d fetches)", video, calls.count("watch?v="+id))
	}

	// Once the verified metadata expired, the video is read again, and a translated flat listing stored in
	// between did not replace the original title either.
	stored, err := app.FindRecordById("videos", record)
	if err != nil {
		t.Fatal(err)
	}
	catalog.ExpireMetadata(stored)
	if err := app.Save(stored); err != nil {
		t.Fatal(err)
	}
	storedVideo(t, app, platform.Video{ID: id, Title: "Translated listing after expiry", OriginalTitle: "Translated listing after expiry", ChannelID: "UCcreator", ChannelName: "DEFAKATOR"})
	video = contentDecode[platform.Video](t, request(t, handler, "/api/video/"+record, ""))
	if video.Title != original || !video.OriginalTitleVerified || calls.count("watch?v="+id) != 2 {
		t.Fatalf("an expired video was not read again, or lost its original title: %+v (%d fetches)", video, calls.count("watch?v="+id))
	}
}

func TestOriginalAudioOverridesTheVisitorDubAndSurvivesTheStore(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, `printf '%s\n' '{"id":"audiovid001","title":"Original video","channel_id":"UCfixture","channel":"Creator","language":"en-US","formats":[{"acodec":"mp4a.40.2","language":"en-US","language_preference":5},{"acodec":"mp4a.40.2","language":"fr-FR","language_preference":10}]}'`)
	app, handler := newApp(t, option)
	id := storedVideo(t, app, platform.Video{ID: "audiovid001", ChannelID: "UCfixture", ChannelName: "Creator"})
	for _, stored := range []bool{false, true} {
		video := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+id, ""))
		if video.ID != id || video.OriginalLanguage != "fr-FR" {
			t.Fatalf("the visitor's dubbing replaced the original audio (from the store: %v): %+v", stored, video)
		}
	}
	if calls.count("watch?v=audiovid001") != 1 {
		t.Fatalf("the second answer should have come from the store: %q", calls.all())
	}
}

const contentChaptersDownloader = `case "$*" in
  *'/feed/subscriptions'*) printf '%s\n' '{"_type":"playlist","id":"subscriptions","title":"Subscriptions","extractor_key":"YoutubeTab","entries":[]}'; exit 0 ;;
esac
case " $* " in
  *" --cookies "*) title=Private ;;
  *) title=Public ;;
esac
printf '{"id":"dQw4w9WgXcQ","title":"%s","channel_id":"UCfixture","chapters":[{"title":"%s intro","start_time":0,"end_time":12.5},{"title":"Next","start_time":12.5,"end_time":42}]}\n' "$title" "$title"`

func TestChaptersAreFetchedForThePlayerAndKept(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, contentChaptersDownloader)
	app, handler := newApp(t, option)
	const id = "dQw4w9WgXcQ"
	record := storedVideo(t, app, platform.Video{ID: id, Title: "Old", OriginalTitle: "Old", ChannelID: "UCfixture", FullMetadata: true})
	// An existing, still-fresh record from before chapters were stored.
	stored, err := app.FindRecordById("videos", record)
	if err != nil {
		t.Fatal(err)
	}
	meta := catalog.MetadataOf(stored)
	meta.ChaptersLoaded = false
	catalog.SetMetadata(stored, meta)
	if err := app.Save(stored); err != nil {
		t.Fatal(err)
	}

	path := "/api/video/" + record
	if card := contentDecode[platform.Video](t, request(t, handler, path, "")); card.Title != "Old" || card.ID != record || len(calls.all()) != 0 {
		t.Fatalf("a card triggered a chapter refresh: %+v %q", card, calls.all())
	}
	want := []platform.Chapter{{Title: "Public intro", StartTime: 0, EndTime: 12.5}, {Title: "Next", StartTime: 12.5, EndTime: 42}}
	if player := contentDecode[platform.Video](t, request(t, handler, path+"?chapters=1", "")); !reflect.DeepEqual(player.Chapters, want) {
		t.Fatalf("player chapters: %+v", player.Chapters)
	}
	// A flat listing knows no chapters and must not erase them; they are served without the downloader.
	storedVideo(t, app, platform.Video{ID: id, Title: "Flat", ChannelID: "UCfixture"})
	if cached := contentDecode[platform.Video](t, request(t, handler, path+"?chapters=1", "")); !reflect.DeepEqual(cached.Chapters, want) || len(calls.all()) != 1 {
		t.Fatalf("the stored chapters were lost: %+v (%d downloader calls)", cached.Chapters, len(calls.all()))
	}
}

func TestConnectedChaptersStayOutOfThePublicStore(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, contentChaptersDownloader)
	app, handler := newApp(t, option)
	_, token := viewerOf(t, handler)
	path := "/api/video/" + storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ", ChannelID: "UCfixture"})
	if video := contentDecode[platform.Video](t, request(t, handler, path+"?chapters=1", "")); len(video.Chapters) != 2 || video.Chapters[0].Title != "Public intro" {
		t.Fatalf("public chapters: %+v", video.Chapters)
	}
	contentConnect(t, handler, token, contentCookies)

	private := request(t, handler, path+"?source=youtube&chapters=1", token)
	if video := contentDecode[platform.Video](t, private); len(video.Chapters) != 2 || video.Chapters[0].Title != "Private intro" {
		t.Fatalf("private chapters: %s", private.Body)
	}
	if private.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a connected answer may be kept by a cache: %v", private.Header())
	}
	if video := contentDecode[platform.Video](t, request(t, handler, path+"?chapters=1", "")); len(video.Chapters) != 2 || video.Chapters[0].Title != "Public intro" {
		t.Fatalf("private chapters leaked into the public store: %+v", video.Chapters)
	}
}

func TestVideosWithoutChaptersAreStoredAsAnEmptyList(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, "exit 1")
	app, handler := newApp(t, option)
	id := storedVideo(t, app, platform.Video{ID: "no-chapters", Title: "No chapters", OriginalTitle: "No chapters", ChannelID: "UCfixture", FullMetadata: true})
	res := request(t, handler, "/api/video/"+id+"?chapters=1", "")
	body := contentDecode[map[string]any](t, res)
	if chapters, ok := body["chapters"].([]any); !ok || len(chapters) != 0 || len(calls.all()) != 0 {
		t.Fatalf("empty chapters were not stored as an array: %s (%d downloader calls)", res.Body, len(calls.all()))
	}
}

func TestAFailedStreamIsAClientSafeBadGateway(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, fixtureDownloaderBody)
	app, handler := newApp(t, option)
	// "failed" is a fixture name, not a real YouTube id: the record carries its platform.
	res := request(t, handler, "/api/stream/"+storedVideo(t, app, platform.Video{ID: "failed"}), "")
	contentExpectError(t, res, http.StatusBadGateway, "SOURCE_REQUEST_FAILED")
	if strings.Contains(res.Body.String(), "fixture download failed") {
		t.Fatalf("the downloader's output reached the client: %s", res.Body)
	}
	// Everything under /api/stream is hardened, errors included: the bytes come from third parties.
	header := res.Header()
	if header.Get("Cache-Control") != "no-store" || header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("an error under /api/stream is not hardened: %v", header)
	}
}

func TestAVideoRequestLeavesWithItsClient(t *testing.T) {
	t.Parallel()
	started := filepath.Join(t.TempDir(), "started")
	option, _ := contentDownloader(t, `case "$*" in *watch?v=cancel*) : > '`+started+`'; exec sleep 30 ;; esac`)
	app, handler := newApp(t, option)
	id := storedVideo(t, app, platform.Video{ID: "cancel"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/video/"+id, nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()
	waitFor(t, "the downloader to start", func() bool { _, err := os.Stat(started); return err == nil })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the downloader was not stopped with its request")
	}
}

func TestCollectionTimestampsSupportClientSorting(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	user, token := viewerOf(t, handler)
	profile := profileOf(t, app, user)
	video := storedVideo(t, app, platform.Video{ID: "timestamp-video", Platform: "youtube", ChannelID: "UCactivity", Title: "timestamp-video"})
	collection, err := app.FindCollectionByNameOrId("video_reactions")
	if err != nil {
		t.Fatal(err)
	}
	reaction := core.NewRecord(collection)
	reaction.Load(map[string]any{"profile": profile.Id, "video": video, "reaction": "like"})
	if err := app.Save(reaction); err != nil {
		t.Fatal(err)
	}
	created := reaction.GetDateTime("created")
	if created.Time().IsZero() || reaction.GetDateTime("updated").Time().IsZero() {
		t.Fatal("new records must receive both timestamps")
	}
	reaction.Set("reaction", "dislike")
	if err := app.Save(reaction); err != nil {
		t.Fatal(err)
	}
	if reaction.GetDateTime("created") != created {
		t.Fatal("updating a record changed its creation time")
	}
	page := contentDecode[struct {
		Items []struct {
			ID       string `json:"id"`
			Video    string `json:"video"`
			Reaction string `json:"reaction"`
		} `json:"items"`
	}](t, request(t, handler, "/api/collections/video_reactions/records?sort=-created", token))
	if len(page.Items) != 1 || page.Items[0].ID != reaction.Id || page.Items[0].Video != video || page.Items[0].Reaction != "dislike" {
		t.Fatalf("the client's sorted collection query failed: %+v", page)
	}
}
