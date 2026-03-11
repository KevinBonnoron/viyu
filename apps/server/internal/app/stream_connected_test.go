package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const streamTestCookies = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tLOGIN_INFO\tfixture-login\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tfixture-session\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSAPISID\tfixture-sapisid\n"

// streamConnect imports the fixture cookies as the YouTube session of the account.
func streamConnect(t *testing.T, handler http.Handler, token string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"cookies": streamTestCookies})
	if err != nil {
		t.Fatal(err)
	}
	if res := send(t, handler, http.MethodPost, "/api/sources/youtube/connect", token, string(body)); res.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", res.Code, res.Body)
	}
}

func TestConnectedStreamUsesBearerTokenAndIsScopedAndRevoked(t *testing.T) {
	t.Parallel()
	upstream := streamUpstream(t)
	app, handler := newApp(t, withDownloader(t, streamYouTubeDownloader(upstream.URL+"/api/manifest/hls_playlist/master.m3u8")), withLoopbackMedia)
	user := testUser(t, app, "playback@example.test")
	token := tokenOf(t, user)
	streamConnect(t, handler, token)
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	private := "/api/stream/" + video + "?source=youtube"

	master := request(t, handler, private, token)
	if master.Code != http.StatusOK || !strings.HasPrefix(master.Body.String(), "#EXTM3U") {
		t.Fatalf("authenticated playback manifest: %d %s", master.Code, master.Body)
	}
	streamHardened(t, "connected master", master)
	resources := streamResources(master.Body.String())
	if len(resources) != 1 || !strings.Contains(resources[0], "source=youtube") {
		t.Fatalf("a connected playlist must keep the connection in its resources: %v", resources)
	}
	variant := request(t, handler, resources[0], token)
	if variant.Code != http.StatusOK || !strings.Contains(variant.Body.String(), "#EXTINF:6,") {
		t.Fatalf("private variant was not playable: %d %s", variant.Code, variant.Body)
	}
	segments := streamResources(variant.Body.String())
	if len(segments) != 1 {
		t.Fatalf("variant resources: %v", segments)
	}
	segmentPath := segments[0]
	if res := request(t, handler, segmentPath, token); res.Code != http.StatusOK || res.Body.String() != "authenticated-media" {
		t.Fatalf("private media segment: %d %s", res.Code, res.Body)
	}

	for _, path := range []string{private, segmentPath} {
		if res := anonymousRequest(t, handler, path); res.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous stream access %s: %d", path, res.Code)
		}
	}

	// Another account never reaches this account's manifest or resources, with or without the same source.
	other := testUser(t, app, "other-viewer@example.test")
	otherToken := tokenOf(t, other)
	if res := request(t, handler, segmentPath, otherToken); res.Code == http.StatusOK {
		t.Fatalf("another account read a private segment: %d", res.Code)
	}
	if res := request(t, handler, private, otherToken); res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "SOURCE_NOT_CONNECTED") {
		t.Fatalf("an account without the connection streamed through it: %d %s", res.Code, res.Body)
	}

	// A source the account turned off is refused even for public playback.
	if res := send(t, handler, http.MethodPut, "/api/sources/youtube/activation", otherToken, `{"enabled":false}`); res.Code != http.StatusOK {
		t.Fatalf("turn the source off: %d %s", res.Code, res.Body)
	}
	if res := request(t, handler, "/api/stream/"+video, otherToken); res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "SOURCE_DISABLED") {
		t.Fatalf("disabled platform streamed: %d %s", res.Code, res.Body)
	}

	// Reconnecting rotates the revision: what the previous connection handed out is gone.
	if res := send(t, handler, http.MethodDelete, "/api/sources/youtube", token, ""); res.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", res.Code, res.Body)
	}
	if res := request(t, handler, private, token); res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "SOURCE_NOT_CONNECTED") {
		t.Fatalf("a disconnected account kept streaming: %d %s", res.Code, res.Body)
	}
	streamConnect(t, handler, token)
	if res := request(t, handler, segmentPath, token); res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), "STREAM_RESOURCE_EXPIRED") {
		t.Fatalf("reconnected account accepted a resource of the previous connection: %d %s", res.Code, res.Body)
	}
	if res := request(t, handler, private, token); res.Code != http.StatusOK {
		t.Fatalf("reconnected account could not start playback again: %d %s", res.Code, res.Body)
	}
}

// What a session sees is private to it: it never replaces what the public record holds, and the route that
// returns it refuses anonymous callers.
func TestConnectedMetadataDoesNotLeakIntoPublicCache(t *testing.T) {
	t.Parallel()
	upstream := streamUpstream(t)
	app, handler := newApp(t, withDownloader(t, streamYouTubeDownloader(upstream.URL+"/master.m3u8")), withLoopbackMedia)
	user := testUser(t, app, "metadata@example.test")
	token := tokenOf(t, user)
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	if res := request(t, handler, "/api/video/"+video, token); res.Code != http.StatusOK {
		t.Fatalf("public metadata: %d %s", res.Code, res.Body)
	}
	streamConnect(t, handler, token)

	res := request(t, handler, "/api/video/"+video+"?source=youtube", token)
	var private platform.Video
	if err := json.Unmarshal(res.Body.Bytes(), &private); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || private.Title != "Private video" || private.Source != "youtube" || private.ID != video {
		t.Fatalf("connected metadata used the public cache: %d %s", res.Code, res.Body)
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a connected answer must not be cacheable: %v", res.Header())
	}
	if res = request(t, handler, "/api/search?q=private&source=youtube", token); res.Code != http.StatusOK {
		t.Fatalf("connected search: %d %s", res.Code, res.Body)
	}

	record, err := newStore(app).FindVideo("youtube", "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("the public record disappeared: %v", err)
	}
	if title, _ := catalog.TitlesOf(record); title != "Public video" || len(record.GetStringSlice("owners")) != 0 {
		t.Fatalf("a connected request rewrote the public record: title=%q owners=%v", title, record.GetStringSlice("owners"))
	}
	if res = anonymousRequest(t, handler, "/api/video/"+video+"?source=youtube"); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request read connected metadata: %d", res.Code)
	}
}
