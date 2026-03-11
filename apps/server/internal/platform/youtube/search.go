package youtube

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// searchChannel reads a search hit that is a channel rather than a video.
func (d *ytEntry) searchChannel() (platform.ChannelCard, bool) {
	id := platform.First(d.ChannelID, d.ID)
	if !strings.HasPrefix(id, "UC") || !(d.Type == "playlist" || d.IEKey == "YoutubeTab" || strings.Contains(d.URL, "/channel/") || strings.Contains(d.URL, "/@")) {
		return platform.ChannelCard{}, false
	}
	return platform.ChannelCard{ID: id, Platform: platformID, Name: platform.First(d.Channel, d.Uploader, d.Title), Thumbnail: platform.ChannelImageURL(d.channelThumbnail())}, true
}

// Search answers one page of results. The first page promotes the channel whose name is the query. A
// connected search is private: its results are marked with their source.
func (p *Platform) Search(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
	if strings.TrimSpace(query) == "" {
		return platform.SearchPage{}, apierr.InvalidInput("Provide a search query.")
	}
	start, end, err := platform.PageRange(page, platform.SearchPageSize)
	if err != nil {
		return platform.SearchPage{}, err
	}
	result := platform.SearchPage{Results: []platform.Result{}, Page: page}
	// One result more than the page holds says whether there is a next page, and is read again by it.
	entries, err := p.entries(ctx, []string{"ytsearch" + strconv.Itoa(end) + ":" + query, "--dump-json", "--flat-playlist", "--no-download", "--playlist-start", strconv.Itoa(start), "--playlist-end", strconv.Itoa(end)}, session.CookieFile)
	if err != nil {
		return platform.SearchPage{}, err
	}
	result.HasMore = len(entries) > platform.SearchPageSize
	if result.HasMore {
		entries = entries[:platform.SearchPageSize]
	}
	seenChannels := make(map[string]struct{})
	if page == 1 {
		// A mixed YouTube search can omit an exact-name channel entirely. Use YouTube's channel filter,
		// not a guessed handle or video owner.
		channels, err := p.entries(ctx, []string{"https://www.youtube.com/results?search_query=" + url.QueryEscape(query) + "&sp=EgIQAg%3D%3D", "--dump-json", "--flat-playlist", "--no-download", "--playlist-end", "5"}, session.CookieFile)
		if err != nil {
			return platform.SearchPage{}, err
		}
		for index := range channels {
			channel, ok := channels[index].searchChannel()
			if ok && strings.EqualFold(strings.TrimSpace(channel.Name), strings.TrimSpace(query)) {
				if _, exists := seenChannels[channel.ID]; !exists {
					seenChannels[channel.ID] = struct{}{}
					result.Results = append(result.Results, platform.Result{Channel: &channel})
					break
				}
			}
		}
	}
	for index := range entries {
		entry := &entries[index]
		if channel, ok := entry.searchChannel(); ok {
			if _, exists := seenChannels[channel.ID]; !exists {
				seenChannels[channel.ID] = struct{}{}
				result.Results = append(result.Results, platform.Result{Channel: &channel})
			}
		} else {
			video := entry.video()
			result.Results = append(result.Results, platform.Result{Video: &video})
		}
	}
	if session.Connected() {
		for _, item := range result.Results {
			switch {
			case item.Video != nil:
				item.Video.Source = platformID
			case item.Channel != nil:
				item.Channel.Source = platformID
			}
		}
	}
	return result, nil
}
