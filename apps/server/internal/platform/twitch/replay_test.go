package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

type replayUpstream struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (u *replayUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *replayUpstream) last() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

const replayFirstPage = `[{"data":{"video":{"comments":{"edges":[
  {"cursor":"cur1","node":{"id":"n1","contentOffsetSeconds":12.5,"commenter":{"displayName":"Late"},"message":{"userColor":"#FF0000","fragments":[{"text":"second","emote":null}]}}},
  {"cursor":"cur2","node":{"id":"n2","contentOffsetSeconds":10,"commenter":{"displayName":"Early"},"message":{"userColor":null,"fragments":[{"text":"Kappa","emote":{"emoteID":"25"}},{"text":" hi 😀","emote":null}]}}},
  {"cursor":"cur3","node":{"id":"","contentOffsetSeconds":11,"commenter":null,"message":{"userColor":"","fragments":[{"text":"deleted user","emote":null}]}}},
  {"cursor":"cur4","node":{"contentOffsetSeconds":13,"commenter":{"displayName":"NoID"},"message":{"userColor":"red","fragments":[{"text":"","emote":null}]}}},
  {"cursor":"cur5","node":{"contentOffsetSeconds":14,"commenter":{"displayName":"NoColour"},"message":{"userColor":"bad","fragments":[{"text":"x","emote":{"emoteID":"bad id"}}]}}}
],"pageInfo":{"hasNextPage":true}}}}}]`

const replayLastPage = `[{"data":{"video":{"comments":{"edges":[
  {"cursor":"cur9","node":{"id":"n9","contentOffsetSeconds":70,"commenter":{"displayName":"Last"},"message":{"userColor":"#00FF00","fragments":[{"text":"bye","emote":null}]}}}
],"pageInfo":{"hasNextPage":false}}}}}]`

func replayPlatform(t *testing.T) (*Platform, *replayUpstream) {
	t.Helper()
	upstream := &replayUpstream{}
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		var body []map[string]any
		if r.Method != http.MethodPost || r.Header.Get("Client-Id") != publicClientID || r.Header.Get("Authorization") != "" || json.Unmarshal(payload, &body) != nil || len(body) != 1 {
			t.Errorf("unexpected upstream request: %s %v %s", r.Method, r.Header, payload)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, body[0])
		upstream.mu.Unlock()
		variables, _ := body[0]["variables"].(map[string]any)
		extensions, _ := json.Marshal(body[0]["extensions"])
		if body[0]["operationName"] != "VideoCommentsByOffsetOrCursor" || !strings.Contains(string(extensions), commentsHash) {
			t.Errorf("wrong operation: %s", payload)
		}
		switch id := variables["videoID"]; {
		case id == "500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `secret upstream detail`)
		case id == "403":
			w.WriteHeader(http.StatusForbidden)
		case id == "404":
			_, _ = io.WriteString(w, `[{"errors":[{"message":"PersistedQueryNotFound secret"}]}]`)
		case id == "901":
			_, _ = io.WriteString(w, `[{"data":{"video":null}}]`)
		case id == "902":
			_, _ = io.WriteString(w, `[{"data":{"video":{"comments":null}}}]`)
		case id == "903":
			_, _ = io.WriteString(w, `not json`)
		case id == "904":
			_, _ = io.WriteString(w, `[{"data":{"video":{"comments":{"edges":[],"pageInfo":{"hasNextPage":false}}}}}]`)
		case id == "905":
			_, _ = io.WriteString(w, `[{"data":{"video":{"comments":{"edges":[{"cursor":"not a cursor","node":{"id":"x","contentOffsetSeconds":1,"commenter":{"displayName":"A"},"message":{"fragments":[{"text":"hi"}]}}}],"pageInfo":{"hasNextPage":true}}}}}]`)
		case id == "906":
			_, _ = io.WriteString(w, `[]`)
		case variables["cursor"] == "cur5":
			_, _ = io.WriteString(w, replayLastPage)
		default:
			_, _ = io.WriteString(w, replayFirstPage)
		}
	}, Options{})
	return p, upstream
}

func TestReplayOffsetSortsAndSanitizes(t *testing.T) {
	t.Parallel()
	p, upstream := replayPlatform(t)
	page, err := p.Chat().Replay(context.Background(), "123456", 600, "")
	if err != nil {
		t.Fatal(err)
	}
	variables := upstream.last()["variables"].(map[string]any)
	if variables["videoID"] != "123456" || variables["contentOffsetSeconds"] != float64(600) || variables["cursor"] != nil {
		t.Fatalf("upstream variables: %v", variables)
	}
	// Sorted by at; the deleted-user and empty-text messages are dropped; bad colour/emote ids are not forwarded.
	var order []string
	for _, m := range page.Messages {
		order = append(order, m.ID)
	}
	if strings.Join(order, ",") != "n2,n1,cur5" || page.Next != "cur5" {
		t.Fatalf("messages %v next %q", order, page.Next)
	}
	early := page.Messages[0]
	if early.User != "Early" || early.Color != "" || *early.At != 10 || len(early.Fragments) != 2 || early.Fragments[0].Text != "Kappa" || early.Fragments[0].Emote != "25" || early.Fragments[1].Text != " hi 😀" || early.Fragments[1].Emote != "" {
		t.Fatalf("early message: %+v", early)
	}
	if late := page.Messages[1]; late.Color != "#FF0000" || *late.At != 12.5 || late.Fragments[0].Emote != "" {
		t.Fatalf("late message: %+v", late)
	}
	if last := page.Messages[2]; last.Color != "" || last.Fragments[0].Emote != "" {
		t.Fatalf("unsafe values forwarded: %+v", last)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), `"emote":null`) || strings.Contains(string(raw), `"emote":""`) {
		t.Fatalf("a text fragment has no emote key: %s", raw)
	}
}

