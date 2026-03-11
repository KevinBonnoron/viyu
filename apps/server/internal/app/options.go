package app

import (
	"net/http"

	"github.com/KevinBonnoron/viyu/apps/server/internal/chat"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/peertube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/twitch"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/youtube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// Options configures the application. The zero value of every field is production behaviour; the fields
// after DisableChannelProfiles are the seams tests use to stay offline and fast — they are configuration
// of the parts, never package state, so applications built side by side do not share anything.
type Options struct {
	DataDir string
	UIDir   string
	// DisableChannelProfiles stops the background lookups of channel avatars and subscriber counts and of
	// video publication dates.
	DisableChannelProfiles bool

	// YTDLP configures the downloader runner shared by every platform (binary, concurrency, backoff).
	YTDLP ytdlp.Options
	// YouTube, Twitch and PeerTube configure the platforms (endpoints, clients); their downloader runner
	// is always the shared one.
	YouTube  youtube.Options
	Twitch   twitch.Options
	PeerTube peertube.Options
	// Chat configures the chat rooms (limits and timings); its Spawn is always the application's.
	Chat chat.Options
	// MediaHTTP is the client the HLS proxy fetches media with; nil is the hardened public-Internet client.
	MediaHTTP *http.Client
	// OCR reads episode numbers from thumbnails; nil uses the engine compiled in (none without the `ocr` build tag).
	OCR series.EpisodeReader
	// Jobs tunes the background workers (intervals, batch sizes); the zero value is production behaviour.
	Jobs JobOptions
}
