package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// connected is the session of an account whose auth-token is "search-secret".
func connected(t *testing.T, p *Platform) platform.Session {
	t.Helper()
	cookies, err := p.Connector().NormalizeCookies(cookiesFor("search-secret", 0))
	if err != nil {
		t.Fatal(err)
	}
	return platform.Session{UserID: "user", Cookies: cookies}
}

func searchPlatform(t *testing.T, channelCount, videoCount int) *Platform {
	t.Helper()
	return graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/validate" {
			_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "web-client", "user_id": "user-1"})
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "OAuth search-secret" || r.Header.Get("Client-Id") != "web-client" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var input struct {
			Query     string `json:"query"`
			Variables struct {
				Options struct {
					Targets []struct {
						Index  string `json:"index"`
						Cursor string `json:"cursor"`
						Limit  int    `json:"limit"`
					} `json:"targets"`
				} `json:"options"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("invalid Twitch request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(input.Query, "ViyuAccess") {
			_, _ = io.WriteString(w, `{"data":{}}`)
			return
		}
		if len(input.Variables.Options.Targets) != 1 {
			t.Errorf("invalid Twitch search request: %+v", input)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		target := input.Variables.Options.Targets[0]
		offset := 0
		if target.Cursor != "" {
			var err error
			offset, err = strconv.Atoi(target.Cursor)
			if err != nil {
				t.Errorf("invalid cursor: %q", target.Cursor)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		total, key := videoCount, "videos"
		if target.Index == "CHANNEL" {
			total, key = channelCount, "channels"
		}
		end := min(total, offset+min(7, target.Limit))
		edges := []any{}
		for index := offset; index < end; index++ {
			var item any
			if target.Index == "CHANNEL" {
				item = map[string]any{"login": fmt.Sprintf("channel%02d", index), "displayName": fmt.Sprintf("Channel %d", index)}
			} else {
				item = map[string]any{
					"id": strconv.Itoa(1000 + index), "title": fmt.Sprintf("VOD %d", index), "createdAt": "2026-11-06T12:30:00Z",
					"lengthSeconds": 120, "viewCount": 42, "previewThumbnailURL": "https://static.example/vod.jpg",
					"owner": map[string]string{"login": "creator", "displayName": "Creator", "profileImageURL": "https://static.example/avatar.jpg"},
				}
			}
			edges = append(edges, map[string]any{"item": item})
		}
		cursor := ""
		if end < total {
			cursor = strconv.Itoa(end)
		}
		groups := map[string]any{
			"channels": map[string]any{"cursor": "", "edges": []any{}},
			"videos":   map[string]any{"cursor": "", "edges": []any{}},
		}
		groups[key] = map[string]any{"cursor": cursor, "edges": edges}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"searchFor": groups}})
	}, Options{})
}

func TestSearchReturnsVODsWithoutMatchingChannels(t *testing.T) {
	t.Parallel()
	p := searchPlatform(t, 0, 3)
	page, err := p.Search(context.Background(), connected(t, p), "video title", 1)
	if err != nil || page.HasMore || len(page.Results) != 3 {
		t.Fatalf("VOD search without channels: %+v %v", page, err)
	}
	video := page.Results[0].Video
	if video == nil || video.ID != "1000" || video.Platform != ID || video.Source != ID || video.Title != "VOD 0" || video.ChannelID != "creator" || video.ChannelThumbnail != "https://static.example/avatar.jpg" || video.Duration != 120 || video.ViewCount != 42 || video.Published != "2026-11-06T12:30:00Z" || !video.OriginalTitleVerified {
		t.Fatalf("incorrect searchable Twitch VOD: %+v", page.Results[0])
	}
}

func TestPublicSearchNeedsNoCredentialsOrIdentityValidation(t *testing.T) {
	t.Parallel()
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gql" {
			t.Errorf("public search attempted identity validation: %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Client-Id") != publicClientID {
			t.Error("public search sent account credentials or used the wrong client")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"edges":[{"item":{"login":"xqc","displayName":"xQc"}}]},"videos":{"edges":[{"item":{"id":"123","title":"Public VOD","owner":{"login":"xqc","displayName":"xQc"}}}]}}}}`))
	}, Options{})
	page, err := p.Search(context.Background(), platform.Session{}, "xqc", 1)
	if err != nil || len(page.Results) != 2 {
		t.Fatalf("public Twitch search failed: %+v %v", page, err)
	}
	channel, video := page.Results[0].Channel, page.Results[1].Video
	if channel == nil || channel.ID != "xqc" || channel.Source != "" || video == nil || video.ID != "123" || video.Source != "" {
		t.Fatalf("public results require a private source session: %+v", page.Results)
	}
}

