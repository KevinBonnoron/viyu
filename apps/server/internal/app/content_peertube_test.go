package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	contentPeerTubeChannel = "studio"
	contentPeerTubeUUID    = "b24e88e2-ff37-430f-a073-7deda03e4f4a"
	contentPeerTubeOther   = "22222222-aaaa-bbbb-cccc-dddddddddddd"
	contentPeerTubeLive    = "11111111-2222-3333-4444-555555555555"
)

// contentPeerTube is one PeerTube instance and its Sepia index in a single local server. It listens on a
// loopback address, which the production client refuses, so the application is given the server's own client.
type contentPeerTube struct {
	host     string
	requests atomic.Int32
	hls      atomic.Bool
	live     atomic.Bool
}

// contentPeerTubeFixture starts the instance and returns the options that make the application use it as its
// only PeerTube instance, with a media client allowed to reach it.
func contentPeerTubeFixture(t *testing.T) (*contentPeerTube, func(*Options)) {
	t.Helper()
	fixture := &contentPeerTube{}
	fixture.hls.Store(true)
	mux := http.NewServeMux()
	video := func(uuid string, hls bool) map[string]any {
		item := map[string]any{
			"uuid": uuid, "name": "Open movie", "description": "Short", "duration": 855, "views": 64, "likes": 3, "dislikes": 1,
			"publishedAt": "2026-10-07T09:01:04.381Z", "thumbnailPath": "/lazy-static/thumbnails/a.jpg", "previewPath": "/lazy-static/previews/a.jpg",
			"isLive": uuid == contentPeerTubeLive, "tags": []string{"film"}, "language": map[string]any{"id": "fr"},
			"channel": map[string]any{"name": contentPeerTubeChannel, "displayName": "Studio", "host": fixture.host, "avatars": []map[string]any{{"path": "/avatar-small.png", "width": 48}, {"path": "/avatar-large.png", "width": 600}}},
		}
		if hls {
			item["streamingPlaylists"] = []map[string]any{{"type": 1, "playlistUrl": "http://" + fixture.host + "/hls/master.m3u8"}}
		} else {
			item["streamingPlaylists"] = []any{}
		}
		return item
	}
	reply := func(w http.ResponseWriter, body any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
	count := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { fixture.requests.Add(1); next(w, r) }
	}
	mux.HandleFunc("GET /api/v1/search/videos", count(func(w http.ResponseWriter, r *http.Request) {
		listed, stranger := video(contentPeerTubeUUID, true), video("99999999-aaaa-bbbb-cccc-dddddddddddd", true)
		stranger["channel"] = map[string]any{"name": "elsewhere", "host": "unlisted.example"}
		reply(w, map[string]any{"total": 41, "data": []any{listed, stranger}})
	}))
	mux.HandleFunc("GET /api/v1/search/video-channels", count(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 2, "data": []any{
			map[string]any{"name": contentPeerTubeChannel, "displayName": "Studio", "host": fixture.host, "avatars": []map[string]any{{"path": "/avatar-large.png", "width": 600}}},
			map[string]any{"name": "elsewhere", "displayName": "Elsewhere", "host": "unlisted.example"},
		}})
	}))
	mux.HandleFunc("GET /api/v1/videos/{uuid}", count(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("uuid") == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(w, video(r.PathValue("uuid"), fixture.hls.Load()))
	}))
	mux.HandleFunc("GET /api/v1/videos/{uuid}/description", count(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"description": "The whole description, not the short one."})
	}))
	mux.HandleFunc("GET /api/v1/videos/{uuid}/comment-threads", count(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 3, "data": []any{
			map[string]any{"id": 7, "text": "<p>Hello <b>there</b></p><p>second &amp; last</p>", "createdAt": "2026-10-08T10:00:00.000Z", "account": map[string]any{"name": "ann", "displayName": "Ann"}},
			map[string]any{"id": 8, "text": "<p>gone</p>", "createdAt": "2026-10-08T11:00:00.000Z", "isDeleted": true, "account": map[string]any{"name": "bob"}},
			map[string]any{"id": 9, "text": "<p>plain</p>", "createdAt": "2026-10-08T12:00:00.000Z", "account": map[string]any{"name": "cat"}},
		}})
	}))
	mux.HandleFunc("GET /api/v1/video-channels/{handle}", count(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"name": contentPeerTubeChannel, "displayName": "Studio", "host": fixture.host, "followersCount": 40, "description": "About the studio",
			"avatars": []map[string]any{{"path": "/avatar-small.png", "width": 48}, {"path": "/avatar-large.png", "width": 600}}})
	}))
	mux.HandleFunc("GET /api/v1/video-channels/{handle}/videos", count(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("isLive") == "true" {
			if !fixture.live.Load() {
				reply(w, map[string]any{"total": 0, "data": []any{}})
				return
			}
			reply(w, map[string]any{"total": 1, "data": []any{video(contentPeerTubeLive, true)}})
			return
		}
		reply(w, map[string]any{"total": 2, "data": []any{video(contentPeerTubeUUID, true), video(contentPeerTubeOther, true)}})
	}))
	mux.HandleFunc("GET /hls/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nlow.m3u8\n"))
	})
	server := httptest.NewUnstartedServer(mux)
	fixture.host = server.Listener.Addr().String()
	server.Start()
	t.Cleanup(server.Close)
	return fixture, func(o *Options) {
		o.PeerTube.HTTP = server.Client()
		o.PeerTube.Scheme = "http"
		o.PeerTube.SepiaURL = server.URL
		o.PeerTube.Instances = []string{fixture.host}
		o.MediaHTTP = &http.Client{}
	}
}

