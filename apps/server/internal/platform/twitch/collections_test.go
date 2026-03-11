package twitch

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func collectionsOf(t *testing.T, p *Platform, login string) ([]platform.Collection, error) {
	t.Helper()
	return p.CatalogSync().Collections(context.Background(), platform.Channel{ID: login})
}

// A channel's collections come with their videos in the streamer's order. What cannot be read whole (a
// page of videos was cut), has no title, no video, or holds items that are not videos is left out or
// reduced, never invented.
func TestCollectionsKeepTheStreamersOrderAndLeaveOutWhatIsIncomplete(t *testing.T) {
	t.Parallel()
	var queries int
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(payload), "ViyuCollections") || r.Header.Get("Client-Id") != publicClientID || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request: %s", payload)
		}
		queries++
		_, _ = io.WriteString(w, `{"data":{"user":{"collections":{"pageInfo":{"hasNextPage":false},"edges":[
			{"node":{"title":"  Zelda playthrough ","items":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"id":"30"}},{"node":{"id":"10"}},{"node":{}},{"node":{"id":"20"}},{"node":null}]}}},
			{"node":{"title":"Too long","items":{"pageInfo":{"hasNextPage":true},"edges":[{"node":{"id":"1"}},{"node":{"id":"2"}}]}}},
			{"node":{"title":"","items":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"id":"3"}}]}}},
			{"node":{"title":"Empty","items":{"pageInfo":{"hasNextPage":false},"edges":[]}}},
			{"node":null}
		]}}}}`)
	}, Options{})
	got, err := collectionsOf(t, p, "streamer")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Zelda playthrough" || !slices.Equal(got[0].VideoIDs, []string{"30", "10", "20"}) || queries != 1 {
		t.Fatalf("collections=%+v after %d queries", got, queries)
	}
}

func TestChannelWithoutCollectionsHasNone(t *testing.T) {
	t.Parallel()
	p := graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"user":{"collections":{"edges":[],"pageInfo":{"hasNextPage":false}}}}}`)
	}, Options{})
	if got, err := collectionsOf(t, p, "streamer"); err != nil || len(got) != 0 {
		t.Fatalf("collections=%+v err=%v", got, err)
	}
}

func TestCollectionsFailLikeAnyOtherTwitchRequest(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"unknown channel": `{"data":{"user":null}}`,
		"graphql errors":  `{"errors":[{"message":"service error"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }, Options{})
			_, err := collectionsOf(t, p, "streamer")
			assertCode(t, err, apierr.CodeRequestFailed)
		})
	}
	limited := graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, Options{})
	_, err := collectionsOf(t, limited, "streamer")
	assertCode(t, err, apierr.CodeUpstreamRateLimit)
	_, err = collectionsOf(t, newPlatform(t, Options{}), "not a login")
	assertCode(t, err, apierr.CodeInvalidSourceArg)
}
