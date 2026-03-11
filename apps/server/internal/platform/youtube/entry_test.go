package youtube

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestFilterArtworkThumbnailsDropsVideoFramesAndSignedDuplicates(t *testing.T) {
	t.Parallel()
	urls := []string{
		"https://i.ytimg.com/vi/video/maxresdefault.jpg",
		"https://i.ytimg.com/vi/video/3.jpg",
		"https://i.ytimg.com/vi_webp/video/3.webp",
		"https://i.ytimg.com/vi/video/2.jpg",
		"https://i.ytimg.com/vi_webp/video/2.webp",
		"https://i.ytimg.com/vi/video/1.jpg",
		"https://i.ytimg.com/vi_webp/video/1.webp",
		"https://i.ytimg.com/vi/video/0.jpg",
		"https://i.ytimg.com/vi_webp/video/0.webp",
		"https://i.ytimg.com/vi/video/mq3.jpg",
		"https://i.ytimg.com/vi/video/mq2.jpg",
		"https://i.ytimg.com/vi/video/mq1.jpg",
		"https://i.ytimg.com/vi/video/hq3.jpg",
		"https://i.ytimg.com/vi/video/hq2.jpg",
		"https://i.ytimg.com/vi/video/hq1.jpg",
		"https://i.ytimg.com/vi/video/sd3.jpg",
		"https://i.ytimg.com/vi/video/sd2.jpg",
		"https://i.ytimg.com/vi/video/sd1.jpg",
		"https://i.ytimg.com/vi/video/default.jpg",
		"https://i.ytimg.com/vi_webp/video/default.webp",
		"https://i.ytimg.com/vi/video/mqdefault.jpg",
		"https://i.ytimg.com/vi/video/hqdefault.jpg?sqp=signed&rs=abc",
		"https://i.ytimg.com/vi/video/hqdefault.jpg?sqp=other&rs=def",
		"https://i.ytimg.com/vi/video/hqdefault.jpg",
		"https://i.ytimg.com/vi_webp/video/hqdefault.webp",
		"https://i.ytimg.com/vi/video/sddefault.jpg",
		"https://i.ytimg.com/vi/video/hq720.jpg",
	}
	want := []string{
		"https://i.ytimg.com/vi/video/maxresdefault.jpg",
		"https://i.ytimg.com/vi/video/default.jpg",
		"https://i.ytimg.com/vi_webp/video/default.webp",
		"https://i.ytimg.com/vi/video/mqdefault.jpg",
		"https://i.ytimg.com/vi/video/hqdefault.jpg",
		"https://i.ytimg.com/vi_webp/video/hqdefault.webp",
		"https://i.ytimg.com/vi/video/sddefault.jpg",
		"https://i.ytimg.com/vi/video/hq720.jpg",
	}
	if got := filterArtworkThumbnails(urls); !slices.Equal(got, want) {
		t.Fatalf("artwork thumbnails = %v, want %v", got, want)
	}
}

func TestFilterArtworkThumbnailsKeepsFramesWhenNothingElseExists(t *testing.T) {
	t.Parallel()
	frames := []string{"https://i.ytimg.com/vi/video/1.jpg", "https://i.ytimg.com/vi/video/2.jpg"}
	if got := filterArtworkThumbnails(frames); !slices.Equal(got, frames) {
		t.Fatalf("frame-only thumbnails = %v, want them kept", got)
	}
}

func TestFilterArtworkThumbnailsLeavesOtherHostsAlone(t *testing.T) {
	t.Parallel()
	urls := []string{"https://example.test/3.jpg", "https://example.test/poster.jpg"}
	if got := filterArtworkThumbnails(urls); !slices.Equal(got, urls) {
		t.Fatalf("foreign thumbnails = %v, want them kept", got)
	}
}

func TestEntryThumbnailsPreferArtworkAndDropFrames(t *testing.T) {
	t.Parallel()
	entry := ytEntry{Thumbnail: "https://i.ytimg.com/vi/video/maxresdefault.jpg", Thumbnails: []ytThumbnail{
		{ID: "0", URL: "https://i.ytimg.com/vi/video/1.jpg"},
		{ID: "1", URL: "https://i.ytimg.com/vi_webp/video/1.webp"},
		{ID: "2", URL: "https://i.ytimg.com/vi/video/hqdefault.jpg?sqp=signed&rs=abc"},
		{ID: "3", URL: "https://i.ytimg.com/vi/video/hqdefault.jpg"},
		{ID: "4", URL: "https://i.ytimg.com/vi/video/hq720.jpg"},
	}}
	want := []string{
		"https://i.ytimg.com/vi/video/maxresdefault.jpg",
		"https://i.ytimg.com/vi/video/hqdefault.jpg",
		"https://i.ytimg.com/vi/video/hq720.jpg",
	}
	if got := entry.thumbnails(); !slices.Equal(got, want) {
		t.Fatalf("thumbnails = %v, want %v", got, want)
	}
}

func TestEntryVideoUsesFlatListingArtworkWhenThumbnailIsMissing(t *testing.T) {
	t.Parallel()
	entry := ytEntry{ID: "video", Title: "Titre traduit", Thumbnails: []ytThumbnail{
		{ID: "0", URL: "https://i.ytimg.com/vi/video/1.jpg"},
		{ID: "1", URL: "https://i.ytimg.com/vi/video/hqdefault.jpg?sqp=signed&rs=abc"},
		{ID: "2", URL: "https://i.ytimg.com/vi/video/hqdefault.jpg"},
	}}
	video := entry.video()
	if video.Thumbnail != "https://i.ytimg.com/vi/video/hqdefault.jpg" || !slices.Equal(video.Thumbnails, []string{video.Thumbnail}) || video.OriginalTitleVerified {
		t.Fatalf("flat listing lost artwork or claimed verification: %+v", video)
	}
}

