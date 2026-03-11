package twitch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestParseIRCLine(t *testing.T) {
	t.Parallel()
	line, ok := parseIRCLine("@id=abc;display-name=Some\\sOne;empty=;flag :nick!nick@nick.tmi.twitch.tv PRIVMSG #chan :hello : world\r\n")
	if !ok || line.command != "PRIVMSG" || line.prefix != "nick!nick@nick.tmi.twitch.tv" || !reflect.DeepEqual(line.params, []string{"#chan"}) || line.trailing != "hello : world" {
		t.Fatalf("privmsg: %+v", line)
	}
	if line.tags["id"] != "abc" || line.tags["display-name"] != "Some One" || line.tags["empty"] != "" || len(line.tags) != 4 {
		t.Fatalf("tags: %v", line.tags)
	}
	if line, ok := parseIRCLine("PING :tmi.twitch.tv"); !ok || line.command != "PING" || line.trailing != "tmi.twitch.tv" {
		t.Fatalf("ping: %+v", line)
	}
	if line, ok := parseIRCLine(":tmi.twitch.tv 366 justinfan1 #chan :End of /NAMES list"); !ok || line.command != "366" || len(line.params) != 2 {
		t.Fatalf("366: %+v", line)
	}
	for _, hostile := range []string{"", "\r\n", "@", "@tags", ":", ":prefix", "@a=b ", ":p ", "   ", ": "} {
		if line, ok := parseIRCLine(hostile); ok {
			t.Errorf("%q must be rejected, got %+v", hostile, line)
		}
	}
}

func TestChatFragmentsSplitOnCodePointRanges(t *testing.T) {
	t.Parallel()
	text := "héllo Kappa 😀 Kappa end"
	// Code point indexes: "héllo " is 6, Kappa 6-10, the emoji at 12, Kappa 14-18.
	got := chatFragments(text, "25:6-10,14-18/emotesv2_1a:12-12")
	want := []platform.ChatFragment{
		{Text: "héllo "},
		{Text: "Kappa", Emote: "25"},
		{Text: " "},
		{Text: "😀", Emote: "emotesv2_1a"},
		{Text: " "},
		{Text: "Kappa", Emote: "25"},
		{Text: " end"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fragments:\n got %+v\nwant %+v", got, want)
	}
	if got := chatFragments("plain", ""); !reflect.DeepEqual(got, []platform.ChatFragment{{Text: "plain"}}) {
		t.Fatalf("plain: %+v", got)
	}
	if got := chatFragments("", ""); len(got) != 0 {
		t.Fatalf("empty: %+v", got)
	}
}

func TestChatFragmentsIgnoreMalformedRanges(t *testing.T) {
	t.Parallel()
	for _, emotes := range []string{
		"", "garbage", "25", "25:", "25:1", "25:a-b", "25:3-1", "25:-1-2", "25:0-999", "25:99999999999999999999-5",
		"bad id:0-1", ":0-1", "25:0-1,", "///", "25:0-1/25:0-1x", "25:1-2,0-3",
	} {
		got := chatFragments("abcd", emotes)
		var joined strings.Builder
		for _, fragment := range got {
			joined.WriteString(fragment.Text)
		}
		if joined.String() != "abcd" {
			t.Errorf("%q lost or invented text: %+v", emotes, got)
		}
	}
	// Overlapping ranges: the one starting first wins, the other is ignored.
	got := chatFragments("abcd", "25:1-2,0-3")
	if len(got) != 1 || got[0] != (platform.ChatFragment{Text: "abcd", Emote: "25"}) {
		t.Fatalf("overlap: %+v", got)
	}
}

func TestChatFragmentsCapTextAndEmoteCount(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 2000)
	got := chatFragments(long, "25:0-1/25:1999-1999")
	total := 0
	for _, fragment := range got {
		total += len([]rune(fragment.Text))
	}
	if total != textLimit || got[0].Emote != "25" {
		t.Fatalf("cap: total=%d first=%+v", total, got[0])
	}
	var ranges []string
	for i := range 400 {
		ranges = append(ranges, strconv.Itoa(i)+"-"+strconv.Itoa(i))
	}
	got = chatFragments(strings.Repeat("a", 400), "25:"+strings.Join(ranges, ","))
	emotes := 0
	for _, fragment := range got {
		if fragment.Emote != "" {
			emotes++
		}
	}
	if emotes != emoteLimit {
		t.Fatalf("emote cap: %d", emotes)
	}
}

