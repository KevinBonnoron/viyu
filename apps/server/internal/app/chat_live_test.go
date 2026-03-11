package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/chat"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// chatIRC is a local TCP server standing in for irc.chat.twitch.tv.
type chatIRC struct {
	ln       net.Listener
	ack      atomic.Bool // acknowledge JOIN with 001 + ROOMSTATE
	accepted atomic.Int32
	joins    chan *chatIRCConn
	mu       sync.Mutex
	all      []*chatIRCConn
}

type chatIRCConn struct {
	net.Conn
	lines  chan string
	closed chan struct{}
}

func (c *chatIRCConn) send(line string) { _, _ = c.Conn.Write([]byte(line + "\r\n")) }

func newChatIRC(t *testing.T, ack bool) *chatIRC {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &chatIRC{ln: ln, joins: make(chan *chatIRCConn, 32)}
	f.ack.Store(ack)
	t.Cleanup(func() {
		_ = ln.Close()
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, conn := range f.all {
			_ = conn.Close()
		}
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c := &chatIRCConn{Conn: conn, lines: make(chan string, 64), closed: make(chan struct{})}
			f.accepted.Add(1)
			f.mu.Lock()
			f.all = append(f.all, c)
			f.mu.Unlock()
			go f.serve(c)
		}
	}()
	return f
}

func (f *chatIRC) dial(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", f.ln.Addr().String())
}

func (f *chatIRC) serve(c *chatIRCConn) {
	defer close(c.closed)
	scanner := bufio.NewScanner(c.Conn)
	for scanner.Scan() {
		line := scanner.Text()
		select {
		case c.lines <- line:
		default:
		}
		if channel, joined := strings.CutPrefix(line, "JOIN "); joined {
			if f.ack.Load() {
				c.send(":tmi.twitch.tv 001 justinfan1 :Welcome, GLHF!")
				c.send("@emote-only=0;room-id=1 :tmi.twitch.tv ROOMSTATE " + channel)
			}
			select {
			case f.joins <- c:
			default:
			}
		}
	}
}

func (f *chatIRC) nextConn(t *testing.T) *chatIRCConn {
	t.Helper()
	select {
	case c := <-f.joins:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no IRC connection joined a channel")
		return nil
	}
}

// awaitSignal fails the test when ch does not close in time.
func awaitSignal(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: %s", what)
	}
}

// withChatIRC points the Twitch chat of the application at the fake server, with timings short enough for a
// test, and lets tweak adjust the chat limits.
func withChatIRC(irc *chatIRC, tweak func(*chat.Options)) func(*Options) {
	return func(o *Options) {
		o.Twitch.IRCDial = irc.dial
		o.Twitch.IRCHandshakeTimeout = 2 * time.Second
		o.Chat = chat.Options{RoomIdle: 80 * time.Millisecond, PingInterval: 40 * time.Millisecond, ReconnectBase: 5 * time.Millisecond, ReconnectMax: 20 * time.Millisecond}
		if tweak != nil {
			tweak(&o.Chat)
		}
	}
}

// sseStream reads a live chat stream over a real HTTP connection.
type sseStream struct {
	response *http.Response
	lines    chan string
}

// openChatStream opens GET path on the server as the account of token (the viewer account when it is empty).
func openChatStream(t *testing.T, server, path, token string) *sseStream {
	t.Helper()
	response := chatGet(t, server+path, token)
	stream := &sseStream{response: response, lines: make(chan string, 256)}
	go func() {
		defer close(stream.lines)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			stream.lines <- scanner.Text()
		}
	}()
	return stream
}

