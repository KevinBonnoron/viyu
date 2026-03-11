package app

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// contentTwitchDownloader answers the downloader's questions about Twitch: one VOD, the channel's VOD listing
// and its stream in progress.
const contentTwitchDownloader = `case "$*" in
 *twitch.tv/videos/123*) printf '%s\n' '{"id":"v123","title":"VOD title","fulltitle":"VOD title","uploader_id":"channel","uploader":"Channel","thumbnail":"https://static.example/vod.jpg","duration":120,"timestamp":1762434000,"view_count":42,"formats":[{"protocol":"m3u8_native","height":1080,"manifest_url":"https://video.example/vod.m3u8"}]}' ;;
 *twitch.tv/channel/videos*) printf '%s\n' '{"_type":"playlist","id":"channel","entries":[{"id":"123","title":"VOD title","thumbnail":"https://static.example/vod.jpg","duration":120,"view_count":42}]}' ;;
 *twitch.tv/channel*) printf '%s\n' '{"id":"stream-id","title":"Channel (live)","uploader_id":"channel","uploader":"Channel","formats":[{"protocol":"m3u8_native","height":720,"manifest_url":"https://video.example/live.m3u8"}]}' ;;
 *) echo 'unexpected Twitch fixture URL' >&2; exit 9 ;;
esac`

// contentTwitchPage is Twitch's GraphQL as a channel page asks it: the channel profile with its VODs, and the
// playback access tokens that say which VODs are for subscribers only.
type contentTwitchPage struct {
	restricted map[string]bool
	profiles   atomic.Int32
	access     atomic.Int32
}

var contentAccessAlias = regexp.MustCompile(`(v\d+): videoPlaybackAccessToken\(id: "(\d+)"`)

