package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// yt-dlp lists a Twitch channel's VODs but not its avatar, name or followers, nor the VODs' dates,
// durations or view counts; Twitch's public GraphQL API supplies them, in a single request.
const profileQuery = `query ViyuChannel($login: String!, $first: Int!, $withVideos: Boolean!) {
  user(login: $login) {
    login displayName description
    profileImageURL(width: 150)
    followers { totalCount }
    videos(first: $first, sort: TIME) @include(if: $withVideos) {
      edges { node { id title createdAt lengthSeconds viewCount previewThumbnailURL(width: 640, height: 360) } }
    }
  }
}`

// maxVODs is the most VODs one GraphQL page returns.
const maxVODs = 100

// graphQL posts a public GraphQL document (the web client's identity, no account) and decodes the answer.
func (p *Platform) graphQL(ctx context.Context, payload any, clientID, oauth string, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphQLURL, bytes.NewReader(body))
	if err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Client-Id", clientID)
	if oauth != "" {
		request.Header.Set("Authorization", "OAuth "+oauth)
	}
	return p.readJSON(request, target)
}

func (p *Platform) channelProfile(ctx context.Context, login string) (platform.Channel, error) {
	profile, _, err := p.profileWithVideos(ctx, login, 0)
	return profile, err
}

// profileWithVideos returns the channel profile and, when videos > 0, its latest VODs.
func (p *Platform) profileWithVideos(ctx context.Context, login string, videos int) (platform.Channel, []searchVideo, error) {
	videos = min(max(videos, 0), maxVODs)
	var response struct {
		Data *struct {
			User *struct {
				Login           string `json:"login"`
				DisplayName     string `json:"displayName"`
				Description     string `json:"description"`
				ProfileImageURL string `json:"profileImageURL"`
				Followers       *struct {
					TotalCount float64 `json:"totalCount"`
				} `json:"followers"`
				Videos *struct {
					Edges []struct {
						Node *searchVideo `json:"node"`
					} `json:"edges"`
				} `json:"videos"`
			} `json:"user"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	err := p.graphQL(ctx, map[string]any{"query": profileQuery, "variables": map[string]any{"login": login, "first": max(videos, 1), "withVideos": videos > 0}}, publicClientID, "", &response)
	if err != nil {
		return platform.Channel{}, nil, err
	}
	if len(response.Errors) != 0 || response.Data == nil || response.Data.User == nil {
		return platform.Channel{}, nil, apierr.RequestFailed().WithCause(errUnexpectedAnswer)
	}
	user := response.Data.User
	profile := platform.Channel{ID: user.Login, Platform: ID, Name: platform.First(user.DisplayName, user.Login), Thumbnail: user.ProfileImageURL, Description: user.Description}
	if user.Followers != nil {
		profile.SubscriberCount = user.Followers.TotalCount
	}
	var vods []searchVideo
	if user.Videos != nil {
		for _, edge := range user.Videos.Edges {
			if edge.Node != nil {
				vods = append(vods, *edge.Node)
			}
		}
	}
	return profile, vods, nil
}

// completeChannel fills what the VOD listing omits and returns the channel's latest VODs by ID, so
// the caller can fill the listing's entries. A failed lookup leaves the listing as it is.
func (p *Platform) completeChannel(ctx context.Context, info *platform.Channel, videos int) map[string]searchVideo {
	profile, vods, err := p.profileWithVideos(ctx, info.ID, videos)
	if err != nil {
		p.log.Debug("Twitch channel profile unavailable", "channel", info.ID, "error", err)
		return nil
	}
	info.Thumbnail = platform.First(info.Thumbnail, profile.Thumbnail)
	if info.Name == "" || info.Name == info.ID {
		info.Name = profile.Name
	}
	info.Description = platform.First(info.Description, profile.Description)
	if info.SubscriberCount == 0 {
		info.SubscriberCount = profile.SubscriberCount
	}
	byID := make(map[string]searchVideo, len(vods))
	for _, vod := range vods {
		byID[vod.ID] = vod
	}
	return byID
}

// fill completes a VOD the flat listing described only by its title: publication date and time,
// duration, views and artwork. Values the listing already has are kept.
func (item searchVideo) fill(video *platform.Video) {
	video.Published = platform.CarryPublished(video.Published, publication(item.CreatedAt))
	if video.Duration == 0 {
		video.Duration = item.Duration
	}
	if video.ViewCount == 0 {
		video.ViewCount = item.ViewCount
	}
	if video.Title == "" {
		video.Title, video.OriginalTitle = item.Title, item.Title
	}
	if video.Thumbnail == "" && item.Thumbnail != "" {
		video.Thumbnail = item.Thumbnail
		video.Thumbnails = []string{item.Thumbnail}
	}
}
