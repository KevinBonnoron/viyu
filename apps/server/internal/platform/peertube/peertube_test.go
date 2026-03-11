package peertube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/safehttp"
)

const (
	testChannel = "studio"
	testUUID    = "b24e88e2-ff37-430f-a073-7deda03e4f4a"
	liveUUID    = "11111111-2222-3333-4444-555555555555"
)

// fixture is one instance and the Sepia index in a single local server. It listens on a loopback address,
// which the production client refuses, so the platform gets the test server's own client.
type fixture struct {
	*Platform
	host     string
	requests atomic.Int32
	hls      atomic.Bool
	live     atomic.Bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.hls.Store(true)
	mux := http.NewServeMux()
	video := func(uuid string, hls bool) map[string]any {
		item := map[string]any{
			"uuid": uuid, "name": "Open movie", "description": "Short", "duration": 855, "views": 64, "likes": 3, "dislikes": 1,
			"publishedAt": "2026-10-07T09:01:04.381Z", "thumbnailPath": "/lazy-static/thumbnails/a.jpg", "previewPath": "/lazy-static/previews/a.jpg",
			"isLive": uuid == liveUUID, "tags": []string{"film"}, "language": map[string]any{"id": "fr"},
			"channel": map[string]any{"name": testChannel, "displayName": "Studio", "host": f.host, "avatars": []map[string]any{{"path": "/avatar-small.png", "width": 48}, {"path": "/avatar-large.png", "width": 600}}},
		}
		if hls {
			item["streamingPlaylists"] = []map[string]any{{"type": 1, "playlistUrl": "http://" + f.host + "/hls/master.m3u8"}}
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
		return func(w http.ResponseWriter, r *http.Request) { f.requests.Add(1); next(w, r) }
	}
	mux.HandleFunc("GET /api/v1/search/videos", count(func(w http.ResponseWriter, r *http.Request) {
		listed, stranger := video(testUUID, true), video("99999999-aaaa-bbbb-cccc-dddddddddddd", true)
		stranger["channel"] = map[string]any{"name": "elsewhere", "host": "unlisted.example"}
		reply(w, map[string]any{"total": 41, "data": []any{listed, stranger}})
	}))
	mux.HandleFunc("GET /api/v1/search/video-channels", count(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 2, "data": []any{
			map[string]any{"name": testChannel, "displayName": "Studio", "host": f.host, "avatars": []map[string]any{{"path": "/avatar-large.png", "width": 600}}},
			map[string]any{"name": "elsewhere", "displayName": "Elsewhere", "host": "unlisted.example"},
		}})
	}))
	mux.HandleFunc("GET /api/v1/videos/{uuid}", count(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("uuid") == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(w, video(r.PathValue("uuid"), f.hls.Load()))
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
		reply(w, map[string]any{"name": testChannel, "displayName": "Studio", "host": f.host, "followersCount": 40, "description": "About the studio",
			"avatars": []map[string]any{{"path": "/avatar-small.png", "width": 48}, {"path": "/avatar-large.png", "width": 600}}})
	}))
	mux.HandleFunc("GET /api/v1/video-channels/{handle}/videos", count(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("isLive") == "true" {
			if !f.live.Load() {
				reply(w, map[string]any{"total": 0, "data": []any{}})
				return
			}
			reply(w, map[string]any{"total": 1, "data": []any{video(liveUUID, true)}})
			return
		}
		reply(w, map[string]any{"total": 2, "data": []any{video(testUUID, true), video("22222222-aaaa-bbbb-cccc-dddddddddddd", true)}})
	}))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	f.host = strings.TrimPrefix(server.URL, "http://")
	f.Platform = New(Options{HTTP: server.Client(), Scheme: "http", SepiaURL: server.URL, Instances: []string{f.host}})
	return f
}

func (f *fixture) videoID(uuid string) string { return f.host + ":" + uuid }

func (f *fixture) channelID() string { return testChannel + "@" + f.host }

func apiError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *apierr.Error
	if !errors.As(err, &failure) || failure.Status != status || (code != "" && failure.Code != code) {
		t.Fatalf("want apierr %d %s, got %#v", status, code, err)
	}
}

