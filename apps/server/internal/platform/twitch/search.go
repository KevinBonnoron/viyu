package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Twitch's Helix search only exposes channels. Its web search supports VODs and cursor pagination; request
// one index at a time, as Twitch ignores later indices when multiple targets are supplied.
const searchQuery = `query ViyuSearch($query: String!, $options: SearchForOptions) {
  searchFor(userQuery: $query, platform: "web", options: $options) {
    channels {
      cursor
      edges { item { ... on User { login displayName profileImageURL(width: 150) } } }
    }
    videos {
      cursor
      edges { item { ... on Video {
        id title createdAt lengthSeconds viewCount
        previewThumbnailURL(width: 1280, height: 720)
        owner { login displayName profileImageURL(width: 150) }
      } } }
    }
  }
}`

type searchChannel struct {
	Login     string `json:"login"`
	Name      string `json:"displayName"`
	Thumbnail string `json:"profileImageURL"`
}

type searchVideo struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	CreatedAt string         `json:"createdAt"`
	Duration  float64        `json:"lengthSeconds"`
	ViewCount float64        `json:"viewCount"`
	Thumbnail string         `json:"previewThumbnailURL"`
	Owner     *searchChannel `json:"owner"`
}

type searchConnection[T any] struct {
	Cursor string `json:"cursor"`
	Edges  []struct {
		Item *T `json:"item"`
	} `json:"edges"`
}

type searchResults struct {
	Channels *searchConnection[searchChannel] `json:"channels"`
	Videos   *searchConnection[searchVideo]   `json:"videos"`
}

func (p *Platform) searchBatch(ctx context.Context, who identity, token, query, kind, cursor string, limit int) (*searchResults, error) {
	target := map[string]any{"index": kind, "limit": limit}
	if cursor != "" {
		target["cursor"] = cursor
	}
	var response struct {
		Data *struct {
			SearchFor *searchResults `json:"searchFor"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	err := p.graphQL(ctx, map[string]any{
		"query": searchQuery,
		"variables": map[string]any{
			"query":   query,
			"options": map[string]any{"targets": []any{target}},
		},
	}, who.ClientID, token, &response)
	if err != nil {
		return nil, err
	}
	if len(response.Errors) != 0 || response.Data == nil || response.Data.SearchFor == nil || response.Data.SearchFor.Channels == nil || response.Data.SearchFor.Videos == nil {
		return nil, apierr.RequestFailed().WithCause(errUnexpectedAnswer)
	}
	return response.Data.SearchFor, nil
}

func (item searchVideo) video(source string) platform.Video {
	thumbnails := []string{}
	if item.Thumbnail != "" {
		thumbnails = append(thumbnails, item.Thumbnail)
	}
	return platform.Video{
		ID: item.ID, Platform: ID, Source: source,
		Title: item.Title, OriginalTitle: item.Title, OriginalTitleVerified: true,
		ChannelID: item.Owner.Login, ChannelName: platform.First(item.Owner.Name, item.Owner.Login), ChannelThumbnail: item.Owner.Thumbnail,
		Thumbnail: item.Thumbnail, Thumbnails: thumbnails, Duration: item.Duration, ViewCount: item.ViewCount,
		Published: publication(item.CreatedAt), Tags: []string{}, Chapters: []platform.Chapter{},
	}
}

// publication is the exact publication time of a Twitch video, from its creation timestamp.
func publication(createdAt string) string {
	if created, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		return created.UTC().Format(time.RFC3339Nano)
	}
	return ""
}

// Search looks channels and VODs up through Twitch's web search; a connected session searches as the account.
func (p *Platform) Search(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
	if session.Connected() && session.Cookies == "" {
		return platform.SearchPage{}, apierr.NotConnected()
	}
	return p.search(ctx, session.Cookies, query, page)
}

func (p *Platform) search(ctx context.Context, cookies, query string, page int) (platform.SearchPage, error) {
	start, end, err := platform.PageRange(page, platform.SearchPageSize)
	if err != nil {
		return platform.SearchPage{}, err
	}
	who := identity{ClientID: publicClientID}
	token, source := "", ""
	if cookies != "" {
		if who, err = p.identity(ctx, cookies); err != nil {
			return platform.SearchPage{}, err
		}
		if token, err = authToken(cookies); err != nil {
			return platform.SearchPage{}, err
		}
		source = ID
	}
	channels := make([]platform.ChannelCard, 0, min(end, 100))
	videos := make([]platform.Video, 0, min(end, 100))
	for _, kind := range []string{"CHANNEL", "VOD"} {
		cursor, loaded := "", 0
		seen := make(map[string]bool)
		for loaded < end {
			batch, err := p.searchBatch(ctx, who, token, query, kind, cursor, min(100, end-loaded))
			if err != nil {
				return platform.SearchPage{}, err
			}
			next, count := "", 0
			if kind == "CHANNEL" {
				next, count = batch.Channels.Cursor, len(batch.Channels.Edges)
				for _, edge := range batch.Channels.Edges {
					item := edge.Item
					if item != nil && loginPattern.MatchString(item.Login) && !seen[item.Login] {
						seen[item.Login] = true
						channels = append(channels, platform.ChannelCard{ID: item.Login, Platform: ID, Source: source, Name: platform.First(item.Name, item.Login), Thumbnail: item.Thumbnail})
					}
				}
				loaded = len(channels)
			} else {
				next, count = batch.Videos.Cursor, len(batch.Videos.Edges)
				for _, edge := range batch.Videos.Edges {
					item := edge.Item
					if item != nil && vodIDPattern.MatchString(item.ID) && item.Owner != nil && loginPattern.MatchString(item.Owner.Login) && !seen[item.ID] {
						seen[item.ID] = true
						videos = append(videos, item.video(source))
					}
				}
				loaded = len(videos)
			}
			if count == 0 || next == "" {
				break
			}
			if next == cursor {
				return platform.SearchPage{}, apierr.RequestFailed().WithCause(errors.New("twitch repeated its search cursor"))
			}
			cursor = next
		}
	}
	// Interleave from the beginning before slicing, so channels cannot consume the first pages and hide
	// VODs, and either kind fills an exhausted other.
	results := make([]platform.Result, 0, min(end, len(channels)+len(videos)))
	for index := 0; len(results) < end && (index < len(channels) || index < len(videos)); index++ {
		if index < len(channels) {
			results = append(results, platform.Result{Channel: &channels[index]})
		}
		if index < len(videos) && len(results) < end {
			results = append(results, platform.Result{Video: &videos[index]})
		}
	}
	result := platform.SearchPage{Results: []platform.Result{}, Page: page}
	if len(results) >= start {
		result.HasMore = len(results) > start-1+platform.SearchPageSize
		result.Results = results[start-1 : min(start-1+platform.SearchPageSize, len(results))]
	}
	p.markSubscriberOnlyResults(ctx, result.Results)
	return result, nil
}
