package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// streamTwitchDownloader is a yt-dlp that knows one public VOD (123, played from manifestURL), one
// subscriber-only VOD (999) and one channel.
func streamTwitchDownloader(manifestURL string) string {
	return strings.ReplaceAll(`case "$*" in
 *twitch.tv/videos/123*) printf '%s\n' '{"id":"v123","title":"VOD title","fulltitle":"VOD title","uploader_id":"channel","uploader":"Channel","thumbnail":"https://static.example/vod.jpg","duration":120,"timestamp":1762434000,"view_count":42,"formats":[{"protocol":"m3u8_native","height":1080,"manifest_url":"MANIFEST"}]}' ;;
 *twitch.tv/videos/999*) echo 'ERROR: [twitch:vod] 999: You must be logged into an account that has access to this subscriber-only content. Use --cookies' >&2; exit 1 ;;
 *twitch.tv/channel/videos*) printf '%s\n' '{"_type":"playlist","id":"channel","entries":[{"id":"123","title":"VOD title","thumbnail":"https://static.example/vod.jpg","duration":120,"view_count":42}]}' ;;
 *) echo 'unexpected Twitch fixture URL' >&2; exit 9 ;;
esac`, "MANIFEST", manifestURL)
}

func TestTwitchPublicVideoChannelAndPlaybackRoutes(t *testing.T) {
	t.Parallel()
	upstream := streamUpstream(t)
	app, handler := newApp(t, withDownloader(t, streamTwitchDownloader(upstream.URL+"/vod/master.m3u8")), withLoopbackMedia)
	vod := storedVideo(t, app, platform.Video{ID: "123", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"})
	channelRecord := recordIDOf(t, app, "channels", "channel", "twitch")

	res := request(t, handler, "/api/video/"+vod, "")
	if res.Code != http.StatusOK {
		t.Fatalf("Twitch video status=%d body=%s", res.Code, res.Body)
	}
	var video platform.Video
	if err := json.Unmarshal(res.Body.Bytes(), &video); err != nil || video.ID != vod || video.Platform != "twitch" || video.ChannelID != channelRecord || video.Title != "VOD title" {
		t.Fatalf("Twitch video: %+v %v", video, err)
	}
	if strings.Contains(res.Body.String(), `"123"`) {
		t.Fatalf("the native VOD id left the server: %s", res.Body)
	}

	res = request(t, handler, "/api/channel/"+channelRecord, "")
	if res.Code != http.StatusOK {
		t.Fatalf("Twitch channel status=%d body=%s", res.Code, res.Body)
	}
	var listing struct {
		Channel platform.Channel `json:"channel"`
		Videos  []platform.Video `json:"videos"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &listing); err != nil || listing.Channel.Platform != "twitch" || listing.Channel.ID != channelRecord || len(listing.Videos) != 1 || listing.Videos[0].Platform != "twitch" || listing.Videos[0].ID != vod {
		t.Fatalf("Twitch channel: %+v %v", listing, err)
	}

	store := newStore(app)
	if _, err := store.FindVideo("twitch", "123"); err != nil {
		t.Fatalf("Twitch video was not stored under its platform: %v", err)
	}
	if _, err := store.FindVideo("youtube", "123"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Twitch video entered the YouTube catalog: %v", err)
	}

	// An id that is no VOD never reaches the downloader.
	bogus := storedVideo(t, app, platform.Video{ID: "not-a-twitch-video", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"})
	res = request(t, handler, "/api/video/"+bogus, "")
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"code":"INVALID_SOURCE_INPUT"`) {
		t.Fatalf("invalid Twitch video status=%d body=%s", res.Code, res.Body)
	}

	// The VOD plays through the proxy; its resources say which platform they belong to.
	master := request(t, handler, "/api/stream/"+vod, "")
	if master.Code != http.StatusOK || !strings.HasPrefix(master.Body.String(), "#EXTM3U") {
		t.Fatalf("Twitch playback: %d %s", master.Code, master.Body)
	}
	streamHardened(t, "Twitch master", master)
	resources := streamResources(master.Body.String())
	if len(resources) != 1 || !strings.HasPrefix(resources[0], "/api/stream/"+vod+"/resource/") || !strings.Contains(resources[0], "platform=twitch") || strings.Contains(master.Body.String(), "123") {
		t.Fatalf("Twitch HLS resource lost its platform or named the native id: %v", resources)
	}
	if variant := request(t, handler, resources[0], ""); variant.Code != http.StatusOK || !strings.Contains(variant.Body.String(), "#EXTINF:6,") {
		t.Fatalf("Twitch variant: %d %s", variant.Code, variant.Body)
	}
}

// A subscriber-only VOD cannot be played without an account that subscribes; the player must be able to say
// so instead of reporting a generic failure.
func TestTwitchSubscriberOnlyVODReportsItsOwnError(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, withDownloader(t, streamTwitchDownloader("https://video.example/vod.m3u8")))
	subscriberOnly := storedVideo(t, app, platform.Video{ID: "999", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"})
	for _, path := range []string{"/api/video/" + subscriberOnly, "/api/stream/" + subscriberOnly} {
		res := request(t, handler, path, "")
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &failure); res.Code != http.StatusForbidden || err != nil || failure.Code != "SOURCE_SUBSCRIBER_ONLY" {
			t.Fatalf("%s: status=%d body=%s", path, res.Code, res.Body)
		}
		if strings.Contains(res.Body.String(), "yt-dlp") || strings.Contains(res.Body.String(), "--cookies") {
			t.Fatalf("downloader output leaked: %s", res.Body)
		}
	}
}
