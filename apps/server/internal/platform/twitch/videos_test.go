package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// downloader is the fake yt-dlp of the tests: one canned answer per kind of Twitch address.
const downloader = `
case "$*" in
 *twitch.tv/videos/123*) printf '%s\n' '{"id":"v123","title":"VOD title","fulltitle":"VOD title","uploader_id":"channel","uploader":"Channel","thumbnail":"https://static.example/vod.jpg","duration":120,"timestamp":1762434000,"view_count":42,"formats":[{"protocol":"m3u8_native","height":1080,"manifest_url":"https://video.example/vod.m3u8"}]}' ;;
 *twitch.tv/videos/999*) echo 'ERROR: [twitch:vod] 999: You must be logged into an account that has access to this subscriber-only content. Use --cookies' >&2; exit 1 ;;
 *twitch.tv/videos/429*) echo 'ERROR: Unable to download JSON metadata: HTTP Error 429: Too Many Requests' >&2; exit 1 ;;
 *twitch.tv/videos/555*) echo 'ERROR: something else broke, secret detail /home/user/.cache' >&2; exit 1 ;;
 *twitch.tv/videos/888*) printf '%s\n' '{"id":"v888"}' '{"id":"v889"}' ;;
 *twitch.tv/xqc*) echo 'ERROR: [twitch:stream] xqc: The channel is not currently live' >&2; exit 1 ;;
 *twitch.tv/channel/videos*) printf '%s\n' '{"_type":"playlist","id":"channel","entries":[{"id":"123","title":"VOD title","thumbnail":"https://static.example/vod.jpg","duration":120,"view_count":42}]}' ;;
 *twitch.tv/broken/videos*) printf '%s\n' '{"_type":"playlist","id":"broken","entries":[{"id":"not-a-vod","title":"x"}]}' ;;
 *twitch.tv/empty/videos*) printf '%s\n' '{"id":"empty"}' ;;
 *twitch.tv/channel*) printf '%s\n' '{"id":"stream-id","title":"Channel (live) 2026-11-06","uploader_id":"channel","uploader":"Channel","is_live":true,"formats":[{"protocol":"m3u8_native","height":720,"manifest_url":"https://video.example/live.m3u8"}]}' ;;
 *) echo 'unexpected Twitch fixture URL' >&2; exit 9 ;;
esac
`

func TestPublicVideoChannelAndPlayback(t *testing.T) {
	t.Parallel()
	p := newPlatform(t, Options{Runner: fakeYTDLP(t, downloader), GraphQLURL: "http://127.0.0.1:1/graphql"})
	video, err := p.VideoMetadata(context.Background(), "123", "")
	if err != nil {
		t.Fatal(err)
	}
	published := time.Unix(1762434000, 0).UTC().Format(time.RFC3339Nano)
	if video.ID != "123" || video.Platform != ID || video.Title != "VOD title" || video.OriginalTitle != "VOD title" || !video.OriginalTitleVerified || video.ChannelID != "channel" || video.ChannelName != "Channel" ||
		video.Duration != 120 || video.ViewCount != 42 || video.Published != published || video.Live || !video.FullMetadata || !video.SubscriberChecked || video.SubscriberOnly ||
		video.Manifest.URL != "https://video.example/vod.m3u8" || video.Thumbnail != "https://static.example/vod.jpg" {
		t.Fatalf("Twitch VOD: %+v", video)
	}
	// With an account the restriction is not known from the metadata alone.
	if video, err = p.VideoMetadata(context.Background(), "123", "/tmp/jar.txt"); err != nil || video.SubscriberChecked {
		t.Fatalf("a request made as an account cannot vouch for public playback: %+v %v", video, err)
	}

	manifest, err := p.PlaybackManifest(context.Background(), "channel", "")
	if err != nil || manifest.URL != "https://video.example/live.m3u8" {
		t.Fatalf("Twitch live manifest: %+v %v", manifest, err)
	}
	if manifest, err = p.PlaybackManifest(context.Background(), "123", ""); err != nil || manifest.URL != "https://video.example/vod.m3u8" {
		t.Fatalf("Twitch VOD manifest: %+v %v", manifest, err)
	}
}