func TestChatMessageFromIRCCarriesWhatItAnswers(t *testing.T) {
	t.Parallel()
	line, _ := parseIRCLine(`@id=m2;display-name=Sierra;reply-parent-msg-id=parent-1;reply-parent-display-name=Rhiboz;reply-parent-msg-body=le\smême\smot\sde\spasse :sierra!sierra@sierra.tmi.twitch.tv PRIVMSG #chan :@Rhiboz c'est le même`)
	message, ok := chatMessageFromIRC(line)
	if !ok || message.Reply == nil || *message.Reply != (platform.ChatReply{ID: "parent-1", User: "Rhiboz", Text: "le même mot de passe"}) {
		t.Fatalf("reply: %+v", message)
	}
	if message.Fragments[0].Text != "@Rhiboz c'est le même" {
		t.Fatalf("the text keeps its leading mention, so the emote ranges stay valid: %+v", message.Fragments)
	}

	long := strings.Repeat("é", 400)
	line, _ = parseIRCLine("@id=m3;display-name=Sierra;reply-parent-msg-id=p;reply-parent-display-name=Rhiboz;reply-parent-msg-body=" + long + " :s!s@s.tmi.twitch.tv PRIVMSG #chan :hi")
	if message, _ = chatMessageFromIRC(line); message.Reply == nil || len([]rune(message.Reply.Text)) != replyTextLimit {
		t.Fatalf("the answered text is cut: %+v", message.Reply)
	}

	for _, tags := range []string{"reply-parent-msg-id=p", "reply-parent-display-name=Rhiboz", ""} {
		line, _ = parseIRCLine("@id=m4;display-name=Sierra;" + tags + " :s!s@s.tmi.twitch.tv PRIVMSG #chan :hi")
		if message, ok = chatMessageFromIRC(line); !ok || message.Reply != nil {
			t.Errorf("tags %q: a parent without author or id is not shown: %+v", tags, message)
		}
	}
}

func TestChatMessageFromIRC(t *testing.T) {
	t.Parallel()
	line, _ := parseIRCLine("@id=m1;display-name=Viewer;color=#1E90FF;emotes=25:0-4 :viewer!viewer@viewer.tmi.twitch.tv PRIVMSG #chan :Kappa hi")
	message, ok := chatMessageFromIRC(line)
	if !ok || message.ID != "m1" || message.At != nil || message.User != "Viewer" || message.Color != "#1E90FF" || !reflect.DeepEqual(message.Fragments, []platform.ChatFragment{{Text: "Kappa", Emote: "25"}, {Text: " hi"}}) {
		t.Fatalf("message: %+v", message)
	}
	data, _ := json.Marshal(message)
	if strings.Contains(string(data), `"at"`) {
		t.Fatalf("live messages have no at: %s", data)
	}

	// No display name: the nick of the prefix; invalid colour dropped; /me wrapper stripped before the emote ranges apply.
	line, _ = parseIRCLine("@color=red;emotes=25:0-4 :nick!nick@nick.tmi.twitch.tv PRIVMSG #chan :\x01ACTION Kappa\x01")
	message, ok = chatMessageFromIRC(line)
	if !ok || message.User != "nick" || message.Color != "" || !reflect.DeepEqual(message.Fragments, []platform.ChatFragment{{Text: "Kappa", Emote: "25"}}) {
		t.Fatalf("action: %+v", message)
	}

	for _, raw := range []string{
		":nick!nick@nick.tmi.twitch.tv PRIVMSG #chan",
		":nick!nick@nick.tmi.twitch.tv PRIVMSG #chan :",
		"PRIVMSG #chan :no nick",
		"@display-name=X :x!x@x.tmi.twitch.tv NOTICE #chan :not a message",
	} {
		line, _ := parseIRCLine(raw)
		if message, ok := chatMessageFromIRC(line); ok {
			t.Errorf("%q must not produce a message: %+v", raw, message)
		}
	}
}

// fakeIRC is a local TCP server standing in for irc.chat.twitch.tv.
type fakeIRC struct {
	ln    net.Listener
	ack   atomic.Bool // acknowledge JOIN with 001 + ROOMSTATE
	conns chan *fakeIRCConn
	mu    sync.Mutex
	all   []*fakeIRCConn
}

type fakeIRCConn struct {
	net.Conn
	lines chan string
}

