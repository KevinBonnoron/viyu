package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/twitch"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

// libSourceAnswer asserts what every answer of a source route has in common: it is private to the account
// and never shows what the account imported.
func libSourceAnswer(t *testing.T, res *httptest.ResponseRecorder) {
	t.Helper()
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a personal source answer is cacheable: %v", res.Header())
	}
	for _, secret := range []string{"private-cookie-value", "viyu-source-secret"} {
		if strings.Contains(res.Body.String(), secret) {
			t.Fatalf("a source answer leaked private data: %s", res.Body)
		}
	}
}

func TestSourceRoutesConnectCheckFeedWatchDisconnectAndOwnership(t *testing.T) {
	t.Parallel()
	downloader := newLibDownloader(t)
	app, handler := libApp(t, downloader.option())
	owner, other := testUser(t, app, "api-source-owner@example.test"), testUser(t, app, "api-source-other@example.test")
	ownerToken, otherToken := tokenOf(t, owner), tokenOf(t, other)

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/sources"},
		{http.MethodPut, "/api/sources/youtube/activation"},
		{http.MethodPost, "/api/sources/youtube/connect"},
		{http.MethodPost, "/api/sources/youtube/check"},
		{http.MethodGet, "/api/sources/youtube/feed?kind=home"},
		{http.MethodPost, "/api/sources/youtube/watch"},
		{http.MethodDelete, "/api/sources/youtube"},
	} {
		if res := libDo(t, handler, route.method, route.path, "", nil); res.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous access allowed for %s %s: %d", route.method, route.path, res.Code)
		}
	}

	res := request(t, handler, "/api/sources", ownerToken)
	libSourceAnswer(t, res)
	catalog := libOK[[]libSource](t, res)
	if len(catalog) != 3 || catalog[0].Platform != "youtube" || catalog[1].Platform != "twitch" || catalog[2].Platform != "peertube" {
		t.Fatalf("the catalog lists the platforms in registry order: %+v", catalog)
	}
	for _, source := range catalog {
		if source.Connected || source.Name == "" {
			t.Fatalf("nothing is connected yet: %+v", source)
		}
	}
	res = request(t, handler, "/api/sources/youtube/feed?kind=home", ownerToken)
	libFail(t, res, http.StatusConflict, "SOURCE_NOT_CONNECTED")
	libSourceAnswer(t, res)

	res = libDo(t, handler, http.MethodPost, "/api/sources/youtube/connect", ownerToken, map[string]string{"cookies": libCookies("private-cookie-value", 0), "label": "Browser session"})
	libSourceAnswer(t, res)
	var connection map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &connection); err != nil || res.Code != http.StatusOK {
		t.Fatalf("connect: %d %s %v", res.Code, res.Body, err)
	}
	for _, field := range []string{"cookies", "encrypted_cookies"} {
		if _, exposed := connection[field]; exposed {
			t.Fatalf("the connection answer contains the credential field %q", field)
		}
	}
	if string(connection["label"]) != `"Browser session"` || string(connection["connected"]) != "true" {
		t.Fatalf("connection answer: %s", res.Body)
	}

	res = libDo(t, handler, http.MethodPost, "/api/sources/youtube/check", ownerToken, nil)
	libSourceAnswer(t, res)
	if source := libOK[libSource](t, res); !source.Connected || source.Platform != "youtube" {
		t.Fatalf("check answer: %+v", source)
	}

	res = request(t, handler, "/api/sources/youtube/feed?kind=history&page=1&limit=2", ownerToken)
	libSourceAnswer(t, res)
	feed := libOK[platform.Feed](t, res)
	if !feed.HasMore || len(feed.Videos) != 2 || feed.Videos[0].Source != "youtube" {
		t.Fatalf("connected feed shape: %+v", feed)
	}
	// Connected feed videos are stored like any other listing, so each one is addressed by its record id.
	for _, video := range feed.Videos {
		if video.RecordID == "" || video.ID != video.RecordID {
			t.Fatalf("a connected feed video is not addressed by its record id: %+v", video)
		}
		if record, err := app.FindRecordById("videos", video.ID); err != nil || record.GetString("platform_id") != "abcdefghijk" {
			t.Fatalf("a connected feed video has no record: %v", err)
		}
	}
	if args := downloader.arguments(t); strings.Contains(args, " --mark-watched ") {
		t.Fatalf("read-only source requests marked videos: %s", args)
	}

	// Another account has no connection of its own: the owner's is never used for it, and dropping a
	// connection it does not have leaves the owner's alone.
	libFail(t, request(t, handler, "/api/sources/youtube/feed?kind=history", otherToken), http.StatusConflict, "SOURCE_NOT_CONNECTED")
	libFail(t, libDo(t, handler, http.MethodDelete, "/api/sources/youtube", otherToken, nil), http.StatusNoContent, "")
	if !libSources(t, handler, ownerToken)["youtube"].Connected {
		t.Fatal("another account's disconnect ended the owner's session")
	}
	if libSources(t, handler, otherToken)["youtube"].Connected {
		t.Fatal("the owner's session is shown as the other account's")
	}

	watched := storedVideo(t, app, platform.Video{ID: "abcdefghijk"})
	watch := func(played float64) *httptest.ResponseRecorder {
		return libDo(t, handler, http.MethodPost, "/api/sources/youtube/watch", ownerToken, map[string]any{"videoId": watched, "playedSeconds": played, "durationSeconds": 120})
	}
	libFail(t, watch(10), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	res = watch(30)
	libSourceAnswer(t, res)
	if got := libOK[map[string]string](t, res); got["status"] != "marked" {
		t.Fatalf("watch answer: %s", res.Body)
	}
	if args := downloader.arguments(t); !strings.Contains(args, " --mark-watched ") {
		t.Fatalf("watching never marked the video on the platform: %s", args)
	}

	// What the platform says is classified: a refused session is the account's to fix, anything else is a
	// failure of the platform, and neither shows what the downloader printed.
	downloader.mode(t, "auth_error")
	res = request(t, handler, "/api/sources/youtube/feed?kind=home", ownerToken)
	libFail(t, res, http.StatusUnauthorized, "SOURCE_SESSION_INVALID")
	libSourceAnswer(t, res)
	downloader.mode(t, "failure")
	res = request(t, handler, "/api/sources/youtube/feed?kind=home", ownerToken)
	libFail(t, res, http.StatusBadGateway, "SOURCE_REQUEST_FAILED")
	libSourceAnswer(t, res)
	downloader.mode(t, "")

	libFail(t, libDo(t, handler, http.MethodDelete, "/api/sources/youtube", ownerToken, nil), http.StatusNoContent, "")
	libFail(t, libDo(t, handler, http.MethodPost, "/api/sources/youtube/check", ownerToken, nil), http.StatusConflict, "SOURCE_NOT_CONNECTED")
	downloader.assertJarsRemoved(t)
}