func TestInvalidIdentifiersNeverReachTheDownloader(t *testing.T) {
	t.Parallel()
	p := newPlatform(t, Options{})
	for _, id := range []string{"not-a-twitch-video", "", "a b", "bad/id"} {
		_, err := p.VideoMetadata(context.Background(), id, "")
		assertCode(t, err, apierr.CodeInvalidSourceArg)
		_, err = p.PlaybackManifest(context.Background(), id, "")
		assertCode(t, err, apierr.CodeInvalidSourceArg)
	}
	_, _, err := p.ChannelListing(context.Background(), "not a login", 10, "")
	assertCode(t, err, apierr.CodeInvalidSourceArg)
}

func TestLiveStreamTakesItsTitleGameAndAudienceFromTwitch(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	p := graphQLPlatform(t, liveHandler(map[string]bool{"channel": true}, &requests), Options{Runner: fakeYTDLP(t, downloader)})
	stream, err := p.VideoMetadata(context.Background(), "channel", "")
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Live || stream.ID != "channel" || stream.Title != "Speedrun" || stream.OriginalTitle != "Speedrun" || stream.Category != "World of Warcraft" || stream.ViewCount != 321 || stream.Manifest.URL != "https://video.example/live.m3u8" {
		t.Fatalf("stream: %+v", stream)
	}

	// Twitch not answering leaves what the downloader said.
	stream, err = newPlatform(t, Options{Runner: fakeYTDLP(t, downloader), GraphQLURL: "http://127.0.0.1:1/graphql"}).VideoMetadata(context.Background(), "channel", "")
	if err != nil || !stream.Live || stream.Title != "Channel (live) 2026-11-06" || stream.Category != "" {
		t.Fatalf("stream without Twitch: %+v %v", stream, err)
	}
}

func TestDownloaderFailuresAreClassifiedAndSafe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id, code string
	}{
		{"999", apierr.CodeSubscriberOnly},
		{"xqc", apierr.CodeChannelOffline},
		{"429", apierr.CodeUpstreamRateLimit},
		{"555", apierr.CodeRequestFailed},
		{"888", apierr.CodeRequestFailed}, // more than one video
	} {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			p := newPlatform(t, Options{Runner: fakeYTDLP(t, downloader)})
			for name, call := range map[string]func() error{
				"metadata": func() error { _, err := p.VideoMetadata(context.Background(), tc.id, ""); return err },
				"manifest": func() error { _, err := p.PlaybackManifest(context.Background(), tc.id, ""); return err },
			} {
				err := call()
				assertCode(t, err, tc.code)
				failure := err.(*apierr.Error)
				if strings.Contains(failure.Message, "yt-dlp") || strings.Contains(failure.Message, "secret") || strings.Contains(failure.Message, "--cookies") {
					t.Fatalf("%s: downloader output leaked: %q", name, failure.Message)
				}
				if tc.code == apierr.CodeRequestFailed && failure.Cause == nil {
					t.Fatalf("%s: the cause must be attached", name)
				}
			}
		})
	}
	// 429 is what ytdlp reports through errors.Is too.
	_, err := newPlatform(t, Options{Runner: fakeYTDLP(t, downloader)}).VideoMetadata(context.Background(), "429", "")
	if !errors.Is(err, ytdlp.ErrRateLimited) {
		t.Fatalf("rate limit not kept as the cause: %v", err)
	}
}