func (p *contentTwitchPage) serve(w http.ResponseWriter, r *http.Request) {
	payload, _ := io.ReadAll(r.Body)
	var body struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(payload, &body)
	w.Header().Set("Content-Type", "application/json")
	if !strings.Contains(body.Query, "ViyuAccess") {
		p.profiles.Add(1)
		_, _ = io.WriteString(w, `{"data":{"user":{"login":"channel","displayName":"Channel","description":"Streams","profileImageURL":"https://static-cdn.example/channel-150x150.png","followers":{"totalCount":1234},"videos":{"edges":[{"node":{"id":"123","title":"VOD title","createdAt":"2026-11-06T12:30:00Z","lengthSeconds":3600,"viewCount":77,"previewThumbnailURL":"https://static-cdn.example/123.jpg"}}]}}}}`)
		return
	}
	p.access.Add(1)
	data := map[string]any{}
	for _, match := range contentAccessAlias.FindAllStringSubmatch(body.Query, -1) {
		alias, id := match[1], match[2]
		bitrates, known := p.restricted[id]
		switch {
		case !known:
			data[alias] = nil
		case bitrates:
			data[alias] = map[string]string{"value": `{"chansub":{"restricted_bitrates":["chunked","720p60"]},"vod_id":` + id + `}`}
		default:
			data[alias] = map[string]string{"value": `{"chansub":{"restricted_bitrates":[]},"vod_id":` + id + `}`}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func TestTwitchPublicSearchStoresChannelsAndVODsWithoutCredentials(t *testing.T) {
	t.Parallel()
	twitch, requests := contentTwitchServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Client-Id") != contentTwitchID {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"edges":[{"item":{"login":"xqc","displayName":"xQc"}}]},"videos":{"edges":[{"item":{"id":"123","title":"Public VOD","owner":{"login":"xqc","displayName":"xQc"}}}]}}}}`))
	})
	app, handler := newApp(t, twitch)

	// A short login stays searchable, and its channel can be opened.
	page := contentDecode[contentPage](t, request(t, handler, "/api/search?q=xqc&platform=twitch", ""))
	if len(page.Results) != 2 {
		t.Fatalf("public Twitch search: %+v (%d requests)", page, requests.Load())
	}
	channel, video := page.Results[0], page.Results[1]
	channelRecord := recordIDOf(t, app, "channels", "xqc", "twitch")
	if channel["id"] != channelRecord || channel["recordId"] != channelRecord || channel["platform"] != "twitch" || channel["name"] != "xQc" || channel["source"] != nil {
		t.Fatalf("channel result: %v", channel)
	}
	if video["id"] != recordIDOf(t, app, "videos", "123", "twitch") || video["channelId"] != channelRecord || video["source"] != nil || video["title"] != "Public VOD" {
		t.Fatalf("a public result requires no private session: %v", video)
	}
	if res := request(t, handler, "/api/channel/"+channelRecord+"/live", ""); res.Code != http.StatusOK {
		t.Fatalf("the channel of a short login cannot be opened: %d %s", res.Code, res.Body)
	}
}

func TestTwitchChannelPageIsCompletedFromTwitchAndFlagsSubscriberOnlyVODs(t *testing.T) {
	t.Parallel()
	page := &contentTwitchPage{restricted: map[string]bool{"123": true}}
	twitch, _ := contentTwitchServer(t, page.serve)
	downloader, _ := contentDownloader(t, contentTwitchDownloader)
	app, handler := newApp(t, twitch, downloader)
	channel := storedChannel(t, app, "twitch", "channel", "")

	res := request(t, handler, "/api/channel/"+channel, "")
	listing := contentDecode[contentListing](t, res)
	// yt-dlp lists the VOD by title only; the channel's name, avatar and audience come from Twitch.
	if listing.Channel.ID != channel || listing.Channel.Name != "Channel" || listing.Channel.Thumbnail != "https://static-cdn.example/channel-150x150.png" || listing.Channel.SubscriberCount != 1234 || listing.Channel.Description != "Streams" {
		t.Fatalf("the channel was not completed from Twitch: %+v", listing.Channel)
	}
	if len(listing.Videos) != 1 {
		t.Fatalf("videos: %+v", listing.Videos)
	}
	vod := listing.Videos[0]
	// What the listing knows is kept; what it omits (the date) comes from Twitch.
	if vod.ID != recordIDOf(t, app, "videos", "123", "twitch") || vod.Duration != 120 || vod.ViewCount != 42 || vod.Published != "2026-11-06T12:30:00Z" || !vod.OriginalTitleVerified {
		t.Fatalf("the VOD was not completed from Twitch: %+v", vod)
	}
	if !vod.SubscriberOnly || !strings.Contains(res.Body.String(), `"subscriberOnly":true`) {
		t.Fatalf("the restricted VOD carries no flag: %s", res.Body)
	}
	if page.profiles.Load() != 1 || page.access.Load() != 1 {
		t.Fatalf("one listing took %d profile and %d access requests, want 1 and 1", page.profiles.Load(), page.access.Load())
	}
}

func TestSubscriberOnlyIsStoredAndOnlyOverwrittenByAKnownAnswer(t *testing.T) {
	t.Parallel()
	app, _ := newApp(t)
	store := newStore(app)
	stored := func() bool {
		record, err := store.FindVideo("twitch", "4001")
		if err != nil {
			t.Fatal(err)
		}
		return store.VideoOf(record).SubscriberOnly
	}
	listing := func(restricted, checked bool) {
		v := platform.Video{ID: "4001", Platform: "twitch", ChannelID: "streamer", ChannelName: "Streamer", Title: "VOD", SubscriberOnly: restricted, SubscriberChecked: checked}
		if err := store.StoreVideo(&v, ""); err != nil {
			t.Fatal(err)
		}
	}
	listing(true, true)
	if !stored() {
		t.Fatal("a looked-up restriction was not stored")
	}
	listing(false, false)
	if !stored() {
		t.Fatal("a listing that did not look the restriction up erased it")
	}
	listing(false, true)
	if stored() {
		t.Fatal("a VOD that became public kept its badge")
	}
}

func TestATwitchStreamInProgressIsFlaggedAndNeverServedFromTheStore(t *testing.T) {
	t.Parallel()
	downloader, _ := contentDownloader(t, contentTwitchDownloader)
	app, handler := newApp(t, downloader)
	records := map[string]string{
		"channel": storedVideo(t, app, platform.Video{ID: "channel", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"}),
		"123":     storedVideo(t, app, platform.Video{ID: "123", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"}),
	}
	get := func(id string) platform.Video {
		t.Helper()
		video := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+records[id], ""))
		if video.ID != records[id] {
			t.Fatalf("%s: %+v", id, video)
		}
		return video
	}
	retitle := func(id, title string) {
		t.Helper()
		record, err := app.FindRecordById("videos", records[id])
		if err != nil {
			t.Fatal(err)
		}
		catalog.SetTitles(record, title, "")
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
	}

	if stream := get("channel"); !stream.Live || stream.Title != "Channel (live)" {
		t.Fatalf("a channel login is a stream in progress: %+v", stream)
	}
	if record, err := app.FindRecordById("videos", records["channel"]); err != nil || !record.GetBool("live") {
		t.Fatalf("the live flag was not remembered: %v", err)
	}
	// A stream is only true right now: what is stored about it is never an answer.
	retitle("channel", "stale")
	if stream := get("channel"); stream.Title != "Channel (live)" {
		t.Fatalf("a stream was served from the store: %+v", stream)
	}

	if vod := get("123"); vod.Live {
		t.Fatalf("a VOD is not live: %+v", vod)
	}
	retitle("123", "cached")
	if vod := get("123"); vod.Title != "cached" {
		t.Fatalf("a VOD should still come from the store: %+v", vod)
	}
}

// yt-dlp calls a stream "<channel> (live) <date>"; the page shows what the streamer called it, the game and
// the audience, all from Twitch. The category is kept with the video like the rest of its metadata.
func TestATwitchStreamTakesItsTitleGameAndAudienceFromTwitch(t *testing.T) {
	t.Parallel()
	twitch, _ := contentTwitchServer(t, contentTwitchLive(map[string]bool{"channel": true}, nil))
	downloader, _ := contentDownloader(t, contentTwitchDownloader)
	app, handler := newApp(t, twitch, downloader)
	id := storedVideo(t, app, platform.Video{ID: "channel", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"})

	stream := contentDecode[platform.Video](t, request(t, handler, "/api/video/"+id, ""))
	if !stream.Live || stream.Title != "Speedrun" || stream.OriginalTitle != "Speedrun" || stream.Category != "World of Warcraft" || stream.ViewCount != 321 {
		t.Fatalf("stream: %+v", stream)
	}
	if record, err := app.FindRecordById("videos", id); err != nil || record.GetString("category") != "World of Warcraft" {
		t.Fatalf("the category was not kept: %v", err)
	}
	status := contentDecode[contentLive](t, request(t, handler, "/api/channel/"+recordIDOf(t, app, "channels", "channel", "twitch")+"/live", ""))
	if !status.Live || status.Category != "World of Warcraft" || status.Viewers != 321 || status.RecordID != id {
		t.Fatalf("live status: %+v", status)
	}
}

func TestATwitchChannelThatIsNotLiveHasItsOwnError(t *testing.T) {
	t.Parallel()
	downloader, _ := contentDownloader(t, "echo 'ERROR: [twitch:stream] xqc: The channel is not currently live' >&2\nexit 1")
	app, handler := newApp(t, downloader)
	xqc := storedVideo(t, app, platform.Video{ID: "xqc", Platform: "twitch", ChannelID: "xqc", ChannelName: "xqc"})
	for _, path := range []string{"/api/video/" + xqc, "/api/stream/" + xqc} {
		res := request(t, handler, path, "")
		contentExpectError(t, res, http.StatusNotFound, "SOURCE_CHANNEL_OFFLINE")
		if strings.Contains(res.Body.String(), "twitch:stream") {
			t.Fatalf("%s: the downloader's output reached the client: %s", path, res.Body)
		}
	}
}

func TestATwitchVODForSubscribersIsReportedAsSuch(t *testing.T) {
	t.Parallel()
	downloader, _ := contentDownloader(t, `echo 'ERROR: [twitch:vod] 999: You must be logged into an account that has access to this subscriber-only content. Use --cookies' >&2
exit 1`)
	app, handler := newApp(t, downloader)
	vod := storedVideo(t, app, platform.Video{ID: "999", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"})
	res := request(t, handler, "/api/video/"+vod, "")
	contentExpectError(t, res, http.StatusForbidden, "SOURCE_SUBSCRIBER_ONLY")
}