func TestIdentifiersNameTheirInstance(t *testing.T) {
	t.Parallel()
	p := New(Options{Instances: []string{"framatube.org"}})
	for id, want := range map[string]bool{
		"framatube.org:b24e88e2-ff37-430f-a073-7deda03e4f4a": true,
		"127.0.0.1:9000:abc":      true, // a port belongs to the host
		"framatube.org":           false,
		"framatube.org:not valid": false,
		":abc":                    false,
		"dQw4w9WgXcQ":             false,
	} {
		if got := p.ClaimsVideoID(id); got != want {
			t.Errorf("ClaimsVideoID(%q) = %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"framatube.org:abc-123": true, "other.example:abc-123": false, "framatube.org:": false, "abc": false} {
		if got := p.ValidVideoID(id); got != want {
			t.Errorf("ValidVideoID(%q) = %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"studio@framatube.org": true, "studio@other.example": false, "studio": false, "@framatube.org": false, "a/b@framatube.org": false} {
		if got := p.ValidChannelID(id); got != want {
			t.Errorf("ValidChannelID(%q) = %v, want %v", id, got, want)
		}
		if got := p.ValidLiveChannel(id); got != want {
			t.Errorf("ValidLiveChannel(%q) = %v, want %v", id, got, want)
		}
	}
	if got := p.OriginalURL("framatube.org:abc-123"); got != "https://framatube.org/w/abc-123" {
		t.Errorf("OriginalURL: %q", got)
	}
	if got := p.OriginalURL("other.example:abc-123"); got != "" {
		t.Errorf("an unlisted instance has no original URL: %q", got)
	}
}

func TestCommentTextIsPlainText(t *testing.T) {
	t.Parallel()
	if got := plainText("<p>Hello <b>there</b></p><p>second &amp; last</p>"); got != "Hello there\nsecond & last" {
		t.Fatalf("comment text: %q", got)
	}
	if got := plainText(`<p>one<br>two</p>`); got != "one\ntwo" {
		t.Fatalf("line break: %q", got)
	}
}

func TestDefaultClientNeverReachesAPrivateAddress(t *testing.T) {
	t.Parallel()
	var reached atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer local.Close()
	host := strings.TrimPrefix(local.URL, "http://")
	p := New(Options{Scheme: "http", Instances: []string{host}})
	_, err := p.PlaybackManifest(context.Background(), host+":"+testUUID, "")
	apiError(t, err, http.StatusBadGateway, apierr.CodeRequestFailed)
	if reached.Load() != 0 {
		t.Fatal("a request left the process for a private address")
	}
	if _, err := safehttp.Public().Get(local.URL); err == nil {
		t.Fatal("the production client reached a loopback address")
	}
}

func TestDescribesAPublicOptInPlatformWithoutAConnection(t *testing.T) {
	t.Parallel()
	p := New(Options{})
	d := p.Descriptor()
	if p.ID() != "peertube" || d.Platform != "peertube" || d.Name != "PeerTube" || d.ConnectionMethods == nil || len(d.ConnectionMethods) != 0 {
		t.Fatalf("descriptor: %+v", d)
	}
	if p.Connector() != nil || p.CatalogSync() != nil || p.Chat() != nil || p.DefaultEnabled() || !p.SingleTitle() || p.CommentsMode() != platform.CommentsList {
		t.Fatal("PeerTube is opt-in, public, single-titled, with a comment list and no catalog sync or chat")
	}
	if got := p.ChannelIDAliases("studio@framatube.org"); len(got) != 1 || got[0] != "studio@framatube.org" {
		t.Fatalf("aliases: %v", got)
	}
	end := p.EndLive("studio@framatube.org")
	if len(end.ChannelIDs) != 1 || end.ChannelIDs[0] != "studio@framatube.org" || end.StubVideoID != "" {
		t.Fatalf("live end: %+v", end)
	}
	p.EnrichListing(context.Background(), "studio@framatube.org", nil)
}

func TestInstancesComeFromTheEnvironmentAtConstruction(t *testing.T) {
	t.Setenv(envInstances, " Peer.Example , bad host,tube.example:8443 ")
	p := New(Options{})
	if !p.TrustedMediaHost("peer.example") || p.TrustedMediaHost("framatube.org") || !p.ValidChannelID("a@tube.example:8443") {
		t.Fatalf("configured instances: %v", p.instances)
	}
	t.Setenv(envInstances, "")
	if d := New(Options{}); !d.TrustedMediaHost("framatube.org") || d.TrustedMediaHost("peer.example") {
		t.Fatalf("defaults: %v", d.instances)
	}
	if p.TrustedMediaHost("framatube.org") {
		t.Fatal("an existing platform must keep the list read at construction")
	}
	if explicit := New(Options{Instances: []string{"only.example"}}); !explicit.TrustedMediaHost("only.example") || explicit.TrustedMediaHost("peer.example") {
		t.Fatalf("explicit instances: %v", explicit.instances)
	}
}

func TestResolveURLAcceptsWatchPagesOfListedInstances(t *testing.T) {
	t.Parallel()
	p := New(Options{Instances: []string{"framatube.org"}})
	for name, test := range map[string]struct {
		link platform.Link
		want string
	}{
		"watch":      {platform.Link{Host: "framatube.org", Parts: []string{"w", testUUID}}, "framatube.org:" + testUUID},
		"legacy":     {platform.Link{Host: "framatube.org", Parts: []string{"videos", "watch", testUUID}}, "framatube.org:" + testUUID},
		"unlisted":   {platform.Link{Host: "other.example", Parts: []string{"w", testUUID}}, ""},
		"not a uuid": {platform.Link{Host: "framatube.org", Parts: []string{"w", "abc"}}, ""},
		"channel":    {platform.Link{Host: "framatube.org", Parts: []string{"c", "studio"}}, ""},
		"extra":      {platform.Link{Host: "framatube.org", Parts: []string{"w", testUUID, "x"}}, ""},
	} {
		id, ok := p.ResolveURL(test.link)
		if id != test.want || ok != (test.want != "") {
			t.Errorf("%s: ResolveURL = %q, %v; want %q", name, id, ok, test.want)
		}
	}
}

func TestSearchKeepsTheListedInstancesAndNamesThemByInstance(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	page, err := f.Search(context.Background(), platform.Session{}, "film", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 2 || !page.HasMore || page.Page != 1 {
		t.Fatalf("one channel and one video of a listed instance, and more to load: %+v", page)
	}
	channel, video := page.Results[0].Channel, page.Results[1].Video
	if channel == nil || channel.ID != f.channelID() || channel.Platform != "peertube" || channel.Name != "Studio" || !strings.HasSuffix(channel.Thumbnail, "/avatar-large.png") {
		t.Fatalf("channel result: %+v", channel)
	}
	if video == nil || video.ID != f.videoID(testUUID) || video.Platform != "peertube" || video.ChannelID != f.channelID() || video.Duration != 855 || video.Published != "2026-10-07T09:01:04Z" {
		t.Fatalf("video result: %+v", video)
	}
}

func TestSearchSurvivesChannelsFailingButNotACancelledRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/video-channels") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"total":0,"data":[]}`))
	}))
	defer server.Close()
	p := New(Options{HTTP: server.Client(), Scheme: "http", SepiaURL: server.URL, Instances: []string{"x.example"}})
	page, err := p.Search(context.Background(), platform.Session{}, "film", 1)
	if err != nil || len(page.Results) != 0 || page.HasMore {
		t.Fatalf("channels are decoration: %+v %v", page, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Search(cancelled, platform.Session{}, "film", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled search returns the cancellation: %v", err)
	}
}

func TestVideoMetadataCommentsAndPlayback(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	id := f.videoID(testUUID)

	video, err := f.VideoMetadata(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if video.Platform != "peertube" || video.ID != id || video.Title != "Open movie" || video.Description != "The whole description, not the short one." || video.OriginalLanguage != "fr" || video.Live || !video.FullMetadata {
		t.Fatalf("video: %+v", video)
	}
	if video.ChannelID != f.channelID() || video.ChannelThumbnail != "http://"+f.host+"/avatar-large.png" || video.LikeCount == nil || *video.LikeCount != 3 {
		t.Fatalf("channel and reactions: %+v", video)
	}
	if video.Manifest.URL != "http://"+f.host+"/hls/master.m3u8" {
		t.Fatalf("the page's manifest: %+v", video.Manifest)
	}

	comments, err := f.Comments(ctx, id, "")
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments: %+v %v", comments, err)
	}
	if first := comments[0]; first.ID != "7" || first.Author != "Ann" || first.Text != "Hello there\nsecond & last" || first.PublishedAt != "2026-10-08T10:00:00Z" {
		t.Fatalf("first comment: %+v", first)
	}
	if comments[1].Author != "cat" {
		t.Fatalf("a deleted comment must not appear: %+v", comments)
	}

	manifest, err := f.PlaybackManifest(ctx, id, "")
	if err != nil || manifest.URL != "http://"+f.host+"/hls/master.m3u8" {
		t.Fatalf("manifest: %+v %v", manifest, err)
	}
}

func TestVideoWithoutHLSCannotBePlayed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.hls.Store(false)
	id := f.videoID("22222222-aaaa-bbbb-cccc-dddddddddddd")
	_, err := f.PlaybackManifest(context.Background(), id, "")
	apiError(t, err, http.StatusUnprocessableEntity, "SOURCE_STREAM_UNSUPPORTED")
	// The metadata still answers, with no manifest.
	video, err := f.VideoMetadata(context.Background(), id, "")
	if err != nil || video.Manifest.URL != "" {
		t.Fatalf("metadata of a progressive-only video: %+v %v", video, err)
	}
}

func TestUnknownVideoIsNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, err := f.VideoMetadata(context.Background(), f.videoID("missing"), "")
	apiError(t, err, http.StatusNotFound, "SOURCE_NOT_FOUND")
}

func TestFailuresKeepTheirCauseAndHideTheInstance(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret internal detail", http.StatusInternalServerError)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	p := New(Options{HTTP: server.Client(), Scheme: "http", Instances: []string{host}})
	_, err := p.VideoMetadata(context.Background(), host+":"+testUUID, "")
	apiError(t, err, http.StatusBadGateway, apierr.CodeRequestFailed)
	var failure *apierr.Error
	if errors.As(err, &failure); strings.Contains(failure.Message, "secret") || failure.Cause == nil {
		t.Fatalf("message must stay generic and the cause attached: %+v", failure)
	}
}

func TestNeverContactsAnInstanceThatIsNotListed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	const stranger = "unlisted.example"
	_, err := f.VideoMetadata(ctx, stranger+":"+testUUID, "")
	apiError(t, err, http.StatusBadRequest, apierr.CodeInvalidSourceArg)
	_, err = f.PlaybackManifest(ctx, stranger+":"+testUUID, "")
	apiError(t, err, http.StatusBadRequest, apierr.CodeInvalidSourceArg)
	_, err = f.Comments(ctx, stranger+":"+testUUID, "")
	apiError(t, err, http.StatusBadRequest, apierr.CodeInvalidSourceArg)
	_, _, err = f.ChannelListing(ctx, "studio@"+stranger, 20, "")
	apiError(t, err, http.StatusBadRequest, apierr.CodeInvalidSourceArg)
	_, err = f.LiveStream(ctx, "studio@"+stranger)
	apiError(t, err, http.StatusBadRequest, apierr.CodeInvalidSourceArg)
	if f.requests.Load() != 0 {
		t.Fatalf("an unlisted instance led to %d request(s)", f.requests.Load())
	}
}