func TestEntryVideoDoesNotUseScrubberFrameWhenThumbnailIsFrame(t *testing.T) {
	t.Parallel()
	entry := ytEntry{ID: "video", Thumbnail: "https://i.ytimg.com/vi/video/2.jpg", Thumbnails: []ytThumbnail{{ID: "0", URL: "https://i.ytimg.com/vi/video/maxresdefault.jpg"}}}
	if got := entry.video().Thumbnail; got != "https://i.ytimg.com/vi/video/maxresdefault.jpg" {
		t.Fatalf("thumbnail = %q, want artwork", got)
	}
}

func entryOf(t *testing.T, raw string) ytEntry {
	t.Helper()
	var entry ytEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestVideoPublicationTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input, published string
	}{
		{"date only", `{"id":"one","upload_date":"20231114"}`, "2023-11-14"},
		{"exact timestamp", `{"id":"one","upload_date":"20231114","timestamp":1700000000}`, "2023-11-14T22:13:20Z"},
		{"timestamp without date", `{"id":"one","timestamp":1700000000}`, "2023-11-14T22:13:20Z"},
		{"neither", `{"id":"one"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if video := entryOf(t, tc.input).video(); video.Published != tc.published {
				t.Fatalf("unexpected publication: %+v", video)
			}
		})
	}
}

func TestEntryVideoReadsTheOriginalAudioLanguageNotTheDubbedOne(t *testing.T) {
	t.Parallel()
	video := entryOf(t, `{"id":"one","language":"en","formats":[
		{"acodec":"mp4a","language":"en","language_preference":5},
		{"acodec":"none","language":"de","language_preference":10},
		{"acodec":"mp4a","language":"fr-FR","language_preference":10}]}`).video()
	if video.OriginalLanguage != "fr-FR" {
		t.Fatalf("original language = %q", video.OriginalLanguage)
	}
	if got := entryOf(t, `{"id":"one","language":"it"}`).video().OriginalLanguage; got != "it" {
		t.Fatalf("language without formats = %q", got)
	}
}

func TestEntryVideoMapsTheOwnerTagsAndLiveFlag(t *testing.T) {
	t.Parallel()
	video := entryOf(t, `{"id":"one","title":"T","uploader_id":"@handle","uploader":"Up","categories":["Music"],"live_status":"is_live"}`).video()
	if video.Platform != "youtube" || video.ChannelID != "handle" || video.ChannelName != "Up" || video.Category != "Music" || !video.Live || len(video.Tags) != 1 || video.Tags[0] != "Music" {
		t.Fatalf("unexpected video: %+v", video)
	}
	if video.Chapters == nil || len(video.Chapters) != 0 || video.OriginalTitle != "T" {
		t.Fatalf("chapters must be an empty array and the original title the title: %+v", video)
	}
}

func TestChannelAvatarChoice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, entry, want string }{
		{
			"explicit avatar is moved to the reliable host",
			`{"channel_thumbnail":"https://yt3.ggpht.com/explicit=s0","thumbnails":[]}`,
			"https://yt3.googleusercontent.com/explicit=s0",
		},
		{
			"video artwork is never an avatar",
			`{"thumbnail":"https://i.ytimg.com/vi/fixture-video/hqdefault.jpg","thumbnails":[]}`,
			"",
		},
		{
			"a banner is never an avatar",
			`{"thumbnail":"https://yt3.googleusercontent.com/banner=s0","thumbnails":[{"id":"banner_uncropped","url":"https://yt3.googleusercontent.com/banner=s0","preference":-5}]}`,
			"",
		},
		{
			"the uncropped avatar beats a higher-preference banner",
			`{"thumbnails":[{"id":"banner_uncropped","url":"https://yt3.googleusercontent.com/banner=s0","preference":10},{"id":"avatar_uncropped","url":"https://yt3.googleusercontent.com/fixture-avatar=s0","preference":1}]}`,
			"https://yt3.googleusercontent.com/fixture-avatar=s0",
		},
		{
			"the display-ready square avatar beats the uncropped one",
			`{"thumbnails":[{"id":"avatar_uncropped","url":"https://yt3.googleusercontent.com/avatar=s0","preference":1},{"id":"cropped","url":"https://yt3.googleusercontent.com/avatar=s176-c","width":176,"height":176}]}`,
			"https://yt3.googleusercontent.com/avatar=s176-c",
		},
		{
			"the widest square wins",
			`{"thumbnails":[{"id":"a","url":"https://yt3.googleusercontent.com/small=s88-c","width":88,"height":88},{"id":"b","url":"https://yt3.googleusercontent.com/big=s900-c","width":900,"height":900}]}`,
			"https://yt3.googleusercontent.com/big=s900-c",
		},
		{
			"protocol-relative addresses become https",
			`{"thumbnails":[{"id":"a","url":"//yt3.ggpht.com/ego=s176-c","width":176,"height":176}]}`,
			"https://yt3.googleusercontent.com/ego=s176-c",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := entryOf(t, tc.entry).channelThumbnail(); got != tc.want {
				t.Fatalf("avatar = %q, want %q", got, tc.want)
			}
		})
	}
}
