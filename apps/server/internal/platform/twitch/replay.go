package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	// commentsHash is Twitch's persisted query for a VOD's chat replay; when Twitch retires it the endpoint answers 502.
	commentsHash  = "b70a3591ff0f4e0313d126c6a1502d79a1c02baebb288227c582044aa76adf6a"
	replayLimit   = 200
	replayTimeout = 15 * time.Second
)

// replayCursor is the shape of a cursor Twitch hands out; one that does not match is not passed on.
var replayCursor = regexp.MustCompile(`^[A-Za-z0-9+/=_.~-]{1,1000}$`)

// Replay asks Twitch's public GraphQL API for one page of a VOD's chat replay: from the cursor when there
// is one, else from the offset in seconds.
func (c chat) Replay(ctx context.Context, videoID string, offset int, cursor string) (platform.ChatPage, error) {
	if !c.ValidReplayID(videoID) {
		return platform.ChatPage{}, apierr.InvalidInput("Provide a valid Twitch VOD ID.")
	}
	variables := map[string]any{"videoID": videoID}
	if cursor != "" {
		variables["cursor"] = cursor
	} else {
		variables["contentOffsetSeconds"] = offset
	}
	payload := []any{map[string]any{
		"operationName": "VideoCommentsByOffsetOrCursor",
		"variables":     variables,
		"extensions":    map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": commentsHash}},
	}}
	ctx, cancel := context.WithTimeout(ctx, replayTimeout)
	defer cancel()
	var response []struct {
		Data *struct {
			Video *struct {
				Comments *struct {
					Edges []struct {
						Cursor string `json:"cursor"`
						Node   *struct {
							ID                   string  `json:"id"`
							ContentOffsetSeconds float64 `json:"contentOffsetSeconds"`
							Commenter            *struct {
								DisplayName string `json:"displayName"`
							} `json:"commenter"`
							Message *struct {
								UserColor string `json:"userColor"`
								Fragments []struct {
									Text  string `json:"text"`
									Emote *struct {
										EmoteID string `json:"emoteID"`
									} `json:"emote"`
								} `json:"fragments"`
							} `json:"message"`
						} `json:"node"`
					} `json:"edges"`
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"comments"`
			} `json:"video"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := c.p.graphQL(ctx, payload, publicClientID, "", &response); err != nil {
		return platform.ChatPage{}, err
	}
	if len(response) != 1 || len(response[0].Errors) != 0 || response[0].Data == nil || response[0].Data.Video == nil || response[0].Data.Video.Comments == nil {
		return platform.ChatPage{}, apierr.RequestFailed().WithCause(errors.New("twitch answered without the chat replay"))
	}
	comments := response[0].Data.Video.Comments
	page := platform.ChatPage{Messages: make([]platform.ChatMessage, 0, min(len(comments.Edges), replayLimit))}
	last := ""
	for i, edge := range comments.Edges {
		if i == replayLimit {
			break
		}
		last = edge.Cursor
		node := edge.Node
		if node == nil || node.Commenter == nil || node.Commenter.DisplayName == "" || node.Message == nil {
			continue
		}
		at := node.ContentOffsetSeconds
		message := platform.ChatMessage{ID: platform.First(node.ID, edge.Cursor), At: &at, User: node.Commenter.DisplayName}
		if colorPattern.MatchString(node.Message.UserColor) {
			message.Color = node.Message.UserColor
		}
		remaining := textLimit
		for _, fragment := range node.Message.Fragments {
			text := []rune(fragment.Text)
			if len(text) > remaining {
				text = text[:remaining]
			}
			remaining -= len(text)
			emote := ""
			if fragment.Emote != nil && emoteIDPattern.MatchString(fragment.Emote.EmoteID) {
				emote = fragment.Emote.EmoteID
			}
			if len(text) == 0 {
				continue
			}
			message.Fragments = append(message.Fragments, platform.ChatFragment{Text: string(text), Emote: emote})
		}
		if len(message.Fragments) != 0 {
			page.Messages = append(page.Messages, message)
		}
	}
	sort.SliceStable(page.Messages, func(i, j int) bool { return *page.Messages[i].At < *page.Messages[j].At })
	if comments.PageInfo.HasNextPage && replayCursor.MatchString(last) {
		page.Next = last
	}
	return page, nil
}
