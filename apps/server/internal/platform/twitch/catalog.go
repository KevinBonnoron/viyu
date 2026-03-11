package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// catalogQuery lists the videos of a channel whatever their kind (past broadcasts, highlights, uploads),
// newest first. Twitch hands out the cursor of the last video of the page; every edge of a page carries it.
const catalogQuery = `query ViyuCatalog($login: String!, $first: Int!, $after: Cursor) {
  user(login: $login) {
    videos(first: $first, after: $after, sort: TIME) {
      edges { cursor node { id title createdAt lengthSeconds viewCount previewThumbnailURL(width: 640, height: 360) } }
      pageInfo { hasNextPage }
    }
  }
}`

// processingThumbnail is the placeholder Twitch serves for a broadcast it is still processing.
const processingThumbnail = "/_404/"

type catalogSyncer struct{ p *Platform }

var _ platform.CatalogSyncer = catalogSyncer{}

// Catalog streams every video of the channel to visit, a page of the public GraphQL API at a time. The
// listing of a connected account is the public one: a VOD is visible to everybody, and what only subscribers
// can watch is flagged, not hidden. Every request takes a slot of the shared runner and honours its backoff,
// like a yt-dlp run does; a page is visited once the slot is released. A listing that cannot be followed to
// its end fails instead of ending early, since the caller drops what a finished listing did not show.
func (s catalogSyncer) Catalog(ctx context.Context, channel platform.Channel, _ string, visit func(platform.Video) error) error {
	if !loginPattern.MatchString(channel.ID) {
		return apierr.InvalidInput("Provide a valid Twitch channel login.")
	}
	cursor := ""
	for {
		page, err := s.p.catalogPage(ctx, channel, cursor)
		if err != nil {
			return err
		}
		for _, video := range page.videos {
			if err := visit(video); err != nil {
				return err
			}
		}
		if !page.more {
			return nil
		}
		if page.cursor == "" || page.cursor == cursor {
			return apierr.RequestFailed().WithCause(errors.New("twitch video pagination does not advance"))
		}
		cursor = page.cursor
	}
}

// catalogPage is one page of a channel's videos: what Twitch answered, the cursor of the next page and
// whether there is one.
type catalogPage struct {
	videos []platform.Video
	cursor string
	more   bool
}

func (p *Platform) catalogPage(ctx context.Context, channel platform.Channel, cursor string) (catalogPage, error) {
	var page catalogPage
	err := p.runner.Guard(ctx, func() error {
		var response struct {
			Data *struct {
				User *struct {
					Videos *struct {
						Edges []struct {
							Cursor string       `json:"cursor"`
							Node   *searchVideo `json:"node"`
						} `json:"edges"`
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"videos"`
				} `json:"user"`
			} `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		variables := map[string]any{"login": channel.ID, "first": maxVODs}
		if cursor != "" {
			variables["after"] = cursor
		}
		if err := p.graphQL(ctx, map[string]any{"query": catalogQuery, "variables": variables}, publicClientID, "", &response); err != nil {
			return err
		}
		if len(response.Errors) != 0 || response.Data == nil || response.Data.User == nil || response.Data.User.Videos == nil {
			return apierr.RequestFailed().WithCause(errUnexpectedAnswer)
		}
		connection := response.Data.User.Videos
		page.more = connection.PageInfo.HasNextPage
		for _, edge := range connection.Edges {
			page.cursor = edge.Cursor
			if edge.Node == nil || !vodIDPattern.MatchString(edge.Node.ID) {
				return apierr.RequestFailed().WithCause(errors.New("invalid Twitch catalog entry"))
			}
			page.videos = append(page.videos, edge.Node.catalogVideo(channel))
		}
		// A lookup that is limited or fails leaves its videos unflagged: the flag is carried over by the
		// store, and a rate limit stops the listing like any other.
		flags, err := p.subscriberFlags(ctx, videoIDs(page.videos))
		for index := range page.videos {
			applySubscriberFlag(&page.videos[index], flags)
		}
		return err
	})
	return page, fail(err)
}

func videoIDs(videos []platform.Video) []string {
	ids := make([]string, len(videos))
	for index, video := range videos {
		ids[index] = video.ID
	}
	return ids
}

// catalogVideo is a video of the channel's listing. A VOD has one title, so it is its own original.
func (item searchVideo) catalogVideo(channel platform.Channel) platform.Video {
	thumbnails := []string{}
	thumbnail := ""
	if item.Thumbnail != "" && !strings.Contains(item.Thumbnail, processingThumbnail) {
		thumbnail = item.Thumbnail
		thumbnails = append(thumbnails, thumbnail)
	}
	return platform.Video{
		ID: item.ID, Platform: ID,
		Title: item.Title, OriginalTitle: item.Title, OriginalTitleVerified: true,
		ChannelID: channel.ID, ChannelName: platform.First(channel.Name, channel.ID), ChannelThumbnail: channel.Thumbnail,
		Thumbnail: thumbnail, Thumbnails: thumbnails, Duration: item.Duration, ViewCount: item.ViewCount,
		Published: publication(item.CreatedAt), Tags: []string{}, Chapters: []platform.Chapter{},
	}
}
