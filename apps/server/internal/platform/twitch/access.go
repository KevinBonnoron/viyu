package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// A Twitch VOD only a subscriber can watch is not described by any field of its listing, but its
// playback access token is: Twitch lists every quality as restricted for an account that does not
// subscribe. The token is requested for many VODs at once, in a single GraphQL document.
// Twitch rejects a document with more than 15 root field aliases ("exceeds the root field aliases
// limit"), which would silently flag nothing, so the VODs are asked 15 at a time.
const accessBatch = 15

func subscriberQuery(ids []string) string {
	var query strings.Builder
	query.WriteString("query ViyuAccess {")
	for index, id := range ids {
		// IDs are validated digits, never user text.
		fmt.Fprintf(&query, ` v%d: videoPlaybackAccessToken(id: "%s", params: {platform: "web", playerBackend: "mediaplayer", playerType: "site"}) { value }`, index, id)
	}
	query.WriteString(" }")
	return query.String()
}

// subscriberOnlyBatch maps each VOD ID Twitch answered for to whether it is restricted to subscribers.
// A VOD Twitch gave no token for (deleted, private) is absent: its restriction is unknown.
func (p *Platform) subscriberOnlyBatch(ctx context.Context, ids []string) (map[string]bool, error) {
	var response struct {
		Data map[string]*struct {
			Value string `json:"value"`
		} `json:"data"`
	}
	if err := p.graphQL(ctx, map[string]any{"query": subscriberQuery(ids)}, publicClientID, "", &response); err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(ids))
	for index, id := range ids {
		token := response.Data[fmt.Sprintf("v%d", index)]
		if token == nil {
			continue
		}
		var access struct {
			Chansub *struct {
				RestrictedBitrates []string `json:"restricted_bitrates"`
			} `json:"chansub"`
		}
		if json.Unmarshal([]byte(token.Value), &access) != nil || access.Chansub == nil {
			continue
		}
		result[id] = len(access.Chansub.RestrictedBitrates) > 0
	}
	return result, nil
}

// subscriberFlags returns the restriction of every VOD Twitch answered for. A failed lookup returns
// less, never a wrong answer: callers keep what they already know about the rest. Only a rate limit is
// reported, with what was found before it: whoever paces the requests must hear of it.
func (p *Platform) subscriberFlags(ctx context.Context, ids []string) (map[string]bool, error) {
	result := make(map[string]bool, len(ids))
	for start := 0; start < len(ids); start += accessBatch {
		flags, err := p.subscriberOnlyBatch(ctx, ids[start:min(start+accessBatch, len(ids))])
		if errors.Is(err, ytdlp.ErrRateLimited) || ctx.Err() != nil {
			return result, fail(err)
		}
		if err != nil {
			p.log.Debug("Twitch subscriber-only lookup failed", "error", err)
		}
		for id, restricted := range flags {
			result[id] = restricted
		}
	}
	return result, nil
}

// subscriberOnly is subscriberFlags for callers that show what they have whatever happened.
func (p *Platform) subscriberOnly(ctx context.Context, ids []string) map[string]bool {
	flags, _ := p.subscriberFlags(ctx, ids)
	return flags
}

// applySubscriberFlag records a looked-up restriction on a Twitch VOD.
func applySubscriberFlag(video *platform.Video, flags map[string]bool) {
	if restricted, ok := flags[video.ID]; ok && video.Platform == ID {
		video.SubscriberOnly, video.SubscriberChecked = restricted, true
	}
}

// markSubscriberOnly looks up and records the restriction of the VODs in a listing.
func (p *Platform) markSubscriberOnly(ctx context.Context, videos []platform.Video) {
	ids := make([]string, 0, len(videos))
	for _, video := range videos {
		if video.Platform == ID && vodIDPattern.MatchString(video.ID) {
			ids = append(ids, video.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	flags := p.subscriberOnly(ctx, ids)
	for index := range videos {
		applySubscriberFlag(&videos[index], flags)
	}
}

// markSubscriberOnlyResults does the same for the videos among mixed search results.
func (p *Platform) markSubscriberOnlyResults(ctx context.Context, results []platform.Result) {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		if result.Video != nil && result.Video.Platform == ID && vodIDPattern.MatchString(result.Video.ID) {
			ids = append(ids, result.Video.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	flags := p.subscriberOnly(ctx, ids)
	for _, result := range results {
		if result.Video != nil {
			applySubscriberFlag(result.Video, flags)
		}
	}
}
