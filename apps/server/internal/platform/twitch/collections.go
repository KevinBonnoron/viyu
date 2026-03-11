package twitch

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// collectionsQuery reads every collection of a channel with its videos, in the order the streamer gave
// them. Twitch answers without a login, but its collections have no cursors: nothing beyond the first
// hundred collections, nor the first hundred videos of one, can be reached, and a bigger page is silently cut
// down (the answer says so through hasNextPage, which is how an unfinished collection is recognized).
const collectionsQuery = `query ViyuCollections($login: String!) {
  user(login: $login) {
    collections(first: 100) {
      edges { node { title items(first: 100) { edges { node { ... on Video { id } } } pageInfo { hasNextPage } } } }
      pageInfo { hasNextPage }
    }
  }
}`

// Collections lists the channel's collections. A collection whose videos could not all be read (more than a
// page of them) is left out, and so is one without a title or without a video. Like a catalog page, the
// request takes a slot of the shared runner and honours its backoff.
func (s catalogSyncer) Collections(ctx context.Context, channel platform.Channel) ([]platform.Collection, error) {
	if !loginPattern.MatchString(channel.ID) {
		return nil, apierr.InvalidInput("Provide a valid Twitch channel login.")
	}
	var collections []platform.Collection
	err := s.p.runner.Guard(ctx, func() error {
		var response struct {
			Data *struct {
				User *struct {
					Collections *struct {
						Edges []struct {
							Node *struct {
								Title string `json:"title"`
								Items *struct {
									Edges []struct {
										Node *struct {
											ID string `json:"id"`
										} `json:"node"`
									} `json:"edges"`
									PageInfo struct {
										HasNextPage bool `json:"hasNextPage"`
									} `json:"pageInfo"`
								} `json:"items"`
							} `json:"node"`
						} `json:"edges"`
					} `json:"collections"`
				} `json:"user"`
			} `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		payload := map[string]any{"query": collectionsQuery, "variables": map[string]any{"login": channel.ID}}
		if err := s.p.graphQL(ctx, payload, publicClientID, "", &response); err != nil {
			return err
		}
		if len(response.Errors) != 0 || response.Data == nil || response.Data.User == nil || response.Data.User.Collections == nil {
			return apierr.RequestFailed().WithCause(errUnexpectedAnswer)
		}
		for _, edge := range response.Data.User.Collections.Edges {
			node := edge.Node
			if node == nil || node.Items == nil || node.Items.PageInfo.HasNextPage || strings.TrimSpace(node.Title) == "" {
				continue
			}
			collection := platform.Collection{Title: strings.TrimSpace(node.Title), VideoIDs: []string{}}
			for _, item := range node.Items.Edges {
				// What is not a video (an item of another kind) comes without an id.
				if item.Node != nil && vodIDPattern.MatchString(item.Node.ID) {
					collection.VideoIDs = append(collection.VideoIDs, item.Node.ID)
				}
			}
			if len(collection.VideoIDs) > 0 {
				collections = append(collections, collection)
			}
		}
		return nil
	})
	return collections, fail(err)
}
