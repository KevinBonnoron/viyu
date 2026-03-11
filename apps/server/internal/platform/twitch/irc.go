package twitch

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"math/rand/v2"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	// replyTextLimit is how many runes of the answered message are kept: enough to recognise it, not to read it.
	replyTextLimit = 160
	textLimit      = 500 // runes kept of one message
	emoteLimit     = 200 // emote ranges honoured in one message
	ircLineLimit   = 16 << 10
)

var (
	colorPattern   = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	emoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
)

func dialIRC(ctx context.Context) (net.Conn, error) {
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: ircHost}}
	return dialer.DialContext(ctx, "tcp", ircHost+":6697")
}

// chat is Twitch's conversation: anonymous IRC for a live channel, the public GraphQL API for a VOD.
type chat struct{ p *Platform }

var _ platform.Chat = chat{}

func (chat) ValidChannel(channel string) bool  { return loginPattern.MatchString(channel) }
func (chat) ValidReplayID(videoID string) bool { return vodIDPattern.MatchString(videoID) }

// Follow runs one anonymous IRC connection for a channel until it ends. ready is called once the JOIN is
// acknowledged and message for every chat message; wasReady reports whether the JOIN was ever acknowledged.
func (c chat) Follow(ctx context.Context, channel string, ready func(), message func(platform.ChatMessage)) (wasReady bool, err error) {
	if !c.ValidChannel(channel) {
		return false, apierr.InvalidInput("Provide a valid channel.")
	}
	p := c.p
	conn, err := p.ircDial(ctx)
	if err != nil {
		return false, followError(ctx, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(p.ircHandshake))
	nick := fmt.Sprintf("justinfan%05d", rand.IntN(100000))
	if _, err = fmt.Fprintf(conn, "CAP REQ :twitch.tv/tags twitch.tv/commands\r\nPASS SCHMOOPIIE\r\nNICK %s\r\nJOIN #%s\r\n", nick, strings.ToLower(channel)); err != nil {
		return false, followError(ctx, err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), ircLineLimit)
	for scanner.Scan() {
		line, ok := parseIRCLine(scanner.Text())
		if !ok {
			continue
		}
		switch line.command {
		case "PING":
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err = fmt.Fprintf(conn, "PONG :%s\r\n", strings.NewReplacer("\r", "", "\n", "").Replace(platform.First(line.trailing, "tmi.twitch.tv"))); err != nil {
				return wasReady, followError(ctx, err)
			}
		case "ROOMSTATE", "366":
			if !wasReady {
				wasReady = true
				ready()
			}
		case "PRIVMSG":
			if m, ok := chatMessageFromIRC(line); ok {
				message(m)
			}
		case "RECONNECT":
			return wasReady, nil
		}
		if wasReady {
			_ = conn.SetReadDeadline(time.Now().Add(p.ircRead))
		}
	}
	return wasReady, followError(ctx, scanner.Err())
}

// followError is what a chat session reports: nothing for a clean end, the context's error once it ended,
// a generic upstream failure otherwise.
func followError(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	}
	return apierr.RequestFailed().WithCause(err)
}

// ircLine is one parsed IRC protocol line.
type ircLine struct {
	tags     map[string]string
	prefix   string
	command  string
	params   []string
	trailing string
	hasTrail bool
}

