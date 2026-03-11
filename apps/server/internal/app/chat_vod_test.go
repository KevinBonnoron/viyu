package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// vodUpstream is the Twitch GraphQL endpoint that answers chat replay requests; it keeps what it was asked.
type vodUpstream struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (u *vodUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *vodUpstream) last() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

const vodFirstPage = `[{"data":{"video":{"comments":{"edges":[
  {"cursor":"cur1","node":{"id":"n1","contentOffsetSeconds":12.5,"commenter":{"displayName":"Late"},"message":{"userColor":"#FF0000","fragments":[{"text":"second","emote":null}]}}},
  {"cursor":"cur2","node":{"id":"n2","contentOffsetSeconds":10,"commenter":{"displayName":"Early"},"message":{"userColor":null,"fragments":[{"text":"Kappa","emote":{"emoteID":"25"}},{"text":" hi 😀","emote":null}]}}},
  {"cursor":"cur3","node":{"id":"","contentOffsetSeconds":11,"commenter":null,"message":{"userColor":"","fragments":[{"text":"deleted user","emote":null}]}}}
],"pageInfo":{"hasNextPage":true}}}}}]`

const vodLastPage = `[{"data":{"video":{"comments":{"edges":[
  {"cursor":"cur9","node":{"id":"n9","contentOffsetSeconds":70,"commenter":{"displayName":"Last"},"message":{"userColor":"#00FF00","fragments":[{"text":"bye","emote":null}]}}}
],"pageInfo":{"hasNextPage":false}}}}}]`

// withVODUpstream points the Twitch API of the application at a fake that serves the replay of every VOD (and
// fails for the ids 500 and 903, answers an empty chat for 904) and returns what it was asked.
func withVODUpstream(t *testing.T) (func(*Options), *vodUpstream) {
	t.Helper()
	upstream := &vodUpstream{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		var body []map[string]any
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || json.Unmarshal(payload, &body) != nil || len(body) != 1 {
			t.Errorf("unexpected upstream request: %s %v %s", r.Method, r.Header, payload)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, body[0])
		upstream.mu.Unlock()
		variables, _ := body[0]["variables"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		switch id := variables["videoID"]; {
		case id == "500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `secret upstream detail`)
		case id == "903":
			_, _ = io.WriteString(w, `not json`)
		case id == "904":
			_, _ = io.WriteString(w, `[{"data":{"video":{"comments":{"edges":[],"pageInfo":{"hasNextPage":false}}}}}]`)
		case variables["cursor"] == "cur3":
			_, _ = io.WriteString(w, vodLastPage)
		default:
			_, _ = io.WriteString(w, vodFirstPage)
		}
	}))
	t.Cleanup(server.Close)
	return func(o *Options) {
		o.Twitch.HTTP = server.Client()
		o.Twitch.GraphQLURL = server.URL
	}, upstream
}

// vodPath stores a Twitch VOD and returns the chat replay path addressed by its record id.
func vodPath(t *testing.T, app core.App, nativeID string) string {
	t.Helper()
	return "/api/chat/vod/" + storedVideo(t, app, platform.Video{ID: nativeID, Platform: "twitch", ChannelID: "vodstreamer", ChannelName: "VOD streamer"})
}

// vodReplay asks the replay of a path and decodes the page; every answer of the route must be uncacheable.
func vodReplay(t *testing.T, handler http.Handler, path, token string) (int, platform.ChatPage, string) {
	t.Helper()
	res := request(t, handler, path, token)
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%s must not be cacheable: %v", path, res.Header())
	}
	var page platform.ChatPage
	_ = json.Unmarshal(res.Body.Bytes(), &page)
	return res.Code, page, res.Body.String()
}

