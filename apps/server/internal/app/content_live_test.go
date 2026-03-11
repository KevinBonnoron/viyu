package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// contentTwitchLive answers Twitch's live lookup: a login that is a key of live is known, and live when true;
// the others are unknown channels. Unless working says otherwise the whole service is down.
func contentTwitchLive(live map[string]bool, working *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		if working != nil && !working.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var body struct {
			Variables struct {
				Login string `json:"login"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(payload, &body)
		w.Header().Set("Content-Type", "application/json")
		switch isLive, known := live[body.Variables.Login]; {
		case !known:
			_, _ = io.WriteString(w, `{"data":{"user":null}}`)
		case isLive:
			_, _ = io.WriteString(w, `{"data":{"user":{"stream":{"id":"1","title":"Speedrun","viewersCount":321,"game":{"name":"wow","displayName":"World of Warcraft"}}}}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"user":{"stream":null}}}`)
		}
	}
}

// contentStreamsDownloader answers the Streams tab of three YouTube channels: one on air, one quiet, one that
// has no such tab. Anything else is an unexpected downloader failure.
const contentStreamsDownloader = `case "$*" in
 *channel/UClive0000000000000000000/streams*) printf '%s\n' '{"_type":"playlist","id":"UClive0000000000000000000","entries":[{"id":"oldstream01","title":"Ended","live_status":"was_live","view_count":9},{"id":"livestream1","title":"On air","live_status":"is_live","view_count":55}]}' ;;
 *channel/UCquiet000000000000000000/streams*) printf '%s\n' '{"_type":"playlist","id":"UCquiet000000000000000000","entries":[{"id":"oldstream01","title":"Ended","live_status":"was_live"}]}' ;;
 *channel/UCnotab000000000000000000/streams*) echo 'ERROR: [youtube:tab] UCnotab: This channel does not have a streams tab' >&2; exit 1 ;;
 *) echo 'unexpected YouTube fixture URL' >&2; exit 9 ;;