func TestSourceRoutesRefuseInvalidInputWithoutStartingTheDownloader(t *testing.T) {
	t.Parallel()
	downloader := newLibDownloader(t)
	app, handler := libApp(t, downloader.option())
	token := tokenOf(t, testUser(t, app, "api-source-input@example.test"))

	for _, path := range []string{
		"/api/sources/youtube/feed?kind=home&page=0",
		"/api/sources/youtube/feed?kind=home&page=-1",
		"/api/sources/youtube/feed?kind=home&limit=51",
		"/api/sources/youtube/feed?kind=home&limit=bad",
		"/api/sources/youtube/feed?kind=home&page=bad",
	} {
		// An account that is not connected would answer 409: the input is judged before the connection.
		libFail(t, request(t, handler, path, token), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	}
	for name, cookies := range map[string]string{"not a cookie file": "not a cookie file", "too large": strings.Repeat("x", 400<<10), "empty": ""} {
		res := libDo(t, handler, http.MethodPost, "/api/sources/youtube/connect", token, map[string]string{"cookies": cookies})
		libFail(t, res, http.StatusBadRequest, "INVALID_SOURCE_INPUT")
		if t.Failed() {
			t.Fatalf("cookies: %s", name)
		}
	}
	libFail(t, libDo(t, handler, http.MethodPost, "/api/sources/unspecified/connect", token, map[string]string{"cookies": libCookies("private-cookie-value", 0)}), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	// A platform with no account to import says so.
	libFail(t, libDo(t, handler, http.MethodPost, "/api/sources/peertube/connect", token, map[string]string{"cookies": libCookies("private-cookie-value", 0)}), http.StatusBadRequest, "SOURCE_NOT_CONNECTABLE")

	watched := storedVideo(t, app, platform.Video{ID: "abcdefghijk"})
	for _, body := range []string{
		`{"videoId":"` + watched + `","playedSeconds":NaN,"durationSeconds":120}`,
		`{"videoId":"` + watched + `","playedSeconds":30,"durationSeconds":0}`,
		`{"videoId":"` + watched + `","playedSeconds":30,"durationSeconds":120,"unexpected":true}`,
		`{"videoId":"` + watched + `","playedSeconds":30,"durationSeconds":120} {}`,
		`{"videoId":"never-stored","playedSeconds":30,"durationSeconds":120}`,
		`{"playedSeconds":30,"durationSeconds":120}`,
	} {
		libFail(t, send(t, handler, http.MethodPost, "/api/sources/youtube/watch", token, body), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	}
	// A video of another platform is no video of this source.
	other := storedVideo(t, app, platform.Video{ID: "123", Platform: "twitch", ChannelID: "zerator", ChannelName: "ZeratoR"})
	libFail(t, libDo(t, handler, http.MethodPost, "/api/sources/youtube/watch", token, map[string]any{"videoId": other, "playedSeconds": 30, "durationSeconds": 120}), http.StatusBadRequest, "INVALID_SOURCE_INPUT")

	if args := downloader.arguments(t); args != "" {
		t.Fatalf("invalid source inputs started a downloader: %s", args)
	}
}

func TestSourceActivationDefaultsToThePlatformsChoiceAndBelongsToTheAccount(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "activation-owner@example.test"), testUser(t, app, "activation-other@example.test")
	ownerToken, otherToken := tokenOf(t, owner), tokenOf(t, other)

	libFail(t, libDo(t, handler, http.MethodPut, "/api/sources/twitch/activation", "", map[string]bool{"enabled": false}), http.StatusUnauthorized, "")
	for platformID, source := range libSources(t, handler, ownerToken) {
		// YouTube and Twitch are on until chosen otherwise; PeerTube is opt-in.
		if source.Enabled != (platformID != "peertube") || source.Connected {
			t.Fatalf("%s starts with its default and unconnected: %+v", platformID, source)
		}
	}

	res := libActivate(t, handler, ownerToken, "twitch", false)
	if changed := libOK[libSource](t, res); changed.Platform != "twitch" || changed.Enabled {
		t.Fatalf("activation answer: %+v", changed)
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the choice of an account is cacheable: %v", res.Header())
	}
	owned := libSources(t, handler, ownerToken)
	if owned["twitch"].Enabled || !owned["youtube"].Enabled {
		t.Fatalf("only Twitch is off for its owner: %+v", owned)
	}
	if elsewhere := libSources(t, handler, otherToken); !elsewhere["twitch"].Enabled {
		t.Fatalf("another account's choice leaked: %+v", elsewhere)
	}

	// Choosing again updates the same choice instead of adding a second one.
	libFail(t, libActivate(t, handler, ownerToken, "twitch", true), http.StatusOK, "")
	if !libSources(t, handler, ownerToken)["twitch"].Enabled {
		t.Fatal("the source was not turned back on")
	}
	if rows, err := app.FindRecordsByFilter(sources.ActivationsCollection, "user = {:user}", "", 0, 0, map[string]any{"user": owner.Id}); err != nil || len(rows) != 1 {
		t.Fatalf("one row per account and platform: %d %v", len(rows), err)
	}
	// PeerTube is opt-in: turning it on is the account's own choice.
	libFail(t, libActivate(t, handler, ownerToken, "peertube", true), http.StatusOK, "")
	if !libSources(t, handler, ownerToken)["peertube"].Enabled || libSources(t, handler, otherToken)["peertube"].Enabled {
		t.Fatal("PeerTube was not turned on for its account alone")
	}

	libFail(t, libActivate(t, handler, ownerToken, "vimeo", true), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	libFail(t, libDo(t, handler, http.MethodPut, "/api/sources/twitch/activation", ownerToken, map[string]string{}), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	libFail(t, libDo(t, handler, http.MethodPut, "/api/sources/twitch/activation", ownerToken, map[string]any{"enabled": true, "platform": "youtube"}), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
}

// libCountedTwitch answers public Twitch searches with one VOD and counts the requests it receives.
func libCountedTwitch(t *testing.T) (func(*Options), *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"searchFor": map[string]any{
			"channels": map[string]any{"cursor": "", "edges": []any{}},
			"videos": map[string]any{"cursor": "", "edges": []any{map[string]any{"item": map[string]any{
				"id": "100", "title": "VOD", "createdAt": "2026-11-06T12:30:00Z", "lengthSeconds": 60, "viewCount": 1,
				"previewThumbnailURL": "https://static.example/vod.jpg",
				"owner":               map[string]string{"login": "creator", "displayName": "Creator", "profileImageURL": "https://static.example/avatar.jpg"},
			}}}},
		}}})
	}))
	t.Cleanup(server.Close)
	return func(o *Options) {
		o.Twitch = twitch.Options{
			HTTP: server.Client(), ValidateURL: server.URL, APIURL: server.URL, GraphQLURL: server.URL,
			IRCDial: o.Twitch.IRCDial,
		}
	}, &requests
}

