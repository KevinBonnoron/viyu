package twitch

import (
	"strings"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// entry is what yt-dlp prints for a Twitch VOD, a stream or a channel's VOD listing (only the fields used).
type entry struct {
	ID               string   `json:"id"`
	Type             string   `json:"_type"`
	Title            string   `json:"title"`
	FullTitle        string   `json:"fulltitle"`
	LiveStatus       string   `json:"live_status"`
	IsLive           bool     `json:"is_live"`
	Language         string   `json:"language"`
	ChannelID        string   `json:"channel_id"`
	UploaderID       string   `json:"uploader_id"`
	Channel          string   `json:"channel"`
	Uploader         string   `json:"uploader"`
	ChannelThumbnail string   `json:"channel_thumbnail"`
	Creators         []string `json:"creators"`
	Thumbnail        string   `json:"thumbnail"`
	Thumbnails       []struct {
		ID         string  `json:"id"`
		Width      int     `json:"width"`
		Height     int     `json:"height"`
		URL        string  `json:"url"`
		Preference float64 `json:"preference"`
	} `json:"thumbnails"`
	Description  string             `json:"description"`
	Duration     float64            `json:"duration"`
	Tags         []string           `json:"tags"`
	Chapters     []platform.Chapter `json:"chapters"`
	Categories   []string           `json:"categories"`
	UploadDate   string             `json:"upload_date"`
	ViewCount    float64            `json:"view_count"`
	LikeCount    *float64           `json:"like_count"`
	DislikeCount *float64           `json:"dislike_count"`
	Timestamp    float64            `json:"timestamp"`
	Entries      []entry            `json:"entries"`
	Followers    float64            `json:"channel_follower_count"`
	HTTPHeaders  map[string]string  `json:"http_headers"`
	Formats      []struct {
		Protocol    string            `json:"protocol"`
		Height      int               `json:"height"`
		ManifestURL string            `json:"manifest_url"`
		HTTPHeaders map[string]string `json:"http_headers"`
	} `json:"formats"`
}

func (e entry) thumbnails() []string {
	seen := make(map[string]bool, len(e.Thumbnails)+1)
	result := make([]string, 0, len(e.Thumbnails)+1)
	add := func(url string) {
		if url != "" && !seen[url] {
			seen[url] = true
			result = append(result, url)
		}
	}
	add(e.Thumbnail)
	for _, thumbnail := range e.Thumbnails {
		add(thumbnail.URL)
	}
	return result
}

// channelThumbnail prefers the provider's explicit avatar over the generic thumbnail: channel listings
// contain avatars, banners and sometimes video artwork.
func (e entry) channelThumbnail() string {
	if platform.ChannelImage(e.ChannelThumbnail) {
		return platform.ChannelImageURL(e.ChannelThumbnail)
	}
	cropped, croppedWidth := "", 0
	for _, image := range e.Thumbnails {
		if image.Width > 0 && image.Width == image.Height && image.Preference >= 0 && !strings.HasPrefix(image.ID, "banner") && platform.ChannelImage(image.URL) && image.Width > croppedWidth {
			cropped, croppedWidth = image.URL, image.Width
		}
	}
	if cropped != "" {
		return platform.ChannelImageURL(cropped)
	}
	for _, image := range e.Thumbnails {
		if image.ID == "avatar_uncropped" && platform.ChannelImage(image.URL) {
			return platform.ChannelImageURL(image.URL)
		}
	}
	best, width := "", 0
	for _, image := range e.Thumbnails {
		if strings.HasPrefix(image.ID, "banner") || image.Preference < 0 || (image.Width > 0 && image.Height > 0 && image.Width != image.Height) || !platform.ChannelImage(image.URL) {
			continue
		}
		if best == "" || image.Width > width {
			best, width = image.URL, image.Width
		}
	}
	if best != "" {
		return platform.ChannelImageURL(best)
	}
	if platform.ChannelImage(e.Thumbnail) {
		for _, image := range e.Thumbnails {
			if image.URL == e.Thumbnail && (strings.HasPrefix(image.ID, "banner") || image.Preference < 0 || (image.Width > 0 && image.Height > 0 && image.Width != image.Height)) {
				return ""
			}
		}
		return platform.ChannelImageURL(e.Thumbnail)
	}
	return ""
}

// playbackManifest is the tallest HLS format the entry lists.
func (e entry) playbackManifest() platform.Manifest {
	result := platform.Manifest{}
	height := -1
	for _, format := range e.Formats {
		if strings.Contains(format.Protocol, "m3u8") && format.ManifestURL != "" && format.Height > height {
			result = platform.Manifest{URL: format.ManifestURL, Headers: format.HTTPHeaders}
			if len(result.Headers) == 0 {
				result.Headers = e.HTTPHeaders
			}
			height = format.Height
		}
	}
	return result
}

// video is the entry as a Twitch video: a VOD has exactly one title, so it is its own original.
func (e entry) video() platform.Video {
	tags := e.Tags
	if tags == nil {
		tags = e.Categories
	}
	if tags == nil {
		tags = []string{}
	}
	chapters := e.Chapters
	if chapters == nil {
		chapters = []platform.Chapter{}
	}
	// The exact time when yt-dlp gives one, else the day of the upload.
	published := ""
	if e.Timestamp > 0 {
		seconds := int64(e.Timestamp)
		nanoseconds := int64((e.Timestamp - float64(seconds)) * float64(time.Second))
		published = time.Unix(seconds, nanoseconds).UTC().Format(time.RFC3339Nano)
	} else if t, err := time.Parse("20060102", e.UploadDate); err == nil {
		published = t.Format(platform.DayLayout)
	}
	thumbnails := e.thumbnails()
	thumbnail := ""
	if len(thumbnails) > 0 {
		thumbnail = thumbnails[0]
	}
	title := platform.First(e.Title, e.FullTitle)
	return platform.Video{
		ID: strings.TrimPrefix(e.ID, "v"), Platform: ID, Title: title,
		OriginalTitle: platform.First(e.FullTitle, e.Title, title), OriginalTitleVerified: true,
		OriginalLanguage: e.Language,
		ChannelID:        platform.First(e.UploaderID, e.ChannelID),
		ChannelName:      platform.First(e.Channel, e.Uploader), ChannelThumbnail: platform.ChannelImageURL(e.ChannelThumbnail),
		Thumbnail: thumbnail, Thumbnails: thumbnails, Description: e.Description, Duration: e.Duration,
		Creators: e.Creators,
		Tags:     tags, Chapters: chapters, Published: published, ViewCount: e.ViewCount,
		LikeCount: e.LikeCount, DislikeCount: e.DislikeCount, Live: e.IsLive || e.LiveStatus == "is_live", Category: platform.First(e.Categories...),
	}
}
