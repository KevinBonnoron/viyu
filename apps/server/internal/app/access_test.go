package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestRoutesThatReachTheNetworkRequireAnAccount(t *testing.T) {
	t.Parallel()
	// A downloader that records every start: no anonymous request may ever cause one.
	marker := filepath.Join(t.TempDir(), "started")
	app, handler := newApp(t, withDownloader(t, "printf started >> "+marker+"\nexit 1"))
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	channel := storedChannel(t, app, "youtube", "UCstored", "Stored")
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/search?q=anything", ""},
		{http.MethodGet, "/api/search?q=anything&platform=all", ""},
		{http.MethodPost, "/api/resolve", `{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`},
		{http.MethodGet, "/api/video/" + video, ""},
		{http.MethodGet, "/api/video/" + video + "?source=youtube", ""},
		{http.MethodGet, "/api/video/" + video + "/url", ""},
		{http.MethodGet, "/api/video/" + video + "/comments", ""},
		{http.MethodGet, "/api/channel/" + channel, ""},
		{http.MethodGet, "/api/channel/" + channel + "?sync=1", ""},
		{http.MethodGet, "/api/channel/" + channel + "/live", ""},
		{http.MethodGet, "/api/subscriptions/live?profile=x", ""},
		{http.MethodGet, "/api/sources/youtube/feed", ""},
		{http.MethodPost, "/api/sources/youtube/watch", `{"videoId":"` + video + `","played":30,"duration":120}`},
		{http.MethodGet, "/api/chat/live/zerator", ""},
		{http.MethodGet, "/api/chat/vod/" + video, ""},
		{http.MethodGet, "/api/stream/" + video, ""},
		{http.MethodGet, "/api/stream/" + video + "/resource/abc", ""},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(anonymousHeader, "1")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized && res.Code != http.StatusForbidden {
			t.Errorf("%s %s answered an anonymous request: %d %s", route.method, route.path, res.Code, res.Body)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("an anonymous request started the downloader")
	}
	if res := anonymousRequest(t, handler, "/api/health"); res.Code != http.StatusOK {
		t.Fatalf("health must stay public: %d", res.Code)
	}
}

func TestPlaybackSessionEndpointAndCookieAreGone(t *testing.T) {
	t.Parallel()
	_, handler := newApp(t)
	_, token := viewerOf(t, handler)
	res := send(t, handler, http.MethodPost, "/api/playback-session", token, `{"videoId":"x"}`)
	if res.Code != http.StatusNotFound || len(res.Result().Cookies()) != 0 {
		t.Fatalf("the playback session endpoint still exists: %d %v", res.Code, res.Header())
	}
}

func TestEmbeddedClientIsServedWithSecurityHeaders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>viyu</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, handler := newApp(t, func(o *Options) { o.UIDir = dir })
	// An address of the single-page application is served the page, with the page's headers.
	for _, path := range []string{"/", "/watch/some-video"} {
		res := anonymousRequest(t, handler, path)
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "<title>viyu</title>") {
			t.Fatalf("%s: the client page was not served: %d %s", path, res.Code, res.Body)
		}
		header := res.Header()
		policy := header.Get("Content-Security-Policy")
		for _, directive := range []string{
			"default-src 'self'", "img-src 'self' https: data: blob:", "media-src 'self' blob:",
			"connect-src 'self' https://sponsor.ajay.app https://returnyoutubedislikeapi.com", "style-src 'self' 'unsafe-inline'",
			"frame-ancestors 'none'", "base-uri 'self'", "object-src 'none'",
		} {
			if !strings.Contains(policy, directive) {
				t.Errorf("%s: the client policy lacks %q: %s", path, directive, policy)
			}
		}
		if strings.Contains(policy, "unsafe-eval") {
			t.Errorf("%s: scripts may be evaluated: %s", path, policy)
		}
		if header.Get("Referrer-Policy") != "strict-origin-when-cross-origin" || header.Get("X-Frame-Options") != "DENY" || header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: security headers: %v", path, header)
		}
	}
	// The page's headers belong to the page: an API answer carries none of them.
	if api := anonymousRequest(t, handler, "/api/health"); api.Header().Get("Content-Security-Policy") != "" || api.Header().Get("Referrer-Policy") != "" {
		t.Errorf("the page headers leaked onto an API response: %v", api.Header())
	}
}

// The encrypted cookies of a connected source live in a collection nobody reaches through PocketBase's own
// record API, whatever the account: only the server reads it.
func TestSourceConnectionRecordsAreNotReachableThroughTheRecordAPI(t *testing.T) {
	t.Parallel()
	upstream := streamUpstream(t)
	app, handler := newApp(t, withDownloader(t, streamYouTubeDownloader(upstream.URL+"/master.m3u8")))
	user := testUser(t, app, "source-owner@example.test")
	token := tokenOf(t, user)
	streamConnect(t, handler, token)

	collection, err := app.FindCollectionByNameOrId("source_connections")
	if err != nil {
		t.Fatal(err)
	}
	if collection.ListRule != nil || collection.ViewRule != nil || collection.CreateRule != nil || collection.UpdateRule != nil || collection.DeleteRule != nil {
		t.Fatal("the source collection must be locked to backend access")
	}
	record, err := app.FindFirstRecordByFilter("source_connections", "user = {:user}", map[string]any{"user": user.Id})
	if err != nil {
		t.Fatalf("the connection was not stored: %v", err)
	}
	if secret := record.GetString("encrypted_cookies"); !strings.HasPrefix(secret, "v1.") || strings.Contains(secret, "fixture-sapisid") {
		t.Fatal("the cookies were not encrypted at rest")
	}
	for _, path := range []string{"/api/collections/source_connections/records", "/api/collections/source_connections/records/" + record.Id} {
		res := request(t, handler, path, token)
		if res.Code == http.StatusOK || strings.Contains(res.Body.String(), "encrypted_cookies") || strings.Contains(res.Body.String(), "fixture-sapisid") {
			t.Fatalf("an ordinary account read a connection record: %s %d %s", path, res.Code, res.Body)
		}
	}
	// What the account may read about its connection holds no secret.
	res := request(t, handler, "/api/sources", token)
	var listed []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil || res.Code != http.StatusOK || strings.Contains(res.Body.String(), "encrypted_cookies") || strings.Contains(res.Body.String(), "fixture-sapisid") {
		t.Fatalf("the source catalog: %d %s %v", res.Code, res.Body, err)
	}
}
