// Package twitch is the Twitch implementation of platform.Platform: VOD and stream metadata, listings and
// playback manifests through yt-dlp, the public GraphQL API for what yt-dlp omits (profiles, search, live
// status, subscriber-only VODs) and for a channel's whole video list and collections (platform.CatalogSyncer),
// the cookie session of an account (platform.Connector) and the chat of a channel, followed live over IRC or
// replayed with a VOD (platform.Chat).
package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/safehttp"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// errUnexpectedAnswer is the cause of a 200 answer that does not hold what was asked for.
var errUnexpectedAnswer = errors.New("twitch answered without the expected data")

// ID is the platform id.
const ID = "twitch"

const (
	defaultValidateURL = "https://id.twitch.tv/oauth2/validate"
	defaultAPIURL      = "https://api.twitch.tv/helix"
	defaultGraphQLURL  = "https://gql.twitch.tv/gql"
	jsonLimit          = 4 << 20

	// publicClientID is Twitch's public web client identifier, not an application secret (yt-dlp uses it too).
	publicClientID = "ue6666qo983tsx6so1t0vnawi233wa"

	defaultIRCHandshakeTimeout = 15 * time.Second
	defaultIRCReadTimeout      = 6 * time.Minute
	ircHost                    = "irc.chat.twitch.tv"
)

