package youtube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const listingScript = `
case "$*" in
 *watch?v=failed*) echo 'fixture download failed' >&2; exit 9 ;;
 *--dump-single-json*'/channel/UCfixture/videos'*|*'/channel/UCfixture/videos'*--dump-single-json*) printf '%s\n' '{"_type":"playlist","id":"UCfixture","channel_id":"UCfixture","channel":"Fixture channel","title":"Fixture channel","description":"Channel description","channel_follower_count":1234,"thumbnail":"https://i.ytimg.com/vi/fixture-video/hqdefault.jpg","thumbnails":[{"id":"banner_uncropped","url":"https://yt3.googleusercontent.com/banner=s0","preference":10},{"id":"avatar_uncropped","url":"https://yt3.ggpht.com/fixture-avatar=s0","preference":1}],"entries":[{"id":"fixture-video","title":"Fixture video","thumbnail":"https://i.ytimg.com/vi/fixture-video/hqdefault.jpg","duration":120,"upload_date":"20260102","view_count":42},{"id":"owned-video","title":"Own","channel_id":"UCother","channel":"Other"}]}'; exit 0 ;;
 *'/channel/UCfixture/videos'*) printf '%s\n' '{"id":"fixture-video","title":"Fixture video","duration":120,"upload_date":"20260102"}' '{"id":"second-video","title":"Second","channel_id":"UCexplicit"}'; exit 0 ;;
 *'/@handle/videos'*) printf '%s\n' '{"id":"handle-video","title":"By handle"}'; exit 0 ;;
 *--dump-single-json*'youtube:player_client'*|*'youtube:player_client'*--dump-single-json*) printf '%s\n' '{"id":"dQw4w9WgXcQ","http_headers":{"Referer":"https://www.youtube.com/"},"formats":[{"protocol":"https","height":2160,"manifest_url":""},{"protocol":"m3u8_native","height":360,"manifest_url":"https://manifest.googlevideo.com/low.m3u8","http_headers":{"X-Format":"low"}},{"protocol":"m3u8_native","height":1080,"manifest_url":"https://manifest.googlevideo.com/high.m3u8"}]}'; exit 0 ;;
esac
printf '%s\n' '{"id":"testvid0001","title":"Les eaux magiques face à la science","fulltitle":"Les eaux magiques face à la science","channel_id":"UCcreator","channel":"DEFAKATOR","upload_date":"20260304","duration":2529,"tags":["music"],"chapters":[{"title":"intro","start_time":0,"end_time":12.5},{"title":"Next","start_time":12.5,"end_time":42}],"formats":[{"acodec":"mp4a","language":"fr","language_preference":10},{"protocol":"m3u8_native","height":720,"manifest_url":"https://manifest.googlevideo.com/v.m3u8"}]}'
`

func TestVideoMetadataReadsTheVideoPage(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, listingScript)
	video, err := p.VideoMetadata(context.Background(), "testvid0001", "")
	if err != nil {
		t.Fatal(err)
	}
	if !video.FullMetadata || !video.OriginalTitleVerified || video.Title != "Les eaux magiques face à la science" || video.OriginalTitle != video.Title {
		t.Fatalf("a video page is authoritative: %+v", video)
	}
	if video.ChannelID != "UCcreator" || video.ChannelName != "DEFAKATOR" || video.Published != "2026-03-04" || video.Duration != 2529 || video.OriginalLanguage != "fr" {
		t.Fatalf("metadata: %+v", video)
	}
	if len(video.Chapters) != 2 || video.Chapters[0] != (platform.Chapter{Title: "intro", StartTime: 0, EndTime: 12.5}) || video.Chapters[1].Title != "Next" {
		t.Fatalf("chapters: %+v", video.Chapters)
	}
	if video.Manifest.URL != "https://manifest.googlevideo.com/v.m3u8" {
		t.Fatalf("manifest: %+v", video.Manifest)
	}
	if got := calls(t, log); !strings.Contains(got, "--dump-json --no-download https://www.youtube.com/watch?v=testvid0001") || strings.Contains(got, "--cookies") || !strings.Contains(got, "--no-mark-watched") {
		t.Fatalf("arguments: %s", got)
	}
}

func TestVideoMetadataFailuresAreSafeErrors(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, listingScript)
	_, err := p.VideoMetadata(context.Background(), "failed000001", "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
	if strings.Contains(err.Error(), "fixture download failed") {
		t.Fatalf("stderr leaked into the error message: %v", err)
	}
	empty, _ := fakeYTDLP(t, "exit 0")
	_, err = empty.VideoMetadata(context.Background(), "testvid0001", "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
}

func TestVideoMetadataPassesTheCookieJar(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, listingScript)
	if _, err := p.VideoMetadata(context.Background(), "testvid0001", "/private/jar.txt"); err != nil {
		t.Fatal(err)
	}
	if got := calls(t, log); !strings.Contains(got, "--cookies /private/jar.txt") {
		t.Fatalf("cookies not passed: %s", got)
	}
}