// libCountedYouTube is a downloader that answers a YouTube search with one video and counts how often it
// was started.
func libCountedYouTube(t *testing.T) (func(*Options), func() int) {
	t.Helper()
	binary := testutil.Script(t, "yt-dlp", `echo call >> "$(dirname "$0")/calls"
case "$*" in
  *ytsearch*) printf '%s\n' '{"id":"abcdefghijk","title":"YouTube result","channel_id":"UCfixture0000000000000a","channel":"Fixture"}' ;;
  *results?search_query*) exit 0 ;;
  *) echo 'unexpected downloader call' >&2; exit 1 ;;
esac`)
	calls := func() int {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "calls"))
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "call")
	}
	return libBinary(binary), calls
}

func TestSearchAllNeverQueriesAPlatformTheAccountTurnedOff(t *testing.T) {
	t.Parallel()
	youtube, youtubeCalls := libCountedYouTube(t)
	twitchOption, twitchCalls := libCountedTwitch(t)
	app, handler := libApp(t, youtube, twitchOption)
	token := tokenOf(t, testUser(t, app, "search-activation@example.test"))
	platformsOf := func(authorization string) (int, []string) {
		// The query names no channel, so the answer is the platforms' videos only (no channel card is added).
		res := request(t, handler, "/api/search?q=episode&platform=all", authorization)
		var body struct {
			Results []map[string]any `json:"results"`
		}
		if res.Code == http.StatusOK {
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
		}
		var found []string
		for _, result := range body.Results {
			found = append(found, result["platform"].(string))
			// Every result is addressed by its record id.
			if _, err := app.FindRecordById("videos", result["id"].(string)); err != nil {
				t.Fatalf("a search result is not addressed by its record id: %v", result)
			}
		}
		return res.Code, found
	}

	if status, found := platformsOf(token); status != http.StatusOK || strings.Join(found, ",") != "youtube,twitch" {
		t.Fatalf("both platforms by default: %d %v", status, found)
	}

	libFail(t, libActivate(t, handler, token, "twitch", false), http.StatusOK, "")
	youtubeBefore, twitchBefore := youtubeCalls(), twitchCalls.Load()
	if status, found := platformsOf(token); status != http.StatusOK || strings.Join(found, ",") != "youtube" {
		t.Fatalf("twitch is off: %d %v", status, found)
	}
	if twitchCalls.Load() != twitchBefore {
		t.Fatalf("Twitch was queried %d time(s) while turned off", twitchCalls.Load()-twitchBefore)
	}

	libFail(t, libActivate(t, handler, token, "youtube", false), http.StatusOK, "")
	youtubeBefore = youtubeCalls()
	if status, found := platformsOf(token); status != http.StatusOK || len(found) != 0 {
		t.Fatalf("nothing is on: %d %v", status, found)
	}
	if youtubeCalls() != youtubeBefore || twitchCalls.Load() != twitchBefore {
		t.Fatal("a search with every platform off still queried a platform")
	}

	// Searching needs an account: nothing is queried on nobody's behalf.
	if res := anonymousRequest(t, handler, "/api/search?q=fixture&platform=all"); res.Code != http.StatusUnauthorized || youtubeCalls() != youtubeBefore {
		t.Fatalf("anonymous search: %d (a platform was queried: %v)", res.Code, youtubeCalls() != youtubeBefore)
	}
}

