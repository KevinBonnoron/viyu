package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// contentVideoLines is count yt-dlp flat-listing entries of one creator, numbered from first.
func contentVideoLines(first, count int) string {
	var lines []string
	for index := first; index < first+count; index++ {
		line, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("video%06d", index), "title": fmt.Sprintf("Video %d", index), "channel_id": "UCcreator", "channel": "Creator", "duration": 60})
		lines = append(lines, string(line))
	}
	return strings.Join(lines, "\n")
}

// contentTwitchSearch answers Twitch's public GraphQL search with vods VODs and no channel, or fails it. A
// request that carried credentials, or not the public client, is refused: a search without an account must
// never present one.
func contentTwitchSearch(vods int, failing bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if failing || r.Header.Get("Client-Id") != contentTwitchID || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		edges := []any{}
		for i := range vods {
			edges = append(edges, map[string]any{"item": map[string]any{
				"id": fmt.Sprintf("10%d", i), "title": "VOD", "createdAt": "2026-11-06T12:30:00Z", "lengthSeconds": 60, "viewCount": 1,
				"previewThumbnailURL": "https://static.example/vod.jpg",
				"owner":               map[string]string{"login": "creator", "displayName": "Creator", "profileImageURL": "https://static.example/avatar.jpg"},
			}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"searchFor": map[string]any{
			"channels": map[string]any{"cursor": "", "edges": []any{}},
			"videos":   map[string]any{"cursor": "", "edges": edges},
		}}})
	}
}

func TestSearchStoresEveryResultAndAddressesItByRecordID(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, fixtureDownloaderBody)
	app, handler := newApp(t, option)
	for _, path := range []string{"/api/search", "/api/search?q="} {
		contentExpectError(t, request(t, handler, path, ""), http.StatusBadRequest, "INVALID_REQUEST")
	}

	// The query does not name the fixture channel, so no channel card is added in front of its video.
	res := request(t, handler, "/api/search?q=clip", "")
	page := contentDecode[contentPage](t, res)
	if page.Page != 1 || page.HasMore || len(page.Results) != 1 {
		t.Fatalf("search shape: %+v", page)
	}
	video, channel := recordIDOf(t, app, "videos", "fixture-video", "youtube"), recordIDOf(t, app, "channels", "UCfixture", "youtube")
	result := page.Results[0]
	if result["id"] != video || result["recordId"] != video || result["channelId"] != channel || result["channelRecordId"] != channel {
		t.Fatalf("the result is not addressed by record ids: %v", result)
	}
	if result["originalTitle"] != "Original fixture" || result["published"] != "2026-01-02" || result["platform"] != "youtube" {
		t.Fatalf("search result: %v", result)
	}
	if body := res.Body.String(); strings.Contains(body, "fixture-video") || strings.Contains(body, "UCfixture") {
		t.Fatalf("a native id left the server: %s", body)
	}
}