func (c *fakeIRCConn) send(line string) {
	_, _ = c.Conn.Write([]byte(line + "\r\n"))
}

func newFakeIRC(t *testing.T, ack bool) *fakeIRC {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIRC{ln: ln, conns: make(chan *fakeIRCConn, 32)}
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
			fc := &fakeIRCConn{Conn: conn, lines: make(chan string, 64)}
			f.mu.Lock()
			f.all = append(f.all, fc)
			f.mu.Unlock()
			go f.serve(fc)
		}
	}()
	return f
}

func (f *fakeIRC) dial(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", f.ln.Addr().String())
}

func (f *fakeIRC) serve(c *fakeIRCConn) {
	scanner := bufio.NewScanner(c.Conn)
	for scanner.Scan() {
		line := scanner.Text()
		select {
		case c.lines <- line:
		default:
		}
		if strings.HasPrefix(line, "JOIN ") {
			if f.ack.Load() {
				c.send(":tmi.twitch.tv 001 justinfan1 :Welcome, GLHF!")
				c.send("@emote-only=0;room-id=1 :tmi.twitch.tv ROOMSTATE " + strings.TrimPrefix(line, "JOIN "))
			}
			select {
			case f.conns <- c:
			default:
			}
		}
	}
}

func (f *fakeIRC) nextConn(t *testing.T) *fakeIRCConn {
	t.Helper()
	select {
	case c := <-f.conns:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no IRC connection")
		return nil
	}
}

func waitFor[T any](t *testing.T, what string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: %s", what)
		panic("unreachable")
	}
}

func ircPlatform(t *testing.T, irc *fakeIRC, opts Options) *Platform {
	t.Helper()
	opts.IRCDial = irc.dial
	return newPlatform(t, opts)
}

func TestFollowHandshakeMessagesAndPing(t *testing.T) {
	t.Parallel()
	irc := newFakeIRC(t, true)
	chat := ircPlatform(t, irc, Options{IRCHandshakeTimeout: 2 * time.Second}).Chat()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	messages := make(chan platform.ChatMessage, 4)
	type outcome struct {
		wasReady bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		wasReady, err := chat.Follow(ctx, "ZeRaTor", func() { close(ready) }, func(m platform.ChatMessage) { messages <- m })
		done <- outcome{wasReady, err}
	}()
	conn := irc.nextConn(t)
	waitFor(t, "ready", ready)

	var received []string
	for len(received) < 4 {
		received = append(received, waitFor(t, "handshake lines", conn.lines))
	}
	if received[0] != "CAP REQ :twitch.tv/tags twitch.tv/commands" || received[1] != "PASS SCHMOOPIIE" || !regexp.MustCompile(`^NICK justinfan\d{5}$`).MatchString(received[2]) || received[3] != "JOIN #zerator" {
		t.Fatalf("handshake: %v", received)
	}

	conn.send("PING :tmi.twitch.tv")
	if pong := waitFor(t, "pong", conn.lines); pong != "PONG :tmi.twitch.tv" {
		t.Fatalf("pong: %q", pong)
	}
	conn.send("@id=1;display-name=A;emotes=25:0-4 :a!a@a.tmi.twitch.tv PRIVMSG #zerator :Kappa é")
	conn.send(":tmi.twitch.tv NOTICE #zerator :ignored")
	conn.send("@id=2;display-name=B :b!b@b.tmi.twitch.tv PRIVMSG #zerator :second")
	for _, want := range []string{"1", "2"} {
		if m := waitFor(t, "message", messages); m.ID != want {
			t.Fatalf("message order: got %s want %s", m.ID, want)
		}
	}
	cancel()
	result := waitFor(t, "the session to stop with its context", done)
	if !result.wasReady || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("session outcome: %+v", result)
	}
}

func TestFollowWithoutAckHitsTheHandshakeTimeout(t *testing.T) {
	t.Parallel()
	irc := newFakeIRC(t, false)
	chat := ircPlatform(t, irc, Options{IRCHandshakeTimeout: 100 * time.Millisecond}).Chat()
	ready, err := chat.Follow(context.Background(), "silent", func() { t.Error("no ROOMSTATE, no ready") }, func(platform.ChatMessage) {})
	if ready || err == nil {
		t.Fatalf("a silent server must hit the handshake timeout: ready=%v err=%v", ready, err)
	}
	assertCode(t, err, apierr.CodeRequestFailed)
	_ = irc.nextConn(t)
}