func TestADisabledSourceRefusesRequestsAboutItWithoutReachingIt(t *testing.T) {
	t.Parallel()
	youtube, youtubeCalls := libCountedYouTube(t)
	twitchOption, twitchCalls := libCountedTwitch(t)
	app, handler := libApp(t, youtube, twitchOption)
	user := testUser(t, app, "refuse-activation@example.test")
	profile := profileOf(t, app, user)
	token := tokenOf(t, user)
	twitchVideo := storedVideo(t, app, platform.Video{ID: "123", Platform: "twitch", ChannelID: "zerator", ChannelName: "ZeratoR"})
	twitchChannel := recordIDOf(t, app, "channels", "zerator", "twitch")
	libFail(t, libActivate(t, handler, token, "twitch", false), http.StatusOK, "")
	twitchBefore := twitchCalls.Load()

	for _, path := range []string{
		"/api/search?q=fixture&platform=twitch",
		"/api/video/" + twitchVideo,
		"/api/video/" + twitchVideo + "/url",
		"/api/channel/" + twitchChannel,
		"/api/channel/" + twitchChannel + "/live",
		"/api/stream/" + twitchVideo,
	} {
		libFail(t, request(t, handler, path, token), http.StatusConflict, "SOURCE_DISABLED")
	}
	libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions", token, map[string]string{"profile": profile.Id, "channelId": twitchChannel}), http.StatusConflict, "SOURCE_DISABLED")
	if twitchCalls.Load() != twitchBefore {
		t.Fatalf("Twitch was reached %d time(s) for a request it should never have seen", twitchCalls.Load()-twitchBefore)
	}

	before := youtubeCalls()
	if res := request(t, handler, "/api/search?q=fixture&platform=youtube", token); res.Code != http.StatusOK {
		t.Fatalf("YouTube is still on: status=%d body=%s", res.Code, res.Body)
	}
	if youtubeCalls() == before {
		t.Fatal("an enabled platform was not queried")
	}
}