func TestShortLoginsRemainSearchableAndPlayable(t *testing.T) {
	t.Parallel()
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/validate" {
			_, _ = w.Write([]byte(`{"client_id":"web-client","user_id":"user-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"edges":[{"item":{"login":"xqc","displayName":"xQc"}}]},"videos":{"edges":[{"item":{"id":"123","title":"A VOD","owner":{"login":"xqc","displayName":"xQc"}}}]}}}}`))
	}, Options{})
	page, err := p.Search(context.Background(), connected(t, p), "xqc", 1)
	if err != nil || len(page.Results) != 2 {
		t.Fatalf("short Twitch login disappeared from search: %+v %v", page, err)
	}
	channel, video := page.Results[0].Channel, page.Results[1].Video
	if channel == nil || channel.ID != "xqc" || video == nil || video.ChannelID != "xqc" {
		t.Fatalf("short Twitch login was lost: %+v", page.Results)
	}
	if address := p.OriginalURL(channel.ID); address != "https://www.twitch.tv/xqc" {
		t.Fatalf("searchable short Twitch login cannot play: %s", address)
	}
}

func TestSearchInterleavesAndPaginatesBothKinds(t *testing.T) {
	t.Parallel()
	p := searchPlatform(t, 8, 36)
	session := connected(t, p)
	identities := []string{}
	for pageNumber := 1; pageNumber <= 3; pageNumber++ {
		page, err := p.Search(context.Background(), session, "query", pageNumber)
		if err != nil || page.Page != pageNumber || page.HasMore != (pageNumber < 3) {
			t.Fatalf("search page %d: %+v %v", pageNumber, page, err)
		}
		for _, item := range page.Results {
			switch {
			case item.Channel != nil:
				identities = append(identities, "channel:"+item.Channel.ID)
			case item.Video != nil:
				identities = append(identities, "video:"+item.Video.ID)
			default:
				t.Fatalf("unexpected search result: %+v", item)
			}
		}
	}
	expected := []string{}
	for index := range 36 {
		if index < 8 {
			expected = append(expected, fmt.Sprintf("channel:channel%02d", index))
		}
		expected = append(expected, "video:"+strconv.Itoa(1000+index))
	}
	if fmt.Sprint(identities) != fmt.Sprint(expected) {
		t.Fatalf("search skipped, duplicated or hid VODs across pages:\ngot %v\nwant %v", identities, expected)
	}
}

func TestSearchFlagsSubscriberOnlyVODsOfTheShownPage(t *testing.T) {
	t.Parallel()
	var access atomic.Int32
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(readBody(t, r), &body)
		if strings.Contains(body.Query, "ViyuAccess") {
			access.Add(1)
			_, _ = io.WriteString(w, `{"data":{"v0":{"value":"{\"chansub\":{\"restricted_bitrates\":[\"chunked\"]}}"},"v1":{"value":"{\"chansub\":{\"restricted_bitrates\":[]}}"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"searchFor":{"channels":{"edges":[]},"videos":{"edges":[{"item":{"id":"123","title":"Members","owner":{"login":"xqc"}}},{"item":{"id":"124","title":"Public","owner":{"login":"xqc"}}}]}}}}`)
	}, Options{})
	page, err := p.Search(context.Background(), platform.Session{}, "xqc", 1)
	if err != nil || len(page.Results) != 2 || access.Load() != 1 {
		t.Fatalf("%+v %v (%d access requests)", page, err, access.Load())
	}
	members, public := page.Results[0].Video, page.Results[1].Video
	if !members.SubscriberOnly || !members.SubscriberChecked || public.SubscriberOnly || !public.SubscriberChecked {
		t.Fatalf("members %+v public %+v", members, public)
	}
}

func TestSearchRejectsGraphQLErrorsInsteadOfEmptyResults(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"errors":[{"message":"upstream unavailable"}]}`,
		`{"data":{"searchFor":null}}`,
		`{"data":{"searchFor":{"channels":{"edges":[]}}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			p := graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }, Options{})
			_, err := p.searchBatch(context.Background(), identity{ClientID: "web-client"}, "", "query", "VOD", "", 20)
			assertCode(t, err, apierr.CodeRequestFailed)
			_, err = p.Search(context.Background(), platform.Session{}, "query", 1)
			assertCode(t, err, apierr.CodeRequestFailed)
		})
	}
}

func TestSearchRefusesARepeatedCursorAndBadPages(t *testing.T) {
	t.Parallel()
	p := graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"searchFor":{"channels":{"cursor":"same","edges":[{"item":{"login":"a"}}]},"videos":{"cursor":"","edges":[]}}}}`)
	}, Options{})
	_, err := p.Search(context.Background(), platform.Session{}, "query", 1)
	assertCode(t, err, apierr.CodeRequestFailed)
	_, err = p.Search(context.Background(), platform.Session{}, "query", 0)
	assertCode(t, err, apierr.CodeInvalidSourceArg)
}