func TestChatVODOffsetCursorAndDefaultOffset(t *testing.T) {
	t.Parallel()
	option, upstream := withVODUpstream(t)
	app, handler := newApp(t, option)
	vod := vodPath(t, app, "123456")

	code, page, raw := vodReplay(t, handler, vod+"?offset=600", "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, raw)
	}
	variables := upstream.last()["variables"].(map[string]any)
	if variables["videoID"] != "123456" || variables["contentOffsetSeconds"] != float64(600) || variables["cursor"] != nil {
		t.Fatalf("upstream variables: %v", variables)
	}
	var order []string
	for _, m := range page.Messages {
		order = append(order, m.ID)
	}
	if strings.Join(order, ",") != "n2,n1" || page.Next != "cur3" {
		t.Fatalf("messages %v next %q: %s", order, page.Next, raw)
	}
	if early := page.Messages[0]; early.User != "Early" || *early.At != 10 || len(early.Fragments) != 2 || early.Fragments[0] != (platform.ChatFragment{Text: "Kappa", Emote: "25"}) {
		t.Fatalf("early message: %+v", early)
	}
	if strings.Contains(raw, `"emote":null`) || strings.Contains(raw, `"emote":""`) || strings.Contains(raw, "123456") {
		t.Fatalf("a text fragment has no emote key and the native id stays on the server: %s", raw)
	}

	// A missing offset means the start of the video.
	if code, _, raw = vodReplay(t, handler, vod, ""); code != http.StatusOK {
		t.Fatalf("status %d: %s", code, raw)
	}
	if variables = upstream.last()["variables"].(map[string]any); variables["contentOffsetSeconds"] != float64(0) {
		t.Fatalf("missing offset means 0: %v", variables)
	}

	code, page, raw = vodReplay(t, handler, vod+"?cursor=cur3", "")
	if code != http.StatusOK || len(page.Messages) != 1 || page.Messages[0].ID != "n9" || strings.Contains(raw, `"next"`) {
		t.Fatalf("last page: %d %s", code, raw)
	}
	variables = upstream.last()["variables"].(map[string]any)
	if variables["cursor"] != "cur3" || variables["contentOffsetSeconds"] != nil || variables["videoID"] != "123456" {
		t.Fatalf("cursor variables: %v", variables)
	}
	// A cursor wins over an offset.
	if code, _, _ = vodReplay(t, handler, vod+"?cursor=cur3&offset=9", ""); code != http.StatusOK || upstream.last()["variables"].(map[string]any)["cursor"] != "cur3" {
		t.Fatal("cursor must win over offset")
	}
}

func TestChatVODEmptyChat(t *testing.T) {
	t.Parallel()
	option, _ := withVODUpstream(t)
	app, handler := newApp(t, option)
	code, page, raw := vodReplay(t, handler, vodPath(t, app, "904")+"?offset=0", "")
	if code != http.StatusOK || page.Messages == nil || len(page.Messages) != 0 || page.Next != "" || strings.TrimSpace(raw) != `{"messages":[]}` {
		t.Fatalf("empty chat: %d %s", code, raw)
	}
}

func TestChatVODUpstreamFailuresAreBadGatewaysWithoutDetails(t *testing.T) {
	t.Parallel()
	option, upstream := withVODUpstream(t)
	app, handler := newApp(t, option)
	// 500: HTTP error, 903: not JSON.
	for _, id := range []string{"500", "903"} {
		code, _, raw := vodReplay(t, handler, vodPath(t, app, id)+"?offset=1", "")
		var body map[string]string
		if code != http.StatusBadGateway || json.Unmarshal([]byte(raw), &body) != nil || body["code"] != "SOURCE_REQUEST_FAILED" || body["message"] == "" || strings.Contains(raw, "secret") || strings.Contains(raw, `"error"`) {
			t.Errorf("%s: %d %s", id, code, raw)
		}
	}
	if upstream.count() != 2 {
		t.Fatalf("each failure reached Twitch once: %d", upstream.count())
	}
	// Failures are not cached.
	vodReplay(t, handler, vodPath(t, app, "500")+"?offset=1", "")
	if upstream.count() != 3 {
		t.Fatalf("a failure must not be cached: %d", upstream.count())
	}
}

func TestChatVODUnreachableTwitchIsABadGateway(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t) // the Twitch API of the default test application is a closed port
	code, _, raw := vodReplay(t, handler, vodPath(t, app, "123456")+"?offset=1", "")
	if code != http.StatusBadGateway || !strings.Contains(raw, `"code":"SOURCE_REQUEST_FAILED"`) || strings.Contains(raw, "127.0.0.1") {
		t.Fatalf("unreachable upstream: %d %s", code, raw)
	}
}

func TestChatVODRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	option, upstream := withVODUpstream(t)
	app, handler := newApp(t, option)
	vod := vodPath(t, app, "123")
	for _, path := range []string{
		vodPath(t, app, "abc"),
		vodPath(t, app, "0123"),
		vodPath(t, app, "12345678901234567890123"),
		vod + "?offset=-1",
		vod + "?offset=abc",
		vod + "?offset=1.5",
		vod + "?offset=",
		vod + "?offset=99999999999",
		vod + "?cursor=a%20b",
		vod + "?cursor=%22%7D",
		vod + "?cursor=",
		vod + "?cursor=" + strings.Repeat("a", 1001),
	} {
		code, _, raw := vodReplay(t, handler, path, "")
		if code != http.StatusBadRequest || !strings.Contains(raw, `"code":"INVALID_REQUEST"`) || strings.Contains(raw, `"error"`) {
			t.Errorf("%s: %d %s", path, code, raw)
		}
	}
	if upstream.count() != 0 {
		t.Fatalf("invalid input must not reach Twitch (%d requests)", upstream.count())
	}
}

func TestChatVODCachesIdenticalRequestsBriefly(t *testing.T) {
	t.Parallel()
	option, upstream := withVODUpstream(t)
	app, handler := newApp(t, option)
	vod := vodPath(t, app, "123456")
	for range 3 {
		if code, _, raw := vodReplay(t, handler, vod+"?offset=600", ""); code != http.StatusOK {
			t.Fatalf("%d %s", code, raw)
		}
	}
	if upstream.count() != 1 {
		t.Fatalf("identical requests must be cached: %d upstream calls", upstream.count())
	}
	vodReplay(t, handler, vod+"?offset=601", "")
	vodReplay(t, handler, vod+"?cursor=cur3", "")
	vodReplay(t, handler, vodPath(t, app, "654321")+"?offset=600", "")
	if upstream.count() != 4 {
		t.Fatalf("different offset, cursor and VOD are different entries: %d", upstream.count())
	}
}

// The replay is addressed by record id: only a stored video of a platform with a chat that the account sees
// and uses has one.
func TestChatVODAddressingAuthorizationAndActivation(t *testing.T) {
	t.Parallel()
	option, upstream := withVODUpstream(t)
	app, handler := newApp(t, option)
	vod := vodPath(t, app, "123456")
	for name, path := range map[string]string{
		"unknown record":                     "/api/chat/vod/nosuchrecord0001",
		"a video of a platform with no chat": "/api/chat/vod/" + storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"}),
	} {
		if code, _, raw := vodReplay(t, handler, path+"?offset=1", ""); code != http.StatusNotFound || !strings.Contains(raw, `"code":"NOT_FOUND"`) {
			t.Errorf("%s: %d %s", name, code, raw)
		}
	}

	// A VOD another account stored privately is not there for anybody else.
	owner := testUser(t, app, "vod-owner@example.test")
	secret := platform.Video{ID: "777", Platform: "twitch", ChannelID: "private", ChannelName: "Private", Title: "Private"}
	if err := newStore(app).StoreVideo(&secret, owner.Id); err != nil {
		t.Fatal(err)
	}
	if code, _, raw := vodReplay(t, handler, "/api/chat/vod/"+secret.RecordID+"?offset=1", ""); code != http.StatusNotFound {
		t.Fatalf("a private VOD was replayed to another account: %d %s", code, raw)
	}
	if code, _, raw := vodReplay(t, handler, "/api/chat/vod/"+secret.RecordID+"?offset=1", tokenOf(t, owner)); code != http.StatusOK {
		t.Fatalf("the owner cannot replay its VOD: %d %s", code, raw)
	}

	if res := anonymousRequest(t, handler, vod+"?offset=1"); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous replay: %d %s", res.Code, res.Body)
	}

	before := upstream.count()
	user := testUser(t, app, "vod-off@example.test")
	token := tokenOf(t, user)
	if res := send(t, handler, http.MethodPut, "/api/sources/twitch/activation", token, `{"enabled":false}`); res.Code != http.StatusOK {
		t.Fatalf("turn Twitch off: %d %s", res.Code, res.Body)
	}
	if code, _, raw := vodReplay(t, handler, vod+"?offset=1", token); code != http.StatusConflict || !strings.Contains(raw, `"code":"SOURCE_DISABLED"`) {
		t.Fatalf("replay of a source the account turned off: %d %s", code, raw)
	}
	if upstream.count() != before {
		t.Fatalf("a refused replay must not reach Twitch: %d -> %d", before, upstream.count())
	}
}