func TestConnectionNeedsAnEnabledSourceAndSurvivesTurningItOff(t *testing.T) {
	t.Parallel()
	downloader := newLibDownloader(t)
	app, handler := libApp(t, downloader.option())
	token := tokenOf(t, testUser(t, app, "connection-activation@example.test"))
	video := storedVideo(t, app, platform.Video{ID: "abcdefghijk"})
	cookies := map[string]string{"cookies": libCookies("private-cookie-value", 0), "label": "Browser session"}

	libFail(t, libDo(t, handler, http.MethodPost, "/api/sources/youtube/connect", token, cookies), http.StatusOK, "")
	if source := libSources(t, handler, token)["youtube"]; !source.Connected || !source.Enabled {
		t.Fatalf("connected and enabled: %+v", source)
	}

	libFail(t, libActivate(t, handler, token, "youtube", false), http.StatusOK, "")
	source := libSources(t, handler, token)["youtube"]
	if !source.Connected || source.Enabled || source.ConnectionID == "" {
		t.Fatalf("turning a source off keeps its connection: %+v", source)
	}
	calls := downloader.arguments(t)
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/sources/youtube/connect", cookies},
		{http.MethodPost, "/api/sources/youtube/check", nil},
		{http.MethodGet, "/api/sources/youtube/feed?kind=home", nil},
		{http.MethodPost, "/api/sources/youtube/watch", map[string]any{"videoId": video, "playedSeconds": 30, "durationSeconds": 120}},
		// A connected request is refused too, not only the source routes, and so is the stream itself.
		{http.MethodGet, "/api/video/" + video + "?source=youtube", nil},
		{http.MethodGet, "/api/stream/" + video, nil},
		{http.MethodGet, "/api/stream/" + video + "?source=youtube", nil},
	} {
		res := libDo(t, handler, route.method, route.path, token, route.body)
		if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), `"code":"SOURCE_DISABLED"`) {
			t.Fatalf("%s %s on a disabled source: status=%d body=%s", route.method, route.path, res.Code, res.Body)
		}
	}
	if downloader.arguments(t) != calls {
		t.Fatal("a disabled source still ran the downloader")
	}

	libFail(t, libActivate(t, handler, token, "youtube", true), http.StatusOK, "")
	libFail(t, libDo(t, handler, http.MethodPost, "/api/sources/youtube/check", token, nil), http.StatusOK, "")

	// The stored credentials can still be dropped while the source is off.
	libFail(t, libActivate(t, handler, token, "youtube", false), http.StatusOK, "")
	libFail(t, libDo(t, handler, http.MethodDelete, "/api/sources/youtube", token, nil), http.StatusNoContent, "")
	if source = libSources(t, handler, token)["youtube"]; source.Connected || source.Enabled {
		t.Fatalf("disconnected and still off: %+v", source)
	}
}