var ircTagUnescape = strings.NewReplacer(`\s`, " ", `\:`, ";", `\\`, `\`, `\r`, "\r", `\n`, "\n")

// parseIRCLine splits `@tags :prefix COMMAND params :trailing`. It never panics; a line without a command is rejected.
func parseIRCLine(raw string) (ircLine, bool) {
	var line ircLine
	raw = strings.TrimRight(raw, "\r\n")
	if strings.HasPrefix(raw, "@") {
		end := strings.IndexByte(raw, ' ')
		if end < 0 {
			return line, false
		}
		line.tags = make(map[string]string)
		for _, tag := range strings.Split(raw[1:end], ";") {
			key, value, _ := strings.Cut(tag, "=")
			if key != "" {
				line.tags[key] = ircTagUnescape.Replace(value)
			}
		}
		raw = strings.TrimLeft(raw[end:], " ")
	}
	if strings.HasPrefix(raw, ":") {
		end := strings.IndexByte(raw, ' ')
		if end < 0 {
			return line, false
		}
		line.prefix = raw[1:end]
		raw = strings.TrimLeft(raw[end:], " ")
	}
	if head, trailing, ok := strings.Cut(raw, " :"); ok {
		raw, line.trailing, line.hasTrail = head, trailing, true
	} else if strings.HasPrefix(raw, ":") {
		raw, line.trailing, line.hasTrail = "", raw[1:], true
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return line, false
	}
	line.command, line.params = fields[0], fields[1:]
	return line, true
}

// chatFragments splits text on the code point ranges of an IRC `emotes` tag
// (`<id>:<start>-<end>[,<start>-<end>]/...`). Malformed, out-of-range and overlapping ranges are ignored.
func chatFragments(text, emotes string) []platform.ChatFragment {
	runes := []rune(text)
	if len(runes) > textLimit {
		runes = runes[:textLimit]
	}
	type span struct {
		start, end int
		id         string
	}
	var spans []span
collect:
	for _, item := range strings.Split(emotes, "/") {
		id, ranges, ok := strings.Cut(item, ":")
		if !ok || !emoteIDPattern.MatchString(id) {
			continue
		}
		for _, r := range strings.Split(ranges, ",") {
			from, to, ok := strings.Cut(r, "-")
			start, errStart := strconv.Atoi(from)
			end, errEnd := strconv.Atoi(to)
			if !ok || errStart != nil || errEnd != nil || start < 0 || end < start || end >= len(runes) {
				continue
			}
			if spans = append(spans, span{start, end, id}); len(spans) >= emoteLimit {
				break collect
			}
		}
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	fragments := make([]platform.ChatFragment, 0, 2*len(spans)+1)
	cursor := 0
	for _, s := range spans {
		if s.start < cursor {
			continue
		}
		if s.start > cursor {
			fragments = append(fragments, platform.ChatFragment{Text: string(runes[cursor:s.start])})
		}
		fragments = append(fragments, platform.ChatFragment{Text: string(runes[s.start : s.end+1]), Emote: s.id})
		cursor = s.end + 1
	}
	if cursor < len(runes) {
		fragments = append(fragments, platform.ChatFragment{Text: string(runes[cursor:])})
	}
	return fragments
}

// chatMessageFromIRC builds a chat message from a PRIVMSG line.
func chatMessageFromIRC(line ircLine) (platform.ChatMessage, bool) {
	if line.command != "PRIVMSG" || !line.hasTrail {
		return platform.ChatMessage{}, false
	}
	text := line.trailing
	// "/me" messages arrive as CTCP ACTION; the emote ranges then index the text without the wrapper.
	if strings.HasPrefix(text, "\x01ACTION ") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "\x01ACTION "), "\x01")
	}
	user := line.tags["display-name"]
	if user == "" {
		user, _, _ = strings.Cut(line.prefix, "!")
	}
	fragments := chatFragments(text, line.tags["emotes"])
	if user == "" || len(fragments) == 0 {
		return platform.ChatMessage{}, false
	}
	message := platform.ChatMessage{ID: line.tags["id"], User: user, Fragments: fragments}
	if colorPattern.MatchString(line.tags["color"]) {
		message.Color = line.tags["color"]
	}
	// A reply carries its parent in tags (`reply-parent-*`); a parent without an author or an id is no use to show.
	if parentID, parentUser := line.tags["reply-parent-msg-id"], line.tags["reply-parent-display-name"]; parentID != "" && parentUser != "" {
		body := []rune(strings.TrimPrefix(strings.TrimSuffix(line.tags["reply-parent-msg-body"], "\x01"), "\x01ACTION "))
		if len(body) > replyTextLimit {
			body = body[:replyTextLimit]
		}
		message.Reply = &platform.ChatReply{ID: parentID, User: parentUser, Text: strings.TrimSpace(string(body))}
	}
	return message, true
}