func chatGet(t *testing.T, url, token string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func (s *sseStream) next(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-s.lines:
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
func (s *sseStream) event(t *testing.T) (name, data string) {
	t.Helper()
	for {
		line := s.next(t)
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

// chatServer serves the application over a real listener: a live chat stream needs one.
func chatServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	viewerOf(t, handler) // made here, not lazily inside a request served by the listener
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

func TestChatLiveStreamFramingAndDisconnect(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, true)
	_, handler := newApp(t, withChatIRC(irc, nil))
	server := chatServer(t, handler)

	stream := openChatStream(t, server, "/api/chat/live/Zerator", "")
	if stream.response.StatusCode != http.StatusOK || !strings.HasPrefix(stream.response.Header.Get("Content-Type"), "text/event-stream") || stream.response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %d %v", stream.response.StatusCode, stream.response.Header)
	}
	conn := irc.nextConn(t)
	if name, data := stream.event(t); name != "ready" || data != "{}" {
		t.Fatalf("first event: %q %q", name, data)
	}

	conn.send("@id=7;display-name=Viewer;color=#FF0000;emotes=25:0-4 :v!v@v.tmi.twitch.tv PRIVMSG #zerator :Kappa é")
	name, data := stream.event(t)
	var message platform.ChatMessage
	if name != "message" || json.Unmarshal([]byte(data), &message) != nil || message.ID != "7" || message.User != "Viewer" || message.Color != "#FF0000" || len(message.Fragments) != 2 || message.Fragments[0] != (platform.ChatFragment{Text: "Kappa", Emote: "25"}) || message.At != nil {
		t.Fatalf("message event: %q %q", name, data)
	}

	// A comment keeps the stream alive.
	for {
		line := stream.next(t)
		if line == ": ping" {
			break
		}
		if line == "<eof>" {
			t.Fatal("stream ended")
		}
	}

	// Disconnecting releases the subscription: the room closes after the idle delay.
	_ = stream.response.Body.Close()
	awaitSignal(t, "the IRC connection closes after the client left", conn.closed)
}

func TestChatLiveHandshakeCarriesTheLoginAndViewersShareOneRoom(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, true)
	app, handler := newApp(t, withChatIRC(irc, nil))
	server := chatServer(t, handler)

	first := openChatStream(t, server, "/api/chat/live/ZeRaTor", "")
	conn := irc.nextConn(t)
	if name, _ := first.event(t); name != "ready" {
		t.Fatalf("first stream: %q", name)
	}
	var received []string
	for len(received) < 4 {
		select {
		case line := <-conn.lines:
			received = append(received, line)
		case <-time.After(5 * time.Second):
			t.Fatalf("handshake lines: %v", received)
		}
	}
	if received[0] != "CAP REQ :twitch.tv/tags twitch.tv/commands" || received[1] != "PASS SCHMOOPIIE" || !regexp.MustCompile(`^NICK justinfan\d{5}$`).MatchString(received[2]) || received[3] != "JOIN #zerator" {
		t.Fatalf("handshake: %v", received)
	}

	// A second viewer of the same channel shares the room: no second IRC connection.
	other := testUser(t, app, "chat-sharing@example.test")
	second := openChatStream(t, server, "/api/chat/live/zerator", tokenOf(t, other))
	if name, _ := second.event(t); name != "ready" {
		t.Fatalf("second stream: %q", name)
	}
	if irc.accepted.Load() != 1 {
		t.Fatalf("two viewers of one channel opened %d IRC connections", irc.accepted.Load())
	}
	conn.send("@id=1;display-name=A :a!a@a.tmi.twitch.tv PRIVMSG #zerator :hello")
	for _, stream := range []*sseStream{first, second} {
		name, data := stream.event(t)
		var message platform.ChatMessage
		if name != "message" || json.Unmarshal([]byte(data), &message) != nil || message.ID != "1" {
			t.Fatalf("shared message: %q %q", name, data)
		}
	}
}

func TestChatLiveUnavailableEndsTheStream(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, false)
	_, handler := newApp(t, withChatIRC(irc, func(o *chat.Options) { o.MaxAttempts = 2 }), func(o *Options) { o.Twitch.IRCHandshakeTimeout = 50 * time.Millisecond })
	server := chatServer(t, handler)
	stream := openChatStream(t, server, "/api/chat/live/ghost", "")
	if name, data := stream.event(t); name != "unavailable" || data != "{}" {
		t.Fatalf("event: %q %q", name, data)
	}
	if name, _ := stream.event(t); name != "<eof>" {
		t.Fatalf("the stream must end after unavailable, got %q", name)
	}
	if got := irc.accepted.Load(); got != 2 {
		t.Fatalf("attempts: %d", got)
	}
}

func TestChatLiveValidationAndRoomLimit(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, true)
	_, handler := newApp(t, withChatIRC(irc, func(o *chat.Options) { o.MaxRooms = 1 }))
	for _, path := range []string{"/api/chat/live/not%20a%20login", "/api/chat/live/" + strings.Repeat("a", 26), "/api/chat/live/a-b"} {
		res := request(t, handler, path, "")
		if res.Code != http.StatusBadRequest || res.Header().Get("Cache-Control") != "no-store" || !strings.Contains(res.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body)
		}
	}
	if irc.accepted.Load() != 0 {
		t.Fatal("an invalid login must not open an IRC connection")
	}

	server := chatServer(t, handler)
	stream := openChatStream(t, server, "/api/chat/live/first", "")
	if name, _ := stream.event(t); name != "ready" {
		t.Fatalf("first stream: %q", name)
	}
	response := chatGet(t, server+"/api/chat/live/second", "")
	var failure struct{ Code, Message string }
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil || response.StatusCode != http.StatusServiceUnavailable || failure.Code != "UNAVAILABLE" {
		t.Fatalf("room limit: %d %+v %v", response.StatusCode, failure, err)
	}
}

func TestChatLiveNeedsAnAccountAndAnEnabledSource(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, true)
	app, handler := newApp(t, withChatIRC(irc, nil))
	server := chatServer(t, handler)
	if res := anonymousRequest(t, handler, "/api/chat/live/zerator"); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous chat: %d %s", res.Code, res.Body)
	}
	user := testUser(t, app, "chat-off@example.test")
	token := tokenOf(t, user)
	if res := send(t, handler, http.MethodPut, "/api/sources/twitch/activation", token, `{"enabled":false}`); res.Code != http.StatusOK {
		t.Fatalf("turn Twitch off: %d %s", res.Code, res.Body)
	}
	response := chatGet(t, server+"/api/chat/live/zerator", token)
	var failure struct{ Code string }
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil || response.StatusCode != http.StatusConflict || failure.Code != "SOURCE_DISABLED" {
		t.Fatalf("chat of a source the account turned off: %d %+v %v", response.StatusCode, failure, err)
	}
	if irc.accepted.Load() != 0 {
		t.Fatal("a refused viewer opened an IRC connection")
	}
}

// Shutting the application down ends the streams it serves and closes the chat connections they hold.
func TestChatLiveStopsWithTheApplication(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, true)
	app, handler := newApp(t, withChatIRC(irc, nil))
	server := chatServer(t, handler)
	stream := openChatStream(t, server, "/api/chat/live/shutdown", "")
	conn := irc.nextConn(t)
	if name, _ := stream.event(t); name != "ready" {
		t.Fatalf("stream: %q", name)
	}
	stopped := make(chan struct{})
	go func() { Shutdown(app); close(stopped) }()
	awaitSignal(t, "shutdown returns while a chat stream is open", stopped)
	awaitSignal(t, "the IRC connection closes with the application", conn.closed)
	if name, _ := stream.event(t); name != "<eof>" {
		t.Fatalf("the stream must end with the application, got %q", name)
	}
}