// libLiveTwitch answers Twitch's live lookup: every channel is on air. It counts its requests.
func libLiveTwitch(t *testing.T) (func(*Options), *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"user":{"stream":{"id":"1","title":"Speedrun","viewersCount":321,"game":{"name":"wow","displayName":"World of Warcraft"}}}}}`)
	}))
	t.Cleanup(server.Close)
	return func(o *Options) {
		o.Twitch = twitch.Options{HTTP: server.Client(), ValidateURL: server.URL, APIURL: server.URL, GraphQLURL: server.URL, IRCDial: o.Twitch.IRCDial}
	}, &requests
}

func TestSubscriptionsLiveSkipsPlatformsTheAccountTurnedOff(t *testing.T) {
	t.Parallel()
	liveChannel := libChannelID("live")
	binary := testutil.Script(t, "yt-dlp", `echo call >> "$(dirname "$0")/calls"
case "$*" in
 *channel/`+liveChannel+`/streams*) printf '%s\n' '{"_type":"playlist","id":"`+liveChannel+`","entries":[{"id":"livestream1","title":"On air","live_status":"is_live","view_count":55}]}' ;;
 *) echo 'unexpected YouTube fixture URL' >&2; exit 9 ;;
esac`)
	youtubeCalls := func() int {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "calls"))
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "call")
	}
	twitchOption, twitchRequests := libLiveTwitch(t)
	app, handler := libApp(t, libBinary(binary), twitchOption)
	owner := testUser(t, app, "live-activation@example.test")
	profile := libProfile(t, app, owner, "Live")
	token := tokenOf(t, owner)
	libSubscribe(t, app, profile, "twitch", "zerator", "ZeratoR")
	libSubscribe(t, app, profile, "youtube", liveChannel, "On air")
	path := "/api/subscriptions/live?profile=" + profile.Id
	platformsOf := func() []string {
		t.Helper()
		var found []string
		for _, stream := range libOK[[]struct {
			Platform string `json:"platform"`
		}](t, request(t, handler, path, token)) {
			found = append(found, stream.Platform)
		}
		return found
	}

	libFail(t, libActivate(t, handler, token, "twitch", false), http.StatusOK, "")
	if got := platformsOf(); strings.Join(got, ",") != "youtube" {
		t.Fatalf("only the enabled platform can be live: %v", got)
	}
	if twitchRequests.Load() != 0 {
		t.Fatalf("Twitch was asked %d time(s) about a disabled source", twitchRequests.Load())
	}

	// And the other way around: the platform that is on again is asked, the one that is off is not.
	libFail(t, libActivate(t, handler, token, "twitch", true), http.StatusOK, "")
	libFail(t, libActivate(t, handler, token, "youtube", false), http.StatusOK, "")
	youtubeBefore := youtubeCalls()
	if got := platformsOf(); strings.Join(got, ",") != "twitch" {
		t.Fatalf("only the enabled platform can be live: %v", got)
	}
	if youtubeCalls() != youtubeBefore {
		t.Fatal("YouTube was asked about a disabled source")
	}
}

func TestAccountTransferCarriesSourceChoicesAndSkipsUnknownPlatforms(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	from, to := tokenOf(t, testUser(t, app, "transfer-from@example.test")), tokenOf(t, testUser(t, app, "transfer-to@example.test"))
	libFail(t, libActivate(t, handler, from, "twitch", false), http.StatusOK, "")

	exported := libOK[map[string]any](t, request(t, handler, "/api/account/export", from))
	data := exported["data"].(map[string]any)
	if choices, _ := data["sources"].(map[string]any); len(choices) != 1 || choices["twitch"] != false {
		t.Fatalf("only the choice that was made is exported: %v", data["sources"])
	}

	data["sources"] = map[string]bool{"twitch": false, "vimeo": true}
	libFail(t, libDo(t, handler, http.MethodPost, "/api/account/import", to, exported), http.StatusOK, "")
	if catalog := libSources(t, handler, to); catalog["twitch"].Enabled || !catalog["youtube"].Enabled || len(catalog) != 3 {
		t.Fatalf("imported choices: %+v", catalog)
	}

	// An export from before sources could be turned off carries no choice and changes none.
	delete(data, "sources")
	libFail(t, libDo(t, handler, http.MethodPost, "/api/account/import", to, exported), http.StatusOK, "")
	if catalog := libSources(t, handler, to); catalog["twitch"].Enabled {
		t.Fatalf("an export without choices must not turn a source back on: %+v", catalog)
	}
}
