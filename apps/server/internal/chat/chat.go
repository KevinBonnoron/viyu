// Package chat is the platform-independent chat service: the live-chat hub (one upstream session per
// channel, shared by every viewer), the per-account connection limiter, the Server-Sent Events loop and the
// recorded-chat page cache with its request validation. The upstream protocol is a platform's own business;
// it arrives through platform.Chat.
package chat

import (
	"context"
	"regexp"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	defaultMaxRooms         = 30
	defaultMaxPerUser       = 4
	defaultRoomIdle         = 10 * time.Second
	defaultPingInterval     = 20 * time.Second
	defaultReconnectBase    = time.Second
	defaultReconnectMax     = 30 * time.Second
	defaultSubscriberBuffer = 64
	defaultMaxAttempts      = 3
	defaultVODCacheTTL      = 30 * time.Second
	defaultVODCacheEntries  = 64

	maxReplayOffset = 10_000_000
	replayTimeout   = 15 * time.Second
)

var replayCursor = regexp.MustCompile(`^[A-Za-z0-9+/=_.~-]{1,1000}$`)

// Options configures a Service; a zero field takes its default.
type Options struct {
	// MaxRooms bounds the channels followed at once (30).
	MaxRooms int
	// MaxPerUser bounds the live streams one account holds open at once (4).
	MaxPerUser int
	// RoomIdle is how long a room outlives its last viewer (10s).
	RoomIdle time.Duration
	// PingInterval is the keep-alive comment cadence of a live stream (20s).
	PingInterval time.Duration
	// ReconnectBase and ReconnectMax bound the backoff between upstream sessions of a room (1s, 30s).
	ReconnectBase, ReconnectMax time.Duration
	// SubscriberBuffer is the messages a viewer may fall behind before it loses some (64).
	SubscriberBuffer int
	// MaxAttempts consecutive sessions that never became ready make the channel unavailable (3).
	MaxAttempts int
	// VODCacheTTL and VODCacheEntries bound the replay page cache (30s, 64).
	VODCacheTTL     time.Duration
	VODCacheEntries int
	// Spawn owns the goroutines of the rooms and of the replay loads; a room ends when the context it
	// hands over does. Required.
	Spawn cache.Spawner
}

func (o Options) withDefaults() Options {
	fill := func(value *int, fallback int) {
		if *value <= 0 {
			*value = fallback
		}
	}
	fillDuration := func(value *time.Duration, fallback time.Duration) {
		if *value <= 0 {
			*value = fallback
		}
	}
	fill(&o.MaxRooms, defaultMaxRooms)
	fill(&o.MaxPerUser, defaultMaxPerUser)
	fill(&o.SubscriberBuffer, defaultSubscriberBuffer)
	fill(&o.MaxAttempts, defaultMaxAttempts)
	fill(&o.VODCacheEntries, defaultVODCacheEntries)
	fillDuration(&o.RoomIdle, defaultRoomIdle)
	fillDuration(&o.PingInterval, defaultPingInterval)
	fillDuration(&o.ReconnectBase, defaultReconnectBase)
	fillDuration(&o.ReconnectMax, defaultReconnectMax)
	fillDuration(&o.VODCacheTTL, defaultVODCacheTTL)
	return o
}

// Service is the chat of every platform that offers one. Rooms and replay pages are keyed by a caller
// supplied platform key, so two platforms never share a room or a cached page.
type Service struct {
	opts  Options
	hub   hub
	users userLimiter
	vod   *cache.TTL[replayKey, platform.ChatPage]
}

// New builds a Service. It panics without Options.Spawn: a room must always have an owner.
func New(opts Options) *Service {
	if opts.Spawn == nil {
		panic("chat: Options.Spawn is required")
	}
	opts = opts.withDefaults()
	return &Service{
		opts: opts,
		vod: cache.New[replayKey, platform.ChatPage](cache.Options{
			TTL:         opts.VODCacheTTL,
			Max:         opts.VODCacheEntries,
			Spawn:       opts.Spawn,
			LoadTimeout: replayTimeout,
		}),
	}
}

type replayKey struct {
	platform, id, cursor string
	offset               int
}

// Replay answers one page of a video's recorded chat, from a time offset in seconds or, when cursor is
// set, from that cursor (the offset is then ignored). key names the platform source belongs to. Pages are
// cached briefly so seeking back and forth does not hammer the platform; failures are not.
func (s *Service) Replay(ctx context.Context, key string, source platform.Chat, videoID string, offset int, cursor string) (platform.ChatPage, error) {
	if !source.ValidReplayID(videoID) {
		return platform.ChatPage{}, apierr.Invalid("Provide a valid video ID.")
	}
	if cursor != "" && !replayCursor.MatchString(cursor) {
		return platform.ChatPage{}, apierr.Invalid("Provide a valid cursor.")
	}
	if offset < 0 || offset > maxReplayOffset {
		return platform.ChatPage{}, apierr.Invalid("Provide a valid offset in seconds.")
	}
	if cursor != "" {
		offset = 0
	}
	page, err := s.vod.Do(ctx, replayKey{platform: key, id: videoID, cursor: cursor, offset: offset}, func(ctx context.Context) (platform.ChatPage, error) {
		return source.Replay(ctx, videoID, offset, cursor)
	})
	if err != nil {
		return platform.ChatPage{}, apierr.New(502, apierr.CodeRequestFailed, "The chat replay is unavailable.").WithCause(err)
	}
	return page, nil
}
