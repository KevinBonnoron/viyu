package youtube

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	feedTimeout = 8 * time.Second
	feedLimit   = 2 << 20
)

// publication is what the RSS feed knows of a video.
type publication struct{ Published, Title string }

// publicationMetadata reads the entries of a channel's RSS feed. The date is the one the video was
// published on, not the one of its last update; an entry without a valid date is ignored.
func publicationMetadata(reader io.Reader) (map[string]publication, error) {
	var feed struct {
		Entries []struct {
			ID        string `xml:"videoId"`
			Published string `xml:"published"`
			Title     string `xml:"title"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(reader).Decode(&feed); err != nil {
		return nil, err
	}
	result := make(map[string]publication, len(feed.Entries))
	for _, entry := range feed.Entries {
		published, err := time.Parse(time.RFC3339, entry.Published)
		if err == nil && entry.ID != "" {
			result[entry.ID] = publication{Published: published.UTC().Format(time.RFC3339), Title: entry.Title}
		}
	}
	return result, nil
}

// EnrichListing completes a channel listing with the exact publication times and the canonical titles of
// the channel's RSS feed (a flat listing knows neither), then orders it newest first. A feed that does not
// answer leaves the listing as it is.
func (p *Platform) EnrichListing(ctx context.Context, channel string, videos []platform.Video) {
	defer platform.SortByPublication(videos)
	if !channelIDPattern.MatchString(channel) {
		return
	}
	entries, err := p.feedEntries(ctx, channel)
	if err != nil {
		p.log.Warn("Channel publication dates unavailable", "channel", channel, "error", err)
		return
	}
	for index := range videos {
		if data, ok := entries[videos[index].ID]; ok {
			videos[index].Published = platform.CarryPublished(videos[index].Published, data.Published)
			if data.Title != "" {
				videos[index].Title = data.Title
				videos[index].OriginalTitle = data.Title
				videos[index].OriginalTitleVerified = true
			}
		}
	}
}

func (p *Platform) feedEntries(ctx context.Context, channel string) (map[string]publication, error) {
	ctx, cancel := context.WithTimeout(ctx, feedTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.feedURL+"?channel_id="+url.QueryEscape(channel), nil)
	if err != nil {
		return nil, err
	}
	response, err := p.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &feedStatusError{status: response.StatusCode}
	}
	return publicationMetadata(io.LimitReader(response.Body, feedLimit))
}

type feedStatusError struct{ status int }

func (e *feedStatusError) Error() string { return "the feed answered " + http.StatusText(e.status) }
