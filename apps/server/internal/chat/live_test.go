package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// liveServer serves ServeLive the way the HTTP layer does: the account in X-User, the channel in the path,
// errors as {"code","message"}.
func liveServer(t *testing.T, svc *Service, source platform.Chat) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /live/{channel}", func(w http.ResponseWriter, r *http.Request) {
		err := svc.ServeLive(w, r, r.Header.Get("X-User"), "p", source, r.PathValue("channel"))
		if err == nil {
			return
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) {
			apiErr = apierr.RequestFailed()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(apiErr.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": apiErr.Code, "message": apiErr.Message})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func get(t *testing.T, url, user string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-User", user)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

// sseClient reads a live chat stream over a real HTTP connection.
type sseClient struct {
	response *http.Response
	lines    chan string
}

func openSSE(t *testing.T, url, user string) *sseClient {
	t.Helper()
	response := get(t, url, user)
	client := &sseClient{response: response, lines: make(chan string, 256)}
	go func() {
		defer close(client.lines)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			client.lines <- scanner.Text()
		}
	}()
	return client
}

func (c *sseClient) next(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-c.lines:
		if !ok {
			return "<eof>"
		}
		return line
	case <-time.After(5 * time.Second):
		t.Fatal("no SSE line")
		return ""
	}
}

// event returns the next event, skipping comments.
func (c *sseClient) event(t *testing.T) (name, data string) {
	t.Helper()
	for {
		line := c.next(t)
		switch {
		case line == "<eof>":
			return "<eof>", ""
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			return name, strings.TrimPrefix(line, "data: ")
		}
	}
}

func TestServeLiveStreamFramingAndDisconnect(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat := newFakeChat()
	url := liveServer(t, svc, chat)

	client := openSSE(t, url+"/live/Zerator", "viewer")
	if client.response.StatusCode != http.StatusOK || !strings.HasPrefix(client.response.Header.Get("Content-Type"), "text/event-stream") || client.response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %d %v", client.response.StatusCode, client.response.Header)
	}
	session := chat.nextSession(t)
	if name, data := client.event(t); name != "ready" || data != "{}" {
		t.Fatalf("first event: %q %q", name, data)
	}

	session.send(platform.ChatMessage{ID: "7", User: "Viewer", Color: "#FF0000", Fragments: []platform.ChatFragment{{Text: "Kappa", Emote: "25"}, {Text: " é"}}})
	name, data := client.event(t)
	var message platform.ChatMessage
	if name != "message" || json.Unmarshal([]byte(data), &message) != nil || message.ID != "7" || message.User != "Viewer" || message.Color != "#FF0000" || len(message.Fragments) != 2 || message.Fragments[0] != (platform.ChatFragment{Text: "Kappa", Emote: "25"}) || message.At != nil {
		t.Fatalf("message event: %q %q", name, data)
	}

	// A comment keeps the stream alive.
	for {
		if line := client.next(t); line == ": ping" {
			break
		} else if line == "<eof>" {
			t.Fatal("stream ended")
		}
	}

	// Disconnecting releases the subscription: the room closes after the idle delay.
	_ = client.response.Body.Close()
	waitSignal(t, "room closes after the client left", session.closed)
}

func TestServeLiveUnavailableEndsTheStream(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.MaxAttempts = 2 })
	chat := newFakeChat()
	chat.reachable.Store(false)
	client := openSSE(t, liveServer(t, svc, chat)+"/live/ghost", "viewer")
	if name, data := client.event(t); name != "unavailable" || data != "{}" {
		t.Fatalf("event: %q %q", name, data)
	}
	if name, _ := client.event(t); name != "<eof>" {
		t.Fatalf("the stream must end after unavailable, got %q", name)
	}
}

func TestServeLiveEndsWhenTheOwnerStops(t *testing.T) {
	t.Parallel()
	svc, group := newService(t, nil)
	chat := newFakeChat()
	client := openSSE(t, liveServer(t, svc, chat)+"/live/steady", "viewer")
	chat.nextSession(t)
	if name, _ := client.event(t); name != "ready" {
		t.Fatalf("first event: %q", name)
	}
	group.stop()
	if name, _ := client.event(t); name != "<eof>" {
		t.Fatalf("the stream must end when the owner stops, got %q", name)
	}
}

func TestServeLiveRejectsInvalidChannels(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat := newFakeChat()
	url := liveServer(t, svc, chat)
	for _, path := range []string{"/live/not%20a%20login", "/live/" + strings.Repeat("a", 26), "/live/a-b"} {
		res := get(t, url+path, "viewer")
		if res.StatusCode != http.StatusBadRequest || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %v", path, res.StatusCode, res.Header)
		}
	}
	if chat.attempts.Load() != 0 || svc.Rooms() != 0 {
		t.Fatal("an invalid channel must not open an upstream session")
	}
}

func TestServeLiveRoomLimit(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.MaxRooms = 1 })
	url := liveServer(t, svc, newFakeChat())
	client := openSSE(t, url+"/live/first", "viewer")
	if name, _ := client.event(t); name != "ready" {
		t.Fatalf("first stream: %q", name)
	}
	response := get(t, url+"/live/second", "other")
	var body map[string]string
	if response.StatusCode != http.StatusServiceUnavailable || json.NewDecoder(response.Body).Decode(&body) != nil || body["code"] != apierr.CodeUnavailable {
		t.Fatalf("room limit: %d %v", response.StatusCode, body)
	}
}

func TestServeLiveRefusesAnAccountOverItsConnectionCap(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.MaxPerUser = 1 })
	url := liveServer(t, svc, newFakeChat())
	first := openSSE(t, url+"/live/first", "viewer")
	if name, _ := first.event(t); name != "ready" {
		t.Fatalf("first stream: %s", name)
	}
	response := get(t, url+"/live/second", "viewer")
	var body map[string]string
	if response.StatusCode != http.StatusTooManyRequests || json.NewDecoder(response.Body).Decode(&body) != nil || body["code"] != apierr.CodeChatConnections {
		t.Fatalf("second stream of one account: %d %v", response.StatusCode, body)
	}
	second := openSSE(t, url+"/live/second", "someone-else")
	if name, _ := second.event(t); name != "ready" {
		t.Fatalf("another account's stream: %s", name)
	}
}