func TestChannelListingReadsTheProfileAndTheOwnerOfFlatEntries(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, listingScript)
	info, videos, err := p.ChannelListing(context.Background(), "UCfixture", 30, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "UCfixture" || info.Platform != "youtube" || info.Name != "Fixture channel" || info.Description != "Channel description" || info.SubscriberCount != 1234 {
		t.Fatalf("channel: %+v", info)
	}
	if want := "https://yt3.googleusercontent.com/fixture-avatar=s0"; info.Thumbnail != want {
		t.Fatalf("the avatar must not be the banner nor the video artwork: %q", info.Thumbnail)
	}
	if len(videos) != 2 {
		t.Fatalf("videos: %+v", videos)
	}
	first, second := videos[0], videos[1]
	if first.ID != "fixture-video" || first.ChannelID != "UCfixture" || first.ChannelName != "Fixture channel" || first.ChannelThumbnail != info.Thumbnail || first.Published != "2026-01-02" || first.FullMetadata || first.OriginalTitleVerified {
		t.Fatalf("a flat entry takes its owner from the playlist and claims no verification: %+v", first)
	}
	if second.ChannelID != "UCother" || second.ChannelName != "Other" {
		t.Fatalf("an entry that names its owner keeps it: %+v", second)
	}
	if got := calls(t, log); !strings.Contains(got, "https://www.youtube.com/channel/UCfixture/videos --dump-single-json --flat-playlist --playlist-end 30 --no-download") {
		t.Fatalf("arguments: %s", got)
	}
}

func TestChannelListingByHandleAndInvalidListing(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, `printf '%s\n' '{"_type":"video","id":"x"}'`)
	_, _, err := p.ChannelListing(context.Background(), "@handle", 5, "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
	if got := calls(t, log); !strings.Contains(got, "https://www.youtube.com/@handle/videos") {
		t.Fatalf("a handle is listed under /@handle: %s", got)
	}
}

func TestPlaybackManifestTakesTheTallestHLSFormat(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, listingScript)
	manifest, err := p.PlaybackManifest(context.Background(), "dQw4w9WgXcQ", "")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.URL != "https://manifest.googlevideo.com/high.m3u8" || manifest.Headers["Referer"] != "https://www.youtube.com/" {
		t.Fatalf("manifest = %+v (the headers fall back to the video's)", manifest)
	}
	if got := calls(t, log); !strings.Contains(got, "--extractor-args youtube:player_client=default,web_safari https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
		t.Fatalf("arguments: %s", got)
	}
	none, _ := fakeYTDLP(t, `printf '%s\n' '{"id":"dQw4w9WgXcQ","formats":[{"protocol":"https","manifest_url":""}]}'`)
	_, err = none.PlaybackManifest(context.Background(), "dQw4w9WgXcQ", "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
}

func TestCatalogStreamsEveryVideoWithTheChannelAsOwner(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, listingScript)
	channel := platform.Channel{ID: "UCfixture", Platform: "youtube", Name: "Fixture channel", Thumbnail: "https://yt3.googleusercontent.com/a=s0"}
	var got []platform.Video
	err := p.CatalogSync().Catalog(context.Background(), channel, "", func(v platform.Video) error {
		got = append(got, v)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "fixture-video" || got[0].ChannelID != "UCfixture" || got[0].ChannelName != "Fixture channel" || got[0].ChannelThumbnail != channel.Thumbnail || got[1].ChannelID != "UCexplicit" {
		t.Fatalf("catalog: %+v", got)
	}
	if args := calls(t, log); !strings.Contains(args, "/channel/UCfixture/videos --dump-json --flat-playlist --lazy-playlist --no-download") || strings.Contains(args, "--playlist-end") {
		t.Fatalf("the catalog is not capped: %s", args)
	}
}

func TestCatalogStopsOnTheVisitorsErrorAndReturnsItUnchanged(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, listingScript)
	stop := errors.New("store is full")
	count := 0
	err := p.CatalogSync().Catalog(context.Background(), platform.Channel{ID: "UCfixture"}, "", func(platform.Video) error {
		count++
		return stop
	})
	if !errors.Is(err, stop) || count != 1 {
		t.Fatalf("err=%v after %d videos", err, count)
	}
}

func TestCatalogRejectsEntriesThatAreNotVideos(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"no id":    `{"title":"x"}`,
		"playlist": `{"id":"PL1","_type":"playlist"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, _ := fakeYTDLP(t, "printf '%s\\n' '"+line+"'")
			err := p.CatalogSync().Catalog(context.Background(), platform.Channel{ID: "@handle"}, "", func(platform.Video) error { return nil })
			assertCode(t, err, "SOURCE_REQUEST_FAILED")
		})
	}
}
