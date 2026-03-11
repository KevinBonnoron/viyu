package youtube

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// ytThumbnail is one image yt-dlp lists for a video or a channel.
type ytThumbnail struct {
	ID         string  `json:"id"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	URL        string  `json:"url"`
	Preference float64 `json:"preference"`
}

// ytFormat is one format of a video; only what playback and the original language need.
type ytFormat struct {
	Protocol           string            `json:"protocol"`
	Height             int               `json:"height"`
	ManifestURL        string            `json:"manifest_url"`
	HTTPHeaders        map[string]string `json:"http_headers"`
	Language           string            `json:"language"`
	LanguagePreference float64           `json:"language_preference"`
	AudioCodec         string            `json:"acodec"`
}

// ytEntry is one yt-dlp JSON object: a video, a flat listing entry, a channel playlist or a search hit.
type ytEntry struct {
	ID               string             `json:"id"`
	Type             string             `json:"_type"`
	IEKey            string             `json:"ie_key"`
	URL              string             `json:"url"`
	Title            string             `json:"title"`
	FullTitle        string             `json:"fulltitle"`
	LiveStatus       string             `json:"live_status"`
	IsLive           bool               `json:"is_live"`
	Language         string             `json:"language"`
	ChannelID        string             `json:"channel_id"`
	UploaderID       string             `json:"uploader_id"`
	Channel          string             `json:"channel"`
	Uploader         string             `json:"uploader"`
	ChannelURL       string             `json:"channel_url"`
	ChannelThumbnail string             `json:"channel_thumbnail"`
	Creators         []string           `json:"creators"`
	Thumbnail        string             `json:"thumbnail"`
	Thumbnails       []ytThumbnail      `json:"thumbnails"`
	Description      string             `json:"description"`
	Duration         float64            `json:"duration"`
	Tags             []string           `json:"tags"`
	Chapters         []platform.Chapter `json:"chapters"`
	Categories       []string           `json:"categories"`
	UploadDate       string             `json:"upload_date"`
	ViewCount        float64            `json:"view_count"`
	LikeCount        *float64           `json:"like_count"`
	DislikeCount     *float64           `json:"dislike_count"`
	Timestamp        float64            `json:"timestamp"`
	Entries          []ytEntry          `json:"entries"`
	Followers        float64            `json:"channel_follower_count"`
	HTTPHeaders      map[string]string  `json:"http_headers"`
	Formats          []ytFormat         `json:"formats"`
}

// artworkFrameName names the preview frames YouTube lists beside a video's poster (0.jpg, mq1.jpg…).
var artworkFrameName = regexp.MustCompile(`^(?:[0-3]|(?:mq|hq|sd)[123])\.(?:jpg|webp)$`)

func isYouTubeVideoImage(u *url.URL) bool {
	return u.Host == "i.ytimg.com" && (strings.HasPrefix(u.Path, "/vi/") || strings.HasPrefix(u.Path, "/vi_webp/"))
}

// filterArtworkThumbnails keeps the provider thumbnails that represent the video's custom poster.
// YouTube also lists preview frames taken inside the video (1.jpg, mq1.jpg, hq2.jpg, ...); those belong
// to the scrubber, not the gallery, and signed duplicate URLs of the same variant collapse to a single
// entry. It runs when a video is ingested: what is stored is already filtered.
func filterArtworkThumbnails(urls []string) []string {
	if len(urls) == 0 {
		return urls
	}
	filtered := make([]string, 0, len(urls))
	byName := make(map[string]int, len(urls))
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || !isYouTubeVideoImage(u) {
			filtered = append(filtered, raw)
			continue
		}
		name := path.Base(u.Path)
		if artworkFrameName.MatchString(name) {
			continue
		}
		if i, ok := byName[name]; ok {
			if strings.Contains(filtered[i], "?") && !strings.Contains(raw, "?") {
				filtered[i] = raw
			}
			continue
		}
		byName[name] = len(filtered)
		filtered = append(filtered, raw)
	}
	if len(filtered) == 0 {
		return urls
	}
	return filtered
}

func (d ytEntry) thumbnails() []string {
	seen := make(map[string]bool, len(d.Thumbnails)+1)
	result := make([]string, 0, len(d.Thumbnails)+1)
	appendUnique := func(url string) {
		if url != "" && !seen[url] {
			seen[url] = true
			result = append(result, url)
		}
	}
	appendUnique(d.Thumbnail)
	for _, thumbnail := range d.Thumbnails {
		appendUnique(thumbnail.URL)
	}
	return filterArtworkThumbnails(result)
}

// notAvatar tells whether a listed image is a banner or a non-square picture, never a channel avatar.
func (t ytThumbnail) notAvatar() bool {
	return strings.HasPrefix(t.ID, "banner") || t.Preference < 0 || (t.Width > 0 && t.Height > 0 && t.Width != t.Height)
}

// channelThumbnail picks the channel's avatar. Channel listings contain avatars, banners and sometimes
// video artwork: prefer the provider's explicit avatar rather than the generic thumbnail.
func (d ytEntry) channelThumbnail() string {
	if platform.ChannelImage(d.ChannelThumbnail) {
		return platform.ChannelImageURL(d.ChannelThumbnail)
	}
	// Prefer YouTube's display-ready square avatar. Its uncropped '=s0' variant may return an error
	// document even when the cropped image works.
	cropped := ""
	croppedWidth := 0
	for _, image := range d.Thumbnails {
		if image.Width > 0 && image.Width == image.Height && image.Preference >= 0 && !strings.HasPrefix(image.ID, "banner") && platform.ChannelImage(image.URL) && image.Width > croppedWidth {
			cropped, croppedWidth = image.URL, image.Width
		}
	}
	if cropped != "" {
		return platform.ChannelImageURL(cropped)
	}
	for _, image := range d.Thumbnails {
		if image.ID == "avatar_uncropped" && platform.ChannelImage(image.URL) {
			return platform.ChannelImageURL(image.URL)
		}
	}
	best := ""
	width := 0
	for _, image := range d.Thumbnails {
		if image.notAvatar() || !platform.ChannelImage(image.URL) {
			continue
		}
		if best == "" || image.Width > width {
			best, width = image.URL, image.Width
		}
	}
	if best != "" {
		return platform.ChannelImageURL(best)
	}
	if platform.ChannelImage(d.Thumbnail) {
		for _, image := range d.Thumbnails {
			if image.URL == d.Thumbnail && image.notAvatar() {
				return ""
			}
		}
		return platform.ChannelImageURL(d.Thumbnail)
	}
	return ""
}

// video converts the entry to a video: a flat listing entry gives a partial one, a video page a full one.
func (d ytEntry) video() platform.Video {
	tags := d.Tags
	if tags == nil {
		tags = d.Categories
	}
	if tags == nil {
		tags = []string{}
	}
	chapters := d.Chapters
	if chapters == nil {
		chapters = []platform.Chapter{}
	}
	// The exact time when yt-dlp gives one, else the day of the upload.
	published := ""
	if d.Timestamp > 0 {
		seconds := int64(d.Timestamp)
		nanoseconds := int64((d.Timestamp - float64(seconds)) * float64(time.Second))
		published = time.Unix(seconds, nanoseconds).UTC().Format(time.RFC3339Nano)
	} else if t, err := time.Parse("20060102", d.UploadDate); err == nil {
		published = t.Format(platform.DayLayout)
	}
	thumbnails := d.thumbnails()
	thumbnail := ""
	if len(thumbnails) > 0 {
		thumbnail = thumbnails[0]
	}
	return platform.Video{
		ID: d.ID, Platform: platformID, Title: platform.First(d.Title, d.FullTitle),
		OriginalTitle:    platform.First(d.FullTitle, d.Title),
		OriginalLanguage: d.originalAudioLanguage(),
		ChannelID:        platform.First(d.ChannelID, strings.TrimPrefix(d.UploaderID, "@")),
		ChannelName:      platform.First(d.Channel, d.Uploader), ChannelThumbnail: platform.ChannelImageURL(d.ChannelThumbnail),
		Thumbnail: thumbnail, Thumbnails: thumbnails, Description: d.Description, Duration: d.Duration,
		Creators: d.Creators,
		Tags:     tags, Chapters: chapters, Published: published, ViewCount: d.ViewCount,
		LikeCount: d.LikeCount, DislikeCount: d.DislikeCount, Live: d.IsLive || d.LiveStatus == "is_live", Category: platform.First(d.Categories...),
	}
}

func (d ytEntry) originalAudioLanguage() string {
	for _, format := range d.Formats {
		// yt-dlp reserves preference 10 for the source audio, above the visitor's default/dubbed track
		// (5). Do not infer it from UI locale.
		if format.AudioCodec != "" && format.AudioCodec != "none" && format.Language != "" && format.LanguagePreference >= 10 {
			return format.Language
		}
	}
	return d.Language
}

// playbackManifest is the HLS manifest of the tallest format that has one.
func (d ytEntry) playbackManifest() platform.Manifest {
	result := platform.Manifest{}
	height := -1
	for _, format := range d.Formats {
		if strings.Contains(format.Protocol, "m3u8") && format.ManifestURL != "" && format.Height > height {
			result = platform.Manifest{URL: format.ManifestURL, Headers: format.HTTPHeaders}
			if len(result.Headers) == 0 {
				result.Headers = d.HTTPHeaders
			}
			height = format.Height
		}
	}
	return result
}