func TestCancellationPassesThroughUnclassified(t *testing.T) {
	t.Parallel()
	p := newPlatform(t, Options{Runner: fakeYTDLP(t, downloader)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.VideoMetadata(ctx, "123", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("metadata: %v", err)
	}
	if _, _, err := p.ChannelListing(ctx, "channel", 5, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("listing: %v", err)
	}
	if _, err := p.LiveStream(ctx, "channel"); !errors.Is(err, context.Canceled) {
		t.Fatalf("live: %v", err)
	}
}

// The channel listing is where VODs get their badge and their dates: yt-dlp lists them, Twitch says which
// need a subscription and when they were published.
func TestChannelListingIsCompletedFromTwitch(t *testing.T) {
	t.Parallel()
	var access atomic.Int32
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(readBody(t, r), &body)
		if strings.Contains(body.Query, "ViyuAccess") {
			access.Add(1)
			_, _ = io.WriteString(w, `{"data":{"v0":{"value":"{\"chansub\":{\"restricted_bitrates\":[\"chunked\"]}}"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"user":{"login":"channel","displayName":"The Channel","description":"About","profileImageURL":"https://static-cdn.example/channel.png","followers":{"totalCount":99},"videos":{"edges":[{"node":{"id":"123","title":"VOD title","createdAt":"2026-11-06T12:30:00Z","lengthSeconds":3600,"viewCount":1,"previewThumbnailURL":"https://static-cdn.example/123.jpg"}}]}}}}`)
	}, Options{Runner: fakeYTDLP(t, downloader)})

	info, videos, err := p.ChannelListing(context.Background(), "channel", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "channel" || info.Platform != ID || info.Name != "The Channel" || info.Thumbnail != "https://static-cdn.example/channel.png" || info.SubscriberCount != 99 || info.Description != "About" {
		t.Fatalf("channel: %+v", info)
	}
	if len(videos) != 1 {
		t.Fatalf("videos: %+v", videos)
	}
	video := videos[0]
	if video.ID != "123" || video.Platform != ID || video.ChannelID != "channel" || video.Published != "2026-11-06T12:30:00Z" || video.Duration != 120 || video.ViewCount != 42 || !video.SubscriberOnly || !video.SubscriberChecked {
		t.Fatalf("the restricted VOD was not completed: %+v", video)
	}
	if access.Load() != 1 {
		t.Fatalf("one listing took %d access requests, want 1", access.Load())
	}
	if _, _, err := p.ChannelListing(context.Background(), "channel", 1, ""); err != nil || access.Load() != 1 {
		t.Fatalf("a one-VOD listing must not look restrictions up: %v (%d requests)", err, access.Load())
	}
}

func TestChannelListingSurvivesTwitchBeingDownButRejectsBadEntries(t *testing.T) {
	t.Parallel()
	p := newPlatform(t, Options{Runner: fakeYTDLP(t, downloader), GraphQLURL: "http://127.0.0.1:1/graphql"})
	info, videos, err := p.ChannelListing(context.Background(), "channel", 5, "")
	if err != nil || info.ID != "channel" || info.Name != "channel" || len(videos) != 1 || videos[0].SubscriberChecked {
		t.Fatalf("listing without Twitch: %+v %+v %v", info, videos, err)
	}
	for _, login := range []string{"broken", "empty"} {
		_, _, err := p.ChannelListing(context.Background(), login, 5, "")
		assertCode(t, err, apierr.CodeRequestFailed)
	}
}

func TestPlatformFacts(t *testing.T) {
	t.Parallel()
	p := newPlatform(t, Options{})
	if p.ID() != "twitch" || !p.DefaultEnabled() || !p.SingleTitle() || p.CatalogSync() == nil || p.Chat() == nil || p.Connector() == nil || p.CommentsMode() != platform.CommentsChat {
		t.Fatal("platform facts changed")
	}
	if descriptor := p.Descriptor(); descriptor.Platform != "twitch" || descriptor.Name != "Twitch" || len(descriptor.ConnectionMethods) != 1 || descriptor.ConnectionMethods[0] != "cookies" {
		t.Fatalf("descriptor: %+v", descriptor)
	}
	_, err := p.Comments(context.Background(), "123", "")
	assertCode(t, err, apierr.CodeInvalidSourceArg)
	p.EnrichListing(context.Background(), "channel", []platform.Video{{ID: "123"}})

	for id, want := range map[string]bool{
		"2919341981":  true,
		"123":         true,
		"zerator":     true, // a live is opened by the channel login
		"dQw4w9WgXcQ": false,
		"12345678901": false, // 11 characters is the length of a YouTube ID: the platform has to be stated
		"elevenchars": false,
		"not-a-login": false, // 11 characters again, and a hyphen is not allowed in a login anyway
		"@handle":     false,
	} {
		if got := p.ClaimsVideoID(id); got != want {
			t.Errorf("ClaimsVideoID(%q) = %v, want %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"123": true, "12345678901": true, "zerator": true, "Zerator_1": true, "0123": true, "": false, "a-b": false, "@h": false, strings.Repeat("a", 26): false} {
		// "0123" is a valid login although it is not a VOD id.
		if got := p.ValidVideoID(id); got != want {
			t.Errorf("ValidVideoID(%q) = %v, want %v", id, got, want)
		}
	}
	if !p.ValidChannelID("xqc") || p.ValidChannelID("a b") || !p.ValidLiveChannel("xqc") || p.ValidLiveChannel("") {
		t.Error("channel validation")
	}
	for id, want := range map[string]string{
		"123":     "https://www.twitch.tv/videos/123",
		"zerator": "https://www.twitch.tv/zerator",
		"a b":     "",
	} {
		if got := p.OriginalURL(id); got != want {
			t.Errorf("OriginalURL(%q) = %q, want %q", id, got, want)
		}
	}
	if got := p.LiveThumbnail(platform.LiveStream{VideoID: "ZeRaTor"}); got != "https://static-cdn.jtvnw.net/previews-ttv/live_user_zerator-640x360.jpg" {
		t.Errorf("live thumbnail %q", got)
	}
	if aliases := p.ChannelIDAliases("ZeRaTor"); len(aliases) != 1 || aliases[0] != "zerator" {
		t.Errorf("aliases %v", aliases)
	}
	if end := p.EndLive("ZeRaTor"); len(end.ChannelIDs) != 1 || end.ChannelIDs[0] != "zerator" || end.StubVideoID != "zerator" {
		t.Errorf("end of live %+v", end)
	}
	for host, want := range map[string]bool{
		"video-weaver.ams.hls.ttvnw.net": true, "ttvnw.net": true, "usher.twitch.tv": true, "twitch.tv": true,
		"nottwitch.tv": false, "twitch.tv.evil.test": false, "googlevideo.com": false, "": false,
	} {
		if got := p.TrustedMediaHost(host); got != want {
			t.Errorf("TrustedMediaHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestResolveURL(t *testing.T) {
	t.Parallel()
	p := newPlatform(t, Options{})
	registry := platform.NewRegistry(p)
	for raw, want := range map[string]string{
		"https://www.twitch.tv/videos/2919341981":     "2919341981",
		"twitch.tv/videos/123":                        "123",
		"http://TWITCH.tv/videos/123/":                "123",
		"https://www.twitch.tv/videos/123?t=1h2m3s":   "123",
		"https://www.twitch.tv/zerator":               "",
		"https://www.twitch.tv/zerator/videos/123":    "",
		"https://www.twitch.tv/videos/abc":            "",
		"https://www.twitch.tv/videos/0123":           "",
		"https://m.twitch.tv/videos/123":              "",
		"https://twitch.tv.evil.test/videos/123":      "",
		"https://user@www.twitch.tv/videos/123":       "",
		"https://www.twitch.tv:8443/videos/123":       "",
		"https://www.twitch.tv/videos/123/extra/path": "",
	} {
		found, id, ok := registry.ResolveURL(raw)
		if ok != (want != "") || id != want || (ok && found != platform.Platform(p)) {
			t.Errorf("ResolveURL(%q) = %v %q %v, want %q", raw, found, id, ok, want)
		}
	}
}
