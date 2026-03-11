// Package platform defines what a video service looks like to the rest of the server: the domain types
// every layer exchanges (Video, Channel, LiveStream…), the Platform interface each service implements, and
// the Registry that holds them. Everything that differs between services lives behind Platform, so no
// other package names a platform: adding one means implementing the interface in a sub-package and
// listing it where the registry is built.
package platform

import (
	"context"
	"net/url"
)

// Platform is one video service Viyu can browse and play. Every platform implements all of it; what a
// platform does not offer is said by the answer (no connection, no comments), not by a missing method.
//
// Failures cross this boundary as *apierr.Error: a platform classifies what it knows (a subscriber-only
// VOD, a session that expired) and wraps the rest as apierr.RequestFailed with the cause attached.
type Platform interface {
	ID() string
	// Descriptor is what the Sources screen shows: name, description, capabilities and the ways an
	// account can connect.
	Descriptor() Descriptor
	// Connector is the optional authenticated session of an account on the platform, nil when the
	// platform has none.
	Connector() Connector
	// DefaultEnabled is whether an account that never chose uses the platform.
	DefaultEnabled() bool

	// ClaimsVideoID reports whether a bare video ID, opened without the platform of the page it came
	// from, can only be this platform's. Platforms are asked in registry order.
	ClaimsVideoID(id string) bool
	// ValidVideoID accepts the video identifiers the platform can play.
	ValidVideoID(id string) bool
	// OriginalURL is the page of a video on its own service, "" when the id names none.
	OriginalURL(id string) string
	// ValidChannelID accepts the channel identifiers a subscription may name.
	ValidChannelID(id string) bool
	// ValidLiveChannel accepts the identifiers the live lookup can resolve.
	ValidLiveChannel(id string) bool
	// ResolveURL extracts the native video id from a pasted link whose host and path segments are
	// already parsed; ok is false when the link is not a video of this platform.
	ResolveURL(link Link) (nativeID string, ok bool)
	// ChannelIDAliases lists the forms under which the channel the id names may be stored (a handle
	// with and without its "@"); the first is the canonical one.
	ChannelIDAliases(id string) []string
	// SingleTitle reports whether a video has one title only: there is no translation to tell apart
	// from an original.
	SingleTitle() bool
	// TrustedMediaHost reports whether the platform's own media may be fetched from host with the
	// manifest's credentials stripped to what a public CDN needs.
	TrustedMediaHost(host string) bool

	Search(ctx context.Context, session Session, query string, page int) (SearchPage, error)
	VideoMetadata(ctx context.Context, id, cookieFile string) (Video, error)
	ChannelListing(ctx context.Context, id string, limit int, cookieFile string) (Channel, []Video, error)
	PlaybackManifest(ctx context.Context, id, cookieFile string) (Manifest, error)
	// EnrichListing completes the videos of a channel listing with what the platform's cheap public
	// feeds know (exact publication times, canonical titles). It never fails: a lookup that does not
	// answer leaves the listing as it is.
	EnrichListing(ctx context.Context, channelID string, videos []Video)

	LiveStream(ctx context.Context, channelID string) (LiveStream, error)
	LiveThumbnail(stream LiveStream) string
	// EndLive says what to forget of a channel found not to be live.
	EndLive(channelID string) LiveEnd

	// CommentsMode says how the conversation about a video is offered, the same way on every platform:
	// CommentsList is a list of top-level comments, each with the bounded replies the platform fetched;
	// CommentsChat a chat followed live or replayed with the video, "" none.
	CommentsMode() string
	// Comments lists a video's top-level comments with their fetched replies; platforms whose mode is
	// not CommentsList refuse.
	Comments(ctx context.Context, videoID, cookieFile string) ([]Comment, error)

	// CatalogSync is the capability of listing a channel's whole catalog for background synchronization,
	// nil when the platform cannot.
	CatalogSync() CatalogSyncer
	// Chat is the capability of following a channel's live chat and replaying a video's, nil when the
	// platform has none.
	Chat() Chat
}

// Link is a pasted address, parsed once for every platform to inspect.
type Link struct {
	URL *url.URL
	// Host is the lowercased host name.
	Host string
	// Parts are the non-empty path segments.
	Parts []string
}

// Connector is the optional authenticated session of an account on a platform.
type Connector interface {
	// NormalizeCookies keeps only the rows of the platform out of an imported Netscape cookie file and
	// proves they hold a session.
	NormalizeCookies(raw string) (string, error)
	// Validate asks the platform whether the session still works.
	Validate(ctx context.Context, cookies string) error
	// Feed lists one page of the account's personal feed ("home", "subscriptions", "history").
	Feed(ctx context.Context, cookies, kind string, page, limit int) (Feed, error)
	// MarkWatched records a watched video on the account.
	MarkWatched(ctx context.Context, cookies, videoID string) error
}

// CatalogSyncer lists a channel's whole catalog.
type CatalogSyncer interface {
	// Catalog streams every video of the channel to visit as the platform paginates.
	Catalog(ctx context.Context, channel Channel, cookieFile string, visit func(Video) error) error
	// Collections lists the collections the channel itself made, nil when the platform has none. A
	// collection that could not be read to its end is left out: a partial one would name a series that
	// lacks episodes.
	Collections(ctx context.Context, channel Channel) ([]Collection, error)
}

// Collection is a list of videos a channel put together and named, in the order it gave them.
type Collection struct {
	Title string `json:"title"`
	// VideoIDs are the native ids of its videos, in the collection's order.
	VideoIDs []string `json:"videoIds"`
}

// Chat is a platform's conversation followed live or replayed with a video.
type Chat interface {
	// ValidChannel accepts the channel identifiers a live chat can join.
	ValidChannel(channel string) bool
	// ValidReplayID accepts the video identifiers whose chat can be replayed.
	ValidReplayID(videoID string) bool
	// Follow keeps one chat session on the channel until ctx ends or the connection fails, calling ready
	// once the join is acknowledged and message for each chat message. wasReady reports whether the
	// session ever became ready.
	Follow(ctx context.Context, channel string, ready func(), message func(ChatMessage)) (wasReady bool, err error)
	// Replay answers one page of the recorded chat of a video, from a time offset in seconds or an
	// opaque cursor.
	Replay(ctx context.Context, videoID string, offset int, cursor string) (ChatPage, error)
}

// ChatFragment is a piece of a chat message: plain text, or an emote (Text is then the emote's name).
type ChatFragment struct {
	Text  string `json:"text"`
	Emote string `json:"emote,omitempty"`
}

// ChatReply is the message a chat message answers: who wrote it, what it said (cut short) and its id, so a
// client can find it again in what it already holds. Only live chat knows it; a replay does not.
type ChatReply struct {
	ID   string `json:"id"`
	User string `json:"user"`
	Text string `json:"text"`
}

// ChatMessage is the shape shared by live chat (At is absent) and replay (At is the offset in seconds).
type ChatMessage struct {
	ID        string         `json:"id"`
	At        *float64       `json:"at,omitempty"`
	User      string         `json:"user"`
	Color     string         `json:"color,omitempty"`
	Fragments []ChatFragment `json:"fragments"`
	Reply     *ChatReply     `json:"reply,omitempty"`
}

// ChatPage is one page of replayed chat.
type ChatPage struct {
	Messages []ChatMessage `json:"messages"`
	Next     string        `json:"next,omitempty"`
}