var (
	loginPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,25}$`)
	vodIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
)

// Options configures the platform. Only Runner is required; every other zero value is production behaviour.
type Options struct {
	// Runner starts yt-dlp for VOD metadata, listings and manifests.
	Runner *ytdlp.Runner
	// HTTP is the client of every Twitch API call; nil uses safehttp.Public().
	HTTP *http.Client
	// ValidateURL, APIURL and GraphQLURL are the endpoints; tests point them at httptest servers.
	ValidateURL, APIURL, GraphQLURL string
	// IRCDial opens the chat connection; the default is TLS to irc.chat.twitch.tv:6697.
	IRCDial func(ctx context.Context) (net.Conn, error)
	// IRCHandshakeTimeout bounds the wait for the join to be acknowledged, IRCReadTimeout the silence an
	// established chat connection tolerates (Twitch pings every few minutes).
	IRCHandshakeTimeout, IRCReadTimeout time.Duration
	Log                                 *slog.Logger
}

// Platform is Twitch. It is safe for concurrent use.
type Platform struct {
	runner                          *ytdlp.Runner
	client                          *http.Client
	validateURL, apiURL, graphQLURL string
	ircDial                         func(ctx context.Context) (net.Conn, error)
	ircHandshake, ircRead           time.Duration
	log                             *slog.Logger
}

var _ platform.Platform = (*Platform)(nil)

// New builds the platform.
func New(opts Options) *Platform {
	if opts.Runner == nil {
		panic("twitch: Options.Runner is required")
	}
	p := &Platform{
		runner: opts.Runner, client: opts.HTTP,
		validateURL: opts.ValidateURL, apiURL: opts.APIURL, graphQLURL: opts.GraphQLURL,
		ircDial: opts.IRCDial, ircHandshake: opts.IRCHandshakeTimeout, ircRead: opts.IRCReadTimeout, log: opts.Log,
	}
	if p.client == nil {
		p.client = safehttp.Public()
	}
	if p.validateURL == "" {
		p.validateURL = defaultValidateURL
	}
	if p.apiURL == "" {
		p.apiURL = defaultAPIURL
	}
	if p.graphQLURL == "" {
		p.graphQLURL = defaultGraphQLURL
	}
	if p.ircDial == nil {
		p.ircDial = dialIRC
	}
	if p.ircHandshake <= 0 {
		p.ircHandshake = defaultIRCHandshakeTimeout
	}
	if p.ircRead <= 0 {
		p.ircRead = defaultIRCReadTimeout
	}
	if p.log == nil {
		p.log = slog.New(slog.DiscardHandler)
	}
	return p
}

func (*Platform) ID() string { return ID }

func (*Platform) Descriptor() platform.Descriptor {
	return platform.Descriptor{
		Platform: ID, Name: "Twitch",
		Description:       "Browse public Twitch channels and videos without signing in. Connect optionally for followed live channels and subscriber-only content.",
		Capabilities:      []string{"home", "search", "video", "channel", "stream"},
		ConnectionMethods: []string{"cookies"},
	}
}

func (p *Platform) Connector() platform.Connector { return connector{p} }

func (*Platform) DefaultEnabled() bool { return true }

// A Twitch VOD ID is a decimal number and a live is opened by the channel login, while a YouTube video ID is
// always exactly 11 characters: YouTube is asked first, so the one case that needs the platform to be stated
// is an 11-character login or VOD ID.
func (p *Platform) ClaimsVideoID(id string) bool { return len(id) != 11 && p.ValidVideoID(id) }

func (*Platform) ValidVideoID(id string) bool {
	return vodIDPattern.MatchString(id) || loginPattern.MatchString(id)
}

func (*Platform) ValidChannelID(id string) bool   { return loginPattern.MatchString(id) }
func (*Platform) ValidLiveChannel(id string) bool { return loginPattern.MatchString(id) }

func (*Platform) SingleTitle() bool { return true }

// OriginalURL is the VOD's page, or the channel's for a stream in progress, whose id is the login.
func (*Platform) OriginalURL(id string) string {
	page, err := videoURL(id)
	if err != nil {
		return ""
	}
	return page
}

func (*Platform) ResolveURL(link platform.Link) (string, bool) {
	if (link.Host == "twitch.tv" || link.Host == "www.twitch.tv") && len(link.Parts) == 2 && link.Parts[0] == "videos" && vodIDPattern.MatchString(link.Parts[1]) {
		return link.Parts[1], true
	}
	return "", false
}

// ChannelIDAliases: a channel is stored under its lowercased login.
func (*Platform) ChannelIDAliases(id string) []string { return []string{strings.ToLower(id)} }

func (*Platform) TrustedMediaHost(host string) bool {
	for _, domain := range []string{"ttvnw.net", "twitch.tv"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// EnrichListing does nothing: the listing is completed while it is built, from the same GraphQL request
// that supplies the channel profile.
func (*Platform) EnrichListing(context.Context, string, []platform.Video) {}

func (*Platform) LiveThumbnail(stream platform.LiveStream) string {
	return "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + strings.ToLower(stream.VideoID) + "-640x360.jpg"
}

// EndLive: a stream is opened by the channel login, so the channel's pseudo-video is named by it. It is
// meaningless once the stream is over, since the VOD appears under its own numeric id.
func (p *Platform) EndLive(channelID string) platform.LiveEnd {
	return platform.LiveEnd{ChannelIDs: p.ChannelIDAliases(channelID), StubVideoID: strings.ToLower(channelID)}
}

// CommentsMode: Twitch's conversation is a chat followed with the video, not a list of comments.
func (*Platform) CommentsMode() string { return platform.CommentsChat }

func (*Platform) Comments(context.Context, string, string) ([]platform.Comment, error) {
	return nil, apierr.InvalidInput("This platform has no comment list.")
}

// CatalogSync lists a channel's whole video list through the public GraphQL API.
func (p *Platform) CatalogSync() platform.CatalogSyncer { return catalogSyncer{p} }

func (p *Platform) Chat() platform.Chat { return chat{p} }

// fail turns what a lookup returned into what the boundary may show: classified failures keep their
// meaning, cancellation passes through, anything else is a generic upstream failure with its cause attached.
// yt-dlp's message is only inspected here: it never reaches the client.
func fail(err error) error {
	var known *apierr.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &known), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ytdlp.ErrRateLimited):
		return apierr.RateLimited().WithCause(err)
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "subscriber-only"):
		return apierr.SubscriberOnly().WithCause(err)
	case strings.Contains(message, "not currently live"):
		return apierr.ChannelOffline().WithCause(err)
	}
	return apierr.RequestFailed().WithCause(err)
}

// readJSON sends a request and decodes its one JSON document.
func (p *Platform) readJSON(request *http.Request, target any) error {
	response, err := p.client.Do(request)
	if err != nil {
		return fail(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return apierr.SessionInvalid()
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return apierr.RateLimited().WithCause(ytdlp.ErrRateLimited)
	}
	if response.StatusCode != http.StatusOK {
		return apierr.RequestFailed().WithCause(errors.New("twitch answered HTTP " + response.Status))
	}
	limited := &io.LimitedReader{R: response.Body, N: jsonLimit + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(target); err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF || limited.N <= 0 {
		return apierr.RequestFailed().WithCause(errors.New("unexpected twitch answer"))
	}
	return nil
}