func TestSearchPromotesTheExactChannelAndContinuesWithoutLosingTheLookahead(t *testing.T) {
	t.Parallel()
	channel, _ := json.Marshal(map[string]any{"_type": "url", "id": "UCego", "ie_key": "YoutubeTab", "channel": "EGO", "channel_id": "UCego", "url": "https://www.youtube.com/channel/UCego", "thumbnails": []map[string]any{{"url": "//yt3.ggpht.com/ego=s176-c", "width": 176, "height": 176}}})
	// The first page asks for one entry more than it shows: 20 videos, the channel among them.
	lines := strings.Split(contentVideoLines(1, 20), "\n")
	first := strings.Join(append([]string{lines[0], string(channel)}, lines[1:]...), "\n")
	dir := t.TempDir()
	for name, content := range map[string]string{"channel": string(channel), "first": first, "second": contentVideoLines(20, 2)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	option := withDownloader(t, `case "$*" in
 *'search_query='*) cat '`+filepath.Join(dir, "channel")+`';;
 *'--playlist-start 21'*) cat '`+filepath.Join(dir, "second")+`';;
 *) cat '`+filepath.Join(dir, "first")+`';;
esac`)
	app, handler := newApp(t, option)

	firstPage := contentDecode[contentPage](t, request(t, handler, "/api/search?q=eGo", ""))
	if firstPage.Page != 1 || !firstPage.HasMore || len(firstPage.Results) != 20 {
		t.Fatalf("first page: %+v", firstPage)
	}
	if got := firstPage.Results[0]; got["id"] != recordIDOf(t, app, "channels", "UCego", "youtube") || got["thumbnail"] != "https://yt3.googleusercontent.com/ego=s176-c" {
		t.Fatalf("the exact channel was missing, duplicated or not promoted: %v", got)
	}
	for index, result := range firstPage.Results[1:] {
		if result["id"] != recordIDOf(t, app, "videos", fmt.Sprintf("video%06d", index+1), "youtube") {
			t.Fatalf("video relevance order changed at %d: %v", index, result)
		}
	}

	next := contentDecode[contentPage](t, request(t, handler, "/api/search?q=eGo&page=2", ""))
	if next.Page != 2 || next.HasMore || len(next.Results) != 2 || next.Results[0]["id"] != recordIDOf(t, app, "videos", "video000020", "youtube") || next.Results[1]["id"] != recordIDOf(t, app, "videos", "video000021", "youtube") {
		t.Fatalf("lost the lookahead result or fabricated more pages: %+v", next)
	}

	for _, page := range []string{"0", "-1", "51", "abc", "999999999999999999999"} {
		if res := request(t, handler, "/api/search?q=eGo&page="+page, ""); res.Code != http.StatusBadRequest {
			t.Fatalf("invalid page %s: %d %s", page, res.Code, res.Body)
		}
	}
}

func TestSearchOfOnePlatformAsksOnlyThatPlatform(t *testing.T) {
	t.Parallel()
	downloader, calls := contentDownloader(t, fixtureDownloaderBody)
	twitch, twitchRequests := contentTwitchServer(t, contentTwitchSearch(2, false))
	_, handler := newApp(t, downloader, twitch)
	youtubeSearch := contentDecode[contentPage](t, request(t, handler, "/api/search?q=fixture&platform=youtube", ""))
	downloaderCalls := len(calls.all())
	if len(youtubeSearch.Results) == 0 || twitchRequests.Load() != 0 {
		t.Fatalf("the YouTube search reached Twitch: %+v, %d requests", youtubeSearch, twitchRequests.Load())
	}
	twitchSearch := contentDecode[contentPage](t, request(t, handler, "/api/search?q=fixture&platform=twitch", ""))
	if len(twitchSearch.Results) != 2 || len(calls.all()) != downloaderCalls {
		t.Fatalf("the Twitch search reached the downloader: %+v, %d new calls", twitchSearch, len(calls.all())-downloaderCalls)
	}
	for platform, page := range map[string]contentPage{"youtube": youtubeSearch, "twitch": twitchSearch} {
		for _, result := range page.Results {
			if result["platform"] != platform {
				t.Fatalf("a %s search returned a %v result", platform, result["platform"])
			}
		}
	}
	contentExpectError(t, request(t, handler, "/api/search?q=fixture&platform=vimeo", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
}

func TestSearchAllInterleavesEveryPublicPlatform(t *testing.T) {
	t.Parallel()
	downloader, _ := contentDownloader(t, fixtureDownloaderBody)
	twitch, _ := contentTwitchServer(t, contentTwitchSearch(3, false))
	app, handler := newApp(t, downloader, twitch)

	page := contentDecode[contentPage](t, request(t, handler, "/api/search?q=clip&platform=all", ""))
	if len(page.Results) != 4 {
		t.Fatalf("results: %+v", page)
	}
	// The downloader finds one YouTube video; Twitch finds three. Alternating puts each platform's best
	// result first instead of letting one platform bury the other.
	for index, want := range []string{"youtube", "twitch", "twitch", "twitch"} {
		if page.Results[index]["platform"] != want {
			t.Fatalf("results are not interleaved: %v", contentPlatformsOf(page))
		}
	}
	want := recordIDOf(t, app, "videos", "fixture-video", "youtube")
	if first := page.Results[0]; first["id"] != want || first["recordId"] != want {
		t.Fatalf("unexpected first result: %v", first)
	}
}

func TestSearchAllSurvivesOnePlatformFailingButNotEveryOne(t *testing.T) {
	t.Parallel()
	downloader, _ := contentDownloader(t, fixtureDownloaderBody)
	twitch, _ := contentTwitchServer(t, contentTwitchSearch(0, true))
	_, handler := newApp(t, downloader, twitch)
	page := contentDecode[contentPage](t, request(t, handler, "/api/search?q=clip&platform=all", ""))
	if len(page.Results) != 1 || page.Results[0]["platform"] != "youtube" {
		t.Fatalf("a failing platform hid the other: %+v", page)
	}

	broken, _ := contentTwitchServer(t, contentTwitchSearch(0, true))
	_, failing := newApp(t, withDownloader(t, "echo 'secret downloader output' >&2\nexit 1"), broken)
	res := request(t, failing, "/api/search?q=fixture&platform=all", "")
	contentExpectError(t, res, http.StatusBadGateway, "SOURCE_REQUEST_FAILED")
	if strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("the downloader's output reached the client: %s", res.Body)
	}
}

func TestSearchOfAPlatformTheAccountTurnedOffIsRefusedWithoutAskingIt(t *testing.T) {
	t.Parallel()
	twitch, twitchRequests := contentTwitchServer(t, contentTwitchSearch(2, false))
	_, handler := newApp(t, twitch)
	_, token := viewerOf(t, handler)
	if res := send(t, handler, http.MethodPut, "/api/sources/twitch/activation", token, `{"enabled":false}`); res.Code != http.StatusOK {
		t.Fatalf("turning Twitch off: %d %s", res.Code, res.Body)
	}
	contentExpectError(t, request(t, handler, "/api/search?q=fixture&platform=twitch", ""), http.StatusConflict, "SOURCE_DISABLED")
	if twitchRequests.Load() != 0 {
		t.Fatalf("Twitch was asked for a source that is turned off")
	}
}