func TestReplayCursorAndDefaultOffset(t *testing.T) {
	t.Parallel()
	p, upstream := replayPlatform(t)
	chat := p.Chat()
	if page, err := chat.Replay(context.Background(), "123456", 0, ""); err != nil || len(page.Messages) == 0 {
		t.Fatalf("%+v %v", page, err)
	}
	if variables := upstream.last()["variables"].(map[string]any); variables["contentOffsetSeconds"] != float64(0) {
		t.Fatalf("offset 0 is sent as 0: %v", variables)
	}
	page, err := chat.Replay(context.Background(), "123456", 0, "cur5")
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "n9" {
		t.Fatalf("cursor page: %+v %v", page, err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), `"next"`) {
		t.Fatalf("no next page: next must be omitted: %s", raw)
	}
	variables := upstream.last()["variables"].(map[string]any)
	if variables["cursor"] != "cur5" || variables["contentOffsetSeconds"] != nil || variables["videoID"] != "123456" {
		t.Fatalf("cursor variables: %v", variables)
	}
	// A cursor wins over an offset.
	if _, err := chat.Replay(context.Background(), "123456", 9, "cur5"); err != nil || upstream.last()["variables"].(map[string]any)["cursor"] != "cur5" || upstream.last()["variables"].(map[string]any)["contentOffsetSeconds"] != nil {
		t.Fatal("cursor must win over offset")
	}
}

func TestReplayEmptyChatAndUnusableCursor(t *testing.T) {
	t.Parallel()
	p, _ := replayPlatform(t)
	page, err := p.Chat().Replay(context.Background(), "904", 0, "")
	raw, _ := json.Marshal(page)
	if err != nil || page.Messages == nil || len(page.Messages) != 0 || page.Next != "" || string(raw) != `{"messages":[]}` {
		t.Fatalf("empty chat: %+v %v %s", page, err, raw)
	}
	// A next cursor of a shape the API would not accept back is not offered.
	if page, err = p.Chat().Replay(context.Background(), "905", 0, ""); err != nil || len(page.Messages) != 1 || page.Next != "" {
		t.Fatalf("unusable cursor: %+v %v", page, err)
	}
}

func TestReplayUpstreamFailuresAreGenericAndSafe(t *testing.T) {
	t.Parallel()
	p, upstream := replayPlatform(t)
	// 500: HTTP error, 404: persisted query rejected, 901: unknown video, 902: no chat, 903: not JSON, 906: no answer.
	for _, id := range []string{"500", "404", "901", "902", "903", "906"} {
		_, err := p.Chat().Replay(context.Background(), id, 1, "")
		assertCode(t, err, apierr.CodeRequestFailed)
		failure := err.(*apierr.Error)
		if strings.Contains(failure.Message, "secret") || failure.Cause == nil {
			t.Errorf("%s: %+v", id, failure)
		}
	}
	if upstream.count() != 6 {
		t.Fatalf("each failure reached Twitch once: %d", upstream.count())
	}
	_, err := p.Chat().Replay(context.Background(), "403", 1, "")
	assertCode(t, err, apierr.CodeSessionInvalid)

	_, err = deadPlatform(t).Chat().Replay(context.Background(), "123456", 1, "")
	assertCode(t, err, apierr.CodeRequestFailed)
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("the upstream address leaked: %v", err)
	}
}

func TestReplayRefusesAnInvalidVODWithoutAskingTwitch(t *testing.T) {
	t.Parallel()
	p, upstream := replayPlatform(t)
	for _, id := range []string{"abc", "0123", "", "12345678901234567890123", "1\"}"} {
		_, err := p.Chat().Replay(context.Background(), id, 0, "")
		assertCode(t, err, apierr.CodeInvalidSourceArg)
	}
	if upstream.count() != 0 {
		t.Fatalf("invalid input must not reach Twitch (%d requests)", upstream.count())
	}
}

func TestReplayStopsWithItsContext(t *testing.T) {
	t.Parallel()
	p, _ := replayPlatform(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Chat().Replay(ctx, "123456", 0, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled request is not an upstream failure: %v", err)
	}
}