func TestFollowDialFailureAndInvalidChannel(t *testing.T) {
	t.Parallel()
	dialErr := errors.New("dial refused")
	chat := newPlatform(t, Options{IRCDial: func(context.Context) (net.Conn, error) { return nil, dialErr }}).Chat()
	ready, err := chat.Follow(context.Background(), "nowhere", func() {}, func(platform.ChatMessage) {})
	if ready || !errors.Is(err, dialErr) {
		t.Fatalf("dial failure: ready=%v err=%v", ready, err)
	}
	assertCode(t, err, apierr.CodeRequestFailed)

	dialed := false
	chat = newPlatform(t, Options{IRCDial: func(context.Context) (net.Conn, error) { dialed = true; return nil, dialErr }}).Chat()
	for _, channel := range []string{"", "a b", "chan\r\nJOIN #other", strings.Repeat("a", 26)} {
		if _, err := chat.Follow(context.Background(), channel, func() {}, func(platform.ChatMessage) {}); err == nil {
			t.Errorf("%q was joined", channel)
		}
	}
	if dialed {
		t.Fatal("an invalid channel must not open a connection")
	}
}

func TestFollowSurvivesHostileLines(t *testing.T) {
	t.Parallel()
	irc := newFakeIRC(t, true)
	chat := ircPlatform(t, irc, Options{}).Chat()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan platform.ChatMessage, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = chat.Follow(ctx, "hostile", func() {}, func(m platform.ChatMessage) { messages <- m })
	}()
	conn := irc.nextConn(t)
	for _, hostile := range []string{"@", ":", "@a=b", "PRIVMSG", ":x PRIVMSG #c", "@emotes=1:9999-0,x:-,/// :x!x@x PRIVMSG #c :", "\x00\x01\x02"} {
		conn.send(hostile)
	}
	conn.send("@display-name=Ok;emotes=1:5-99999999999999999999,0-1 :x!x@x PRIVMSG #c :survived")
	if m := waitFor(t, "a message after hostile input", messages); m.User != "Ok" || len(m.Fragments) != 2 || m.Fragments[0].Emote != "1" {
		t.Fatalf("message after hostile input: %+v", m)
	}
	cancel()
	waitFor(t, "the session to stop", done)
}

func TestFollowEndsCleanlyOnReconnectAndFailsOnSilence(t *testing.T) {
	t.Parallel()
	irc := newFakeIRC(t, true)
	chat := ircPlatform(t, irc, Options{}).Chat()
	type outcome struct {
		wasReady bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		wasReady, err := chat.Follow(context.Background(), "moving", func() {}, func(platform.ChatMessage) {})
		done <- outcome{wasReady, err}
	}()
	conn := irc.nextConn(t)
	conn.send(":tmi.twitch.tv RECONNECT")
	if result := waitFor(t, "the session to end on RECONNECT", done); !result.wasReady || result.err != nil {
		t.Fatalf("RECONNECT is a clean end: %+v", result)
	}

	// An established connection that goes quiet for longer than the read timeout is a failure.
	quiet := ircPlatform(t, irc, Options{IRCReadTimeout: 100 * time.Millisecond}).Chat()
	go func() {
		wasReady, err := quiet.Follow(context.Background(), "quiet", func() {}, func(platform.ChatMessage) {})
		done <- outcome{wasReady, err}
	}()
	irc.nextConn(t)
	result := waitFor(t, "the read timeout", done)
	if !result.wasReady {
		t.Fatalf("the join was acknowledged: %+v", result)
	}
	assertCode(t, result.err, apierr.CodeRequestFailed)
}

func TestChatValidation(t *testing.T) {
	t.Parallel()
	chat := newPlatform(t, Options{}).Chat()
	for channel, want := range map[string]bool{"zerator": true, "Zerator_1": true, "": false, "a-b": false, "#chan": false, strings.Repeat("a", 26): false} {
		if got := chat.ValidChannel(channel); got != want {
			t.Errorf("ValidChannel(%q) = %v", channel, got)
		}
	}
	for id, want := range map[string]bool{"123": true, "2919341981": true, "0": false, "0123": false, "abc": false, "": false, strings.Repeat("1", 21): false} {
		if got := chat.ValidReplayID(id); got != want {
			t.Errorf("ValidReplayID(%q) = %v", id, got)
		}
	}
}
