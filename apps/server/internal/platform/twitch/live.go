package twitch

import (
	"context"
	"encoding/json"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const liveQuery = `query ViyuLive($login: String!) {
  user(login: $login) { stream { id title viewersCount game { name displayName } } }
}`

// LiveStream reports whether a channel is live. An unknown channel is simply not live.
func (p *Platform) LiveStream(ctx context.Context, login string) (platform.LiveStream, error) {
	var response struct {
		Data *struct {
			User *struct {
				Stream *struct {
					Title        string  `json:"title"`
					ViewersCount float64 `json:"viewersCount"`
					Game         *struct {
						Name        string `json:"name"`
						DisplayName string `json:"displayName"`
					} `json:"game"`
				} `json:"stream"`
			} `json:"user"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := p.graphQL(ctx, map[string]any{"query": liveQuery, "variables": map[string]any{"login": login}}, publicClientID, "", &response); err != nil {
		return platform.LiveStream{}, err
	}
	if len(response.Errors) != 0 || response.Data == nil {
		return platform.LiveStream{}, apierr.RequestFailed().WithCause(errUnexpectedAnswer)
	}
	if response.Data.User == nil || response.Data.User.Stream == nil {
		return platform.LiveStream{}, nil
	}
	stream := response.Data.User.Stream
	live := platform.LiveStream{Live: true, VideoID: login, Title: stream.Title, Viewers: stream.ViewersCount}
	if stream.Game != nil {
		live.Category = platform.First(stream.Game.DisplayName, stream.Game.Name)
	}
	return live, nil
}