func (f *contentPeerTube) videoID(uuid string) string { return f.host + ":" + uuid }

func (f *contentPeerTube) channelID() string { return contentPeerTubeChannel + "@" + f.host }

// storedVideo persists a PeerTube video of the fixture channel and returns its record id.
func (f *contentPeerTube) storedVideo(t *testing.T, app core.App, uuid string) string {
	t.Helper()
	return storedVideo(t, app, platform.Video{ID: f.videoID(uuid), Platform: "peertube", ChannelID: f.channelID(), ChannelName: "Studio"})
}

func TestVideoURLIsThePageOnTheVideosOwnService(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	user, _ := viewerOf(t, handler)
	enableSource(t, app, user.Id, "peertube")
	for name, tc := range map[string]struct{ record, want string }{
		"youtube":     {storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ", ChannelID: "UCfixture", ChannelName: "Fixture"}), "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		"twitch VOD":  {storedVideo(t, app, platform.Video{ID: "123456", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"}), "https://www.twitch.tv/videos/123456"},
		"twitch live": {storedVideo(t, app, platform.Video{ID: "channel", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"}), "https://www.twitch.tv/channel"},
		"peertube":    {fixture.storedVideo(t, app, contentPeerTubeUUID), "http://" + fixture.host + "/w/" + contentPeerTubeUUID},
	} {
		body := contentDecode[map[string]string](t, request(t, handler, "/api/video/"+tc.record+"/url", ""))
		if body["url"] != tc.want {
			t.Fatalf("%s: url=%q, want %q", name, body["url"], tc.want)
		}
	}
	if fixture.requests.Load() != 0 {
		t.Fatalf("the page address was built by asking the instance %d time(s)", fixture.requests.Load())
	}
}

func TestPeerTubeSearchKeepsTheListedInstancesAndNamesThemByInstance(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	user, _ := viewerOf(t, handler)
	enableSource(t, app, user.Id, "peertube")

	res := request(t, handler, "/api/search?q=film&platform=peertube", "")
	page := contentDecode[contentPage](t, res)
	if len(page.Results) != 2 || !page.HasMore {
		t.Fatalf("one channel and one video of a listed instance, and more to load: %+v", page)
	}
	channel, video := page.Results[0], page.Results[1]
	channelRecord := recordIDOf(t, app, "channels", fixture.channelID(), "peertube")
	videoRecord := recordIDOf(t, app, "videos", fixture.videoID(contentPeerTubeUUID), "peertube")
	if channel["id"] != channelRecord || channel["recordId"] != channelRecord || channel["platform"] != "peertube" || channel["name"] != "Studio" || !strings.HasSuffix(channel["thumbnail"].(string), "/avatar-large.png") {
		t.Fatalf("channel result: %v", channel)
	}
	if video["id"] != videoRecord || video["recordId"] != videoRecord || video["platform"] != "peertube" || video["channelId"] != channelRecord || video["channelRecordId"] != channelRecord || video["duration"] != float64(855) || video["published"] != "2026-10-07T09:01:04Z" {
		t.Fatalf("video result: %v", video)
	}
	if strings.Contains(res.Body.String(), fixture.host) && strings.Contains(res.Body.String(), contentPeerTubeUUID) {
		t.Fatalf("the instance-qualified native id left the server: %s", res.Body)
	}
	// Every listed result is stored so it can be addressed by record id; the unlisted instance's video is not.
	if videos, err := app.FindRecordsByFilter("videos", "", "", 0, 0); err != nil || len(videos) != 1 {
		t.Fatalf("only the listed instance's video is stored: %d %v", len(videos), err)
	}
}

func TestPeerTubeJoinsTheCombinedSearchOnlyForAccountsThatTurnedItOn(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	downloader, _ := contentDownloader(t, fixtureDownloaderBody)
	app, handler := newApp(t, peertube, downloader)
	user, token := viewerOf(t, handler)
	platformsOf := func(authorization string) []string {
		page := contentDecode[contentPage](t, request(t, handler, "/api/search?q=fixture&platform=all", authorization))
		seen := map[string]bool{}
		var found []string
		for _, result := range page.Results {
			if name := result["platform"].(string); !seen[name] {
				seen[name] = true
				found = append(found, name)
			}
		}
		return found
	}
	if got := strings.Join(platformsOf(token), ","); strings.Contains(got, "peertube") || fixture.requests.Load() != 0 {
		t.Fatalf("an account that never chose is not searched on PeerTube: %q, %d requests", got, fixture.requests.Load())
	}
	enableSource(t, app, user.Id, "peertube")
	if got := strings.Join(platformsOf(token), ","); !strings.Contains(got, "peertube") || !strings.Contains(got, "youtube") || fixture.requests.Load() == 0 {
		t.Fatalf("once on, PeerTube joins the combined search: %q", got)
	}
}

func TestPeerTubeRefusesAnAccountThatNeverTurnedItOn(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	video := fixture.storedVideo(t, app, contentPeerTubeUUID)
	channel := recordIDOf(t, app, "channels", fixture.channelID(), "peertube")
	for _, path := range []string{
		"/api/search?q=film&platform=peertube",
		"/api/video/" + video,
		"/api/video/" + video + "/url",
		"/api/video/" + video + "/comments",
		"/api/channel/" + channel,
		"/api/channel/" + channel + "/live",
		"/api/stream/" + video,
	} {
		contentExpectError(t, request(t, handler, path, ""), http.StatusConflict, "SOURCE_DISABLED")
	}
	if fixture.requests.Load() != 0 {
		t.Fatalf("the instance was contacted %d time(s) for a disabled source", fixture.requests.Load())
	}
}

func TestPeerTubeVideoMetadataCommentsAndPlayback(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	user, _ := viewerOf(t, handler)
	enableSource(t, app, user.Id, "peertube")
	id := fixture.storedVideo(t, app, contentPeerTubeUUID)
	channelRecord := recordIDOf(t, app, "channels", fixture.channelID(), "peertube")

	video := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+id, ""))
	if video.Platform != "peertube" || video.ID != id || video.Title != "Open movie" || video.Description != "The whole description, not the short one." || video.Comments != "list" || video.OriginalLanguage != "fr" || video.Live {
		t.Fatalf("video: %+v", video)
	}
	if video.ChannelID != channelRecord || video.ChannelThumbnail != "http://"+fixture.host+"/avatar-large.png" || video.LikeCount == nil || *video.LikeCount != 3 {
		t.Fatalf("channel and reactions: %+v", video)
	}
	// A second opening comes from the store, and still says how comments work.
	before := fixture.requests.Load()
	if cached := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+id, "")); cached.Comments != "list" || cached.Title != "Open movie" || fixture.requests.Load() != before {
		t.Fatalf("a stored video asked the instance again (%d requests): %+v", fixture.requests.Load()-before, cached)
	}

	// The instance-qualified native id never reaches the client: the record id alone reaches every route.
	res := request(t, handler, "/api/video/"+id+"/comments", "")
	comments := contentDecode[contentComments](t, res)
	if len(comments.Comments) != 2 {
		t.Fatalf("comments: %s", res.Body)
	}
	if first := comments.Comments[0]; first.ID != "7" || first.Author != "Ann" || first.Text != "Hello there\nsecond & last" || first.PublishedAt != "2026-10-08T10:00:00Z" {
		t.Fatalf("first comment: %+v", first)
	}
	if comments.Comments[1].Author != "cat" {
		t.Fatalf("a deleted comment must not appear: %+v", comments.Comments)
	}

	if res = request(t, handler, "/api/stream/"+id, ""); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "#EXTM3U") {
		t.Fatalf("stream: status=%d body=%s", res.Code, res.Body)
	}
	if strings.Contains(res.Body.String(), fixture.host) {
		t.Fatalf("the manifest leaked the instance's address: %s", res.Body)
	}

	fixture.hls.Store(false)
	contentExpectError(t, request(t, handler, "/api/stream/"+fixture.storedVideo(t, app, contentPeerTubeOther), ""), http.StatusUnprocessableEntity, "SOURCE_STREAM_UNSUPPORTED")
}

func TestPeerTubeUnknownVideoIsNotFoundAndNotStored(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	user, _ := viewerOf(t, handler)
	enableSource(t, app, user.Id, "peertube")
	missing := fixture.storedVideo(t, app, "missing")
	contentExpectError(t, request(t, handler, "/api/video/"+missing, ""), http.StatusNotFound, "SOURCE_NOT_FOUND")
	// Only the placeholder record exists: the failed lookup stored nothing about the video.
	record, err := app.FindRecordById("videos", missing)
	if err != nil {
		t.Fatal(err)
	}
	if title, original := catalog.TitlesOf(record); title != "" || original != "" {
		t.Fatalf("an unknown video was filled in: %q %q", title, original)
	}
	if videos, err := app.FindRecordsByFilter("videos", "", "", 0, 0); err != nil || len(videos) != 1 {
		t.Fatalf("an unknown video was stored: %d %v", len(videos), err)
	}
}

func TestPeerTubeNeverContactsAnInstanceThatIsNotListed(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	user, _ := viewerOf(t, handler)
	enableSource(t, app, user.Id, "peertube")
	const stranger = "unlisted.example"
	video := storedVideo(t, app, platform.Video{ID: stranger + ":" + contentPeerTubeUUID, Platform: "peertube", ChannelID: "studio@" + stranger, ChannelName: "Studio"})
	channel := recordIDOf(t, app, "channels", "studio@"+stranger, "peertube")
	for _, path := range []string{
		"/api/video/" + video,
		"/api/video/" + video + "/comments",
		"/api/channel/" + channel,
		"/api/channel/" + channel + "/live",
		"/api/stream/" + video,
	} {
		res := request(t, handler, path, "")
		if res.Code == http.StatusOK || res.Code >= http.StatusInternalServerError {
			t.Fatalf("%s: status=%d body=%s", path, res.Code, res.Body)
		}
	}
	if fixture.requests.Load() != 0 {
		t.Fatalf("an unlisted instance led to %d request(s)", fixture.requests.Load())
	}
}

func TestPeerTubeChannelListingLiveAndSubscription(t *testing.T) {
	t.Parallel()
	fixture, peertube := contentPeerTubeFixture(t)
	app, handler := newApp(t, peertube)
	user, token := viewerOf(t, handler)
	enableSource(t, app, user.Id, "peertube")
	stored := storedChannel(t, app, "peertube", fixture.channelID(), "Studio")

	listing := contentDecode[contentListing](t, request(t, handler, "/api/channel/"+stored, ""))
	if listing.Channel.ID != stored || listing.Channel.Platform != "peertube" || listing.Channel.Name != "Studio" || listing.Channel.SubscriberCount != 40 || listing.Channel.Thumbnail != "http://"+fixture.host+"/avatar-large.png" || len(listing.Videos) != 2 {
		t.Fatalf("channel: %+v", listing)
	}
	if listing.Videos[0].ID != recordIDOf(t, app, "videos", fixture.videoID(contentPeerTubeUUID), "peertube") || listing.Videos[0].ChannelID != stored {
		t.Fatalf("channel videos: %+v", listing.Videos)
	}

	fixture.live.Store(true)
	live := contentDecode[contentLive](t, request(t, handler, "/api/channel/"+stored+"/live", ""))
	if !live.Live || live.Title != "Open movie" || live.RecordID != recordIDOf(t, app, "videos", fixture.videoID(contentPeerTubeLive), "peertube") {
		t.Fatalf("live stream: %+v", live)
	}

	// Subscribing needs the source: it is refused while PeerTube is off, and works once it is on.
	other := testUser(t, app, "peertube-subscribe@example.test")
	otherToken, profile := tokenOf(t, other), profileOf(t, app, other)
	body, _ := json.Marshal(map[string]string{"profile": profile.Id, "channelId": stored})
	contentExpectError(t, send(t, handler, http.MethodPost, "/api/subscriptions", otherToken, string(body)), http.StatusConflict, "SOURCE_DISABLED")
	enableSource(t, app, other.Id, "peertube")
	if res := send(t, handler, http.MethodPost, "/api/subscriptions", otherToken, string(body)); res.Code != http.StatusOK {
		t.Fatalf("subscribing: status=%d %s", res.Code, res.Body)
	}

	// The subscriptions page shows the stream with the instance's own preview picture.
	viewerProfile := profileOf(t, app, user)
	contentSubscribe(t, app, viewerProfile, "peertube", fixture.channelID(), "Studio")
	streams := contentDecode[[]contentLiveStream](t, request(t, handler, "/api/subscriptions/live?profile="+viewerProfile.Id, token))
	if len(streams) != 1 || streams[0].Platform != "peertube" || streams[0].Thumbnail != "http://"+fixture.host+"/lazy-static/previews/a.jpg" {
		t.Fatalf("subscriptions live: %+v", streams)
	}
}
