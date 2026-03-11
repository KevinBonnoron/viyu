// Package youtube is the YouTube implementation of platform.Platform: yt-dlp entry parsing, search, channel
// listings and catalogs, live lookup, comments, publication dates from the public RSS feed, and the
// connected session (imported cookies) behind platform.Connector. It depends on no other platform and on
// nothing of the application: every yt-dlp process goes through the injected ytdlp.Runner.
package youtube

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/safehttp"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// platformID is the registry id of the platform.
const platformID = "youtube"

// defaultFeedURL is the public RSS endpoint that lists the latest videos of a channel.
const defaultFeedURL = "https://www.youtube.com/feeds/videos.xml"

var (
	videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	// channelIDPattern is the canonical channel id (UC plus 22 characters); handlePattern is an
	// @handle, whose characters may come from any script.
	channelIDPattern = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	handlePattern    = regexp.MustCompile(`^@[\p{L}\p{N}._-]{1,100}$`)
	// channelRefPattern is what the live lookup resolves: a channel id or a handle, with or without "@".
	channelRefPattern = regexp.MustCompile(`^@?[A-Za-z0-9._-]{1,100}$`)
)

// Options configures the platform.
type Options struct {
	// Runner starts every yt-dlp process; required.
	Runner *ytdlp.Runner
	// HTTP fetches the RSS feed; nil uses safehttp.Public().
	HTTP *http.Client
	// FeedURL is the RSS endpoint; empty uses the public YouTube one.
	FeedURL string
	// Log receives the failures that are not reported to the caller; nil discards them.
	Log *slog.Logger
}

// Platform is the YouTube platform.
type Platform struct {
	runner  *ytdlp.Runner
	http    *http.Client
	feedURL string
	log     *slog.Logger
}

var (
	_ platform.Platform      = (*Platform)(nil)
	_ platform.Connector     = connector{}
	_ platform.CatalogSyncer = catalogSyncer{}
)

// New builds the platform.
func New(opts Options) *Platform {
	if opts.Runner == nil {
		panic("youtube: Options.Runner is required")
	}
	p := &Platform{runner: opts.Runner, http: opts.HTTP, feedURL: opts.FeedURL, log: opts.Log}
	if p.http == nil {
		p.http = safehttp.Public()
	}
	if p.feedURL == "" {
		p.feedURL = defaultFeedURL
	}
	if p.log == nil {
		p.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return p
}

func (*Platform) ID() string { return platformID }

func (*Platform) Descriptor() platform.Descriptor {
	return platform.Descriptor{
		Platform: platformID, Name: "YouTube",
		Description:       "Connect your YouTube session for personal home, subscriptions and watch history.",
		Capabilities:      []string{"home", "subscriptions", "history", "history-write", "search", "video", "channel", "stream"},
		ConnectionMethods: []string{"cookies"},
	}
}

// Connector is the session imported as cookies.
func (p *Platform) Connector() platform.Connector { return connector{p} }

// CatalogSync lists a channel's whole /videos tab.
func (p *Platform) CatalogSync() platform.CatalogSyncer { return catalogSyncer{p} }

// Chat: YouTube has no chat here.
func (*Platform) Chat() platform.Chat { return nil }

func (*Platform) DefaultEnabled() bool { return true }
func (*Platform) SingleTitle() bool    { return false }

func (*Platform) ClaimsVideoID(id string) bool { return videoIDPattern.MatchString(id) }
func (*Platform) ValidVideoID(id string) bool  { return videoIDPattern.MatchString(id) }

func (*Platform) OriginalURL(id string) string {
	if !videoIDPattern.MatchString(id) {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + id
}

// ValidChannelID accepts a canonical channel id or an @handle.
func (*Platform) ValidChannelID(id string) bool {
	return channelIDPattern.MatchString(id) || handlePattern.MatchString(id)
}

func (*Platform) ValidLiveChannel(id string) bool { return channelRefPattern.MatchString(id) }

// ChannelIDAliases lists the canonical id (without "@"), then its handle form.
func (*Platform) ChannelIDAliases(id string) []string {
	canonical := strings.TrimPrefix(id, "@")
	return []string{canonical, "@" + canonical}
}

// TrustedMediaHost accepts the hosts YouTube serves its own media from.
func (*Platform) TrustedMediaHost(host string) bool {
	for _, domain := range []string{"googlevideo.com", "youtube.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (*Platform) LiveThumbnail(stream platform.LiveStream) string {
	return "https://i.ytimg.com/vi/" + stream.VideoID + "/hqdefault_live.jpg"
}

// EndLive: a finished stream stays stored under its own video id, so only the channel's forms are named.
func (p *Platform) EndLive(channel string) platform.LiveEnd {
	return platform.LiveEnd{ChannelIDs: p.ChannelIDAliases(channel)}
}

func (*Platform) CommentsMode() string { return platform.CommentsList }

// spec describes a yt-dlp run of the platform, as the connected session when there is a cookie jar.
func spec(args []string, cookieFile string) ytdlp.Spec {
	return ytdlp.Spec{Args: ytdlp.WithCookies(args, cookieFile)}
}

// entries runs yt-dlp once and collects its JSON objects.
func (p *Platform) entries(ctx context.Context, args []string, cookieFile string) ([]ytEntry, error) {
	entries, err := ytdlp.All[ytEntry](ctx, p.runner, spec(args, cookieFile))
	if err != nil {
		return nil, fail(ctx, err, cookieFile != "")
	}
	return entries, nil
}

// fail turns what a run reported into what may leave the platform: the cancellation of ctx as it is, a
// client-safe error for everything else with the original attached as its cause. A login demand is the
// session's failure only when the request ran as a connected account.
func fail(ctx context.Context, err error, connected bool) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var safe *apierr.Error
	switch {
	case errors.As(err, &safe):
		return err
	case connected && errors.Is(err, ytdlp.ErrAuthRequired):
		return apierr.SessionInvalid().WithCause(err)
	case errors.Is(err, ytdlp.ErrRateLimited):
		return apierr.RateLimited().WithCause(err)
	}
	return apierr.RequestFailed().WithCause(err)
}