func TestChannelListingAndLive(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	channel, videos, err := f.ChannelListing(ctx, f.channelID(), 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if channel.ID != f.channelID() || channel.Platform != "peertube" || channel.Name != "Studio" || channel.SubscriberCount != 40 || channel.Description != "About the studio" || channel.Thumbnail != "http://"+f.host+"/avatar-large.png" || len(videos) != 2 {
		t.Fatalf("channel: %+v %d videos", channel, len(videos))
	}
	if videos[0].ID != f.videoID(testUUID) || videos[0].ChannelID != f.channelID() {
		t.Fatalf("channel videos: %+v", videos)
	}

	stream, err := f.LiveStream(ctx, f.channelID())
	if err != nil || stream.Live {
		t.Fatalf("offline channel: %+v %v", stream, err)
	}
	f.live.Store(true)
	stream, err = f.LiveStream(ctx, f.channelID())
	if err != nil || !stream.Live || stream.VideoID != f.videoID(liveUUID) || stream.Title != "Open movie" || stream.Viewers != 64 {
		t.Fatalf("live stream: %+v %v", stream, err)
	}
	if got := f.LiveThumbnail(stream); got != "http://"+f.host+"/lazy-static/previews/a.jpg" {
		t.Fatalf("live thumbnail: %q", got)
	}
}

func TestEndpointPathsAreEscaped(t *testing.T) {
	t.Parallel()
	p := New(Options{Scheme: "http", Instances: []string{"a.example"}})
	if got := p.channelURL("n", "a.example"); got != "http://a.example/api/v1/video-channels/"+url.PathEscape("n@a.example") {
		t.Fatalf("channel url: %q", got)
	}
}