esac`

const (
	contentLiveChannel  = "UClive0000000000000000000"
	contentQuietChannel = "UCquiet000000000000000000"
	contentNoTabChannel = "UCnotab000000000000000000"
)

func TestTwitchChannelLiveRoute(t *testing.T) {
	t.Parallel()
	twitch, requests := contentTwitchServer(t, contentTwitchLive(map[string]bool{"zerator": true, "xqc": false}, nil))
	app, handler := newApp(t, twitch)
	get := func(channel string) contentLive {
		return contentDecode[contentLive](t, request(t, handler, "/api/channel/"+storedChannel(t, app, "twitch", channel, "")+"/live", ""))
	}

	stream := get("zerator")
	if !stream.Live || stream.Title != "Speedrun" || stream.Viewers != 321 || stream.Category != "World of Warcraft" || stream.RecordID != recordIDOf(t, app, "videos", "zerator", "twitch") {
		t.Fatalf("live channel: %+v", stream)
	}
	if offline := get("xqc"); offline.Live || offline.RecordID != "" {
		t.Fatalf("offline channel: %+v", offline)
	}
	if unknown := get("nobody"); unknown.Live {
		t.Fatalf("an unknown channel is simply not live: %+v", unknown)
	}

	before := requests.Load()
	if again := get("zerator"); !again.Live || again.RecordID != stream.RecordID || requests.Load() != before {
		t.Fatalf("a repeated lookup must be answered from the cache: %+v (%d requests)", again, requests.Load()-before)
	}
	invalid := storedChannel(t, app, "twitch", "not a login", "")
	contentExpectError(t, request(t, handler, "/api/channel/"+invalid+"/live", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
}

func TestAChannelLiveFailureIsReportedAsNotLiveAndNotCached(t *testing.T) {
	t.Parallel()
	var working atomic.Bool
	twitch, _ := contentTwitchServer(t, contentTwitchLive(map[string]bool{"zerator": true}, &working))
	app, handler := newApp(t, twitch)
	path := "/api/channel/" + storedChannel(t, app, "twitch", "zerator", "") + "/live"
	if stream := contentDecode[contentLive](t, request(t, handler, path, "")); stream.Live {
		t.Fatalf("a failed lookup is not a live stream: %+v", stream)
	}
	// The failure was not remembered: the next lookup asks again.
	working.Store(true)
	if stream := contentDecode[contentLive](t, request(t, handler, path, "")); !stream.Live {
		t.Fatalf("the failure was cached: %+v", stream)
	}
}

func TestYouTubeChannelLiveRoute(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, contentStreamsDownloader)
	app, handler := newApp(t, option)
	path := func(channel string) string {
		return "/api/channel/" + storedChannel(t, app, "youtube", channel, "") + "/live"
	}

	stream := contentDecode[contentLive](t, request(t, handler, path(contentLiveChannel), ""))
	if !stream.Live || stream.Title != "On air" || stream.Viewers != 55 || stream.RecordID != recordIDOf(t, app, "videos", "livestream1", "youtube") {
		t.Fatalf("the stream in progress was not found, or not addressed by record id: %+v", stream)
	}
	// A cached answer serves the same record id: the live route and the subscriptions page open the stream by
	// record id, and a cache hit must not send them to the offline state.
	if again := contentDecode[contentLive](t, request(t, handler, path(contentLiveChannel), "")); again.RecordID != stream.RecordID || calls.count("channel/"+contentLiveChannel+"/streams") != 1 {
		t.Fatalf("a cached answer lost its record id or asked again: %+v", again)
	}
	for _, channel := range []string{contentQuietChannel, contentNoTabChannel} {
		if quiet := contentDecode[contentLive](t, request(t, handler, path(channel), "")); quiet.Live {
			t.Fatalf("%s is not live: %+v", channel, quiet)
		}
	}
	// An unexpected downloader failure is no stream either, and tells the client nothing about itself.
	res := request(t, handler, path("UCunexpected00000000000"), "")
	if unexpected := contentDecode[contentLive](t, res); unexpected.Live || strings.Contains(res.Body.String(), "unexpected YouTube fixture URL") {
		t.Fatalf("an unexpected failure: %s", res.Body)
	}
	contentExpectError(t, request(t, handler, path("bad/channel"), ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
}

// contentLiveVideo stores the video of a stream in progress.
func contentLiveVideo(t *testing.T, app core.App, platformID, channel, id string, full bool) string {
	t.Helper()
	return storedVideo(t, app, platform.Video{ID: id, Platform: platformID, ChannelID: channel, ChannelName: channel, Title: id, OriginalTitle: id, Live: true, FullMetadata: full})
}

func TestTheLiveRouteEndsTheStreamsOfAChannelThatIsNotLive(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, contentStreamsDownloader)
	twitch, _ := contentTwitchServer(t, contentTwitchLive(map[string]bool{"quiet": false, "onair": true, "liked": false}, nil))
	app, handler := newApp(t, option, twitch)
	user, _ := viewerOf(t, handler)
	profile := profileOf(t, app, user)
	// A Twitch stream's pseudo-video is named by the channel login.
	contentLiveVideo(t, app, "twitch", "quiet", "quiet", false)
	contentLiveVideo(t, app, "twitch", "onair", "onair", false)
	liked := contentLiveVideo(t, app, "twitch", "liked", "liked", false)
	reactions, err := app.FindCollectionByNameOrId("video_reactions")
	if err != nil {
		t.Fatal(err)
	}
	reaction := core.NewRecord(reactions)
	reaction.Load(map[string]any{"profile": profile.Id, "video": liked, "reaction": "like"})
	if err := app.Save(reaction); err != nil {
		t.Fatal(err)
	}
	// A YouTube stream stays stored under its own id, and becomes an ordinary video once it ended.
	ended := contentLiveVideo(t, app, "youtube", contentQuietChannel, "endedstream1", true)
	running := contentLiveVideo(t, app, "youtube", contentLiveChannel, "livestream1", true)
	if stored, err := app.FindRecordById("videos", ended); err != nil || !catalog.MetadataOf(stored).Fresh() {
		t.Fatalf("the fixture video must start with fresh metadata: %v", err)
	}

	live := func(platformID, channel string) {
		t.Helper()
		contentDecode[contentLive](t, request(t, handler, "/api/channel/"+recordIDOf(t, app, "channels", channel, platformID)+"/live", ""))
	}
	for platformID, channel := range map[string]string{"twitch": "quiet", "youtube": contentQuietChannel} {
		live(platformID, channel)
	}
	live("twitch", "onair")
	live("twitch", "liked")
	live("youtube", contentLiveChannel)

	if _, err := newStore(app).FindVideo("twitch", "quiet"); err == nil {
		t.Fatal("the record of a Twitch stream that ended is still listed")
	}
	if record, err := newStore(app).FindVideo("twitch", "onair"); err != nil || !record.GetBool("live") {
		t.Fatalf("a stream still in progress must keep its record and flag: %v", err)
	}
	// The like blocks the deletion: the record stays flagged, so clients keep hiding it until it is really live again.
	if record, err := newStore(app).FindVideo("twitch", "liked"); err != nil || !record.GetBool("live") {
		t.Fatalf("a liked stream's record must survive: %v", err)
	}
	if _, err := app.FindRecordById("video_reactions", reaction.Id); err != nil {
		t.Fatalf("the like was lost: %v", err)
	}
	reloaded, err := app.FindRecordById("videos", ended)
	if err != nil || reloaded.GetBool("live") {
		t.Fatalf("the flag of a stream that ended must be cleared: %v", err)
	}
	if catalog.MetadataOf(reloaded).Fresh() {
		t.Fatal("its metadata must be expired so the next opening fetches the final details")
	}
	if still, err := app.FindRecordById("videos", running); err != nil || !still.GetBool("live") {
		t.Fatalf("a stream in progress keeps its flag: %v", err)
	}
}

func TestRefreshingSubscriptionsClearsStreamsThatEndedSinceTheLastVisit(t *testing.T) {
	t.Parallel()
	twitch, _ := contentTwitchServer(t, contentTwitchLive(map[string]bool{"gone": false}, nil))
	app, handler := newApp(t, twitch)
	user, token := viewerOf(t, handler)
	profile := profileOf(t, app, user)
	_, channelID := contentSubscribe(t, app, profile, "twitch", "gone", "Gone")
	// A fresh channel listing: the refresh itself needs no network.
	channel, err := app.FindRecordById("channels", channelID)
	if err != nil {
		t.Fatal(err)
	}
	channel.Set("expires_at", time.Now().Add(time.Hour).UTC().Format(platform.StoredLayout))
	if err := app.Save(channel); err != nil {
		t.Fatal(err)
	}
	contentLiveVideo(t, app, "twitch", "gone", "gone", false)

	if res := send(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+profile.Id, token, ""); res.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", res.Code, res.Body)
	}
	if _, err := newStore(app).FindVideo("twitch", "gone"); err == nil {
		t.Fatal("the refresh left the record of a finished stream")
	}
}

// contentLiveStream is one entry of the subscriptions' live list.
type contentLiveStream struct {
	Platform  string  `json:"platform"`
	ChannelID string  `json:"channelId"`
	Live      bool    `json:"live"`
	RecordID  string  `json:"recordId"`
	Title     string  `json:"title"`
	Viewers   float64 `json:"viewers"`
	Thumbnail string  `json:"thumbnail"`
}

func TestSubscriptionsLiveListsOnlyTheSubscribedChannelsThatAreLive(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, contentStreamsDownloader)
	twitch, requests := contentTwitchServer(t, contentTwitchLive(map[string]bool{"zerator": true, "xqc": false, "stranger": true}, nil))
	app, handler := newApp(t, option, twitch)
	user, token := viewerOf(t, handler)
	profile, elsewhere := profileOf(t, app, user), contentProfile(t, app, user, "Elsewhere")
	_, zerator := contentSubscribe(t, app, profile, "twitch", "zerator", "ZeratoR")
	contentSubscribe(t, app, profile, "twitch", "xqc", "xQc")
	_, onAir := contentSubscribe(t, app, profile, "youtube", contentLiveChannel, "On air")
	contentSubscribe(t, app, profile, "youtube", contentQuietChannel, "Quiet")
	contentSubscribe(t, app, elsewhere, "twitch", "stranger", "Stranger")

	path := "/api/subscriptions/live?profile=" + profile.Id
	res := request(t, handler, path, token)
	streams := contentDecode[[]contentLiveStream](t, res)
	if len(streams) != 2 {
		t.Fatalf("only the two live channels of this profile are expected: %s", res.Body)
	}
	// Most watched first: Twitch's fixture reports 321 viewers, the YouTube stream 55. Channels and streams are
	// addressed by record id at the boundary.
	if got := streams[0]; got.Platform != "twitch" || got.ChannelID != zerator || !got.Live || got.RecordID != recordIDOf(t, app, "videos", "zerator", "twitch") || got.Viewers != 321 || got.Thumbnail == "" {
		t.Fatalf("twitch stream: %+v", got)
	}
	if got := streams[1]; got.Platform != "youtube" || got.ChannelID != onAir || got.RecordID != recordIDOf(t, app, "videos", "livestream1", "youtube") || got.Title != "On air" || got.Thumbnail != "https://i.ytimg.com/vi/livestream1/hqdefault_live.jpg" {
		t.Fatalf("youtube stream: %+v", got)
	}
	// The thumbnail is a picture address that happens to name the stream; no field carries a native id.
	if body := res.Body.String(); strings.Contains(body, `"videoId"`) || strings.Contains(body, `"channelId":"zerator"`) || strings.Contains(body, `"recordId":"livestream1"`) {
		t.Fatalf("a native id left the server: %s", body)
	}

	twitchRequests, lookups := requests.Load(), calls.count("/streams")
	if again := request(t, handler, path, token); again.Code != http.StatusOK || requests.Load() != twitchRequests || calls.count("/streams") != lookups {
		t.Fatalf("a repeated lookup must come from the cache: status=%d, %d new Twitch requests, %d new stream lookups", again.Code, requests.Load()-twitchRequests, calls.count("/streams")-lookups)
	}
}

func TestSubscriptionsLiveIsScopedToTheOwnersProfile(t *testing.T) {
	t.Parallel()
	option, _ := contentDownloader(t, "exit 1")
	app, handler := newApp(t, option)
	owner := testUser(t, app, "sub-live-owner@example.test")
	stranger := testUser(t, app, "sub-live-stranger@example.test")
	profile := profileOf(t, app, owner)
	strangerToken := tokenOf(t, stranger)
	contentExpectError(t, request(t, handler, "/api/subscriptions/live?profile="+profile.Id, strangerToken), http.StatusNotFound, "NOT_FOUND")
	contentExpectError(t, request(t, handler, "/api/subscriptions/live", strangerToken), http.StatusBadRequest, "INVALID_REQUEST")
	if res := anonymousRequest(t, handler, "/api/subscriptions/live?profile="+profile.Id); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request: %d %s", res.Code, res.Body)
	}
	// Without subscriptions the list is empty, not null.
	res := request(t, handler, "/api/subscriptions/live?profile="+profile.Id, tokenOf(t, owner))
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%s", res.Code, res.Body)
	}
}
