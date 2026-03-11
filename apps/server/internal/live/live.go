// Package live answers whether a channel is broadcasting now. Lookups are cached for a minute and shared by
// every concurrent caller of a channel; a stream found is stored like any video, and one found over is
// turned back into an ordinary video.
package live

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Options configures a Service.
type Options struct {
	App       core.App
	Platforms *platform.Registry
	Store     *catalog.Store
	// Spawn owns the detached lookups: their lifetime is the owner's.
	Spawn cache.Spawner
	Log   *slog.Logger
	// TTL bounds how stale a channel's live status can be, and how often one channel is looked up; default a minute.
	TTL time.Duration
	// Max bounds the statuses kept; default 256.
	Max int
	// LookupTimeout bounds one platform lookup; default 60 seconds.
	LookupTimeout time.Duration
	// Concurrency bounds the channels looked up at once by Subscribed: on YouTube each lookup is a process; default 4.
	Concurrency int
}

// Service is the live status lookup.
type Service struct {
	app         core.App
	platforms   *platform.Registry
	store       *catalog.Store
	log         *slog.Logger
	concurrency int
	statuses    *cache.TTL[string, platform.LiveStream]
}

// New builds a Service.
func New(opts Options) *Service {
	if opts.TTL <= 0 {
		opts.TTL = time.Minute
	}
	if opts.Max <= 0 {
		opts.Max = 256
	}
	if opts.LookupTimeout <= 0 {
		opts.LookupTimeout = 60 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		app: opts.App, platforms: opts.Platforms, store: opts.Store, log: opts.Log, concurrency: opts.Concurrency,
		statuses: cache.New[string, platform.LiveStream](cache.Options{TTL: opts.TTL, Max: opts.Max, Spawn: opts.Spawn, LoadTimeout: opts.LookupTimeout}),
	}
}

// Lookup answers from the cache or asks the platform, once for all concurrent callers of a channel. A failed
// lookup counts as "not live" and is not cached, so the next call tries again. The lookup is detached from
// the callers (see cache.Flight): one caller leaving does not fail the others, and a cancelled caller gets
// "not live" and caches nothing.
func (s *Service) Lookup(ctx context.Context, platformID, channelID string) platform.LiveStream {
	p, ok := s.platforms.Get(platformID)
	if !ok {
		return platform.LiveStream{}
	}
	key := platformID + ":" + strings.ToLower(strings.TrimPrefix(channelID, "@"))
	stream, err := s.statuses.Do(ctx, key, func(ctx context.Context) (platform.LiveStream, error) {
		return s.refresh(ctx, p, channelID)
	})
	if err != nil {
		return platform.LiveStream{}
	}
	return stream
}

// refresh asks the platform and stores what it learned. A failure is returned so that nothing is cached.
func (s *Service) refresh(ctx context.Context, p platform.Platform, channelID string) (platform.LiveStream, error) {
	stream, err := p.LiveStream(ctx, channelID)
	if ctx.Err() != nil {
		return platform.LiveStream{}, ctx.Err()
	}
	if err != nil {
		s.log.Warn("Failed to look up a channel's live status", "platform", p.ID(), "id", channelID, "error", err)
		return platform.LiveStream{}, err
	}
	if !stream.Live {
		// A video's live flag is only written when its full metadata is fetched, so without this a stream
		// that ended would keep its "live" badge in every listing for good.
		if err := s.store.EndLive(p.ID(), p.EndLive(channelID)); err != nil {
			s.log.Warn("Failed to end the streams of a channel that is not live", "platform", p.ID(), "id", channelID, "error", err)
		}
		return stream, nil
	}
	if stream.VideoID != "" {
		// A live stream is a video like any other once stored: the player addresses it by record id.
		video := platform.Video{ID: stream.VideoID, Platform: p.ID(), Title: stream.Title, Live: true, ChannelID: channelID, Category: stream.Category, Thumbnail: stream.Thumbnail, ViewCount: stream.Viewers}
		if channel, err := s.store.FindChannel(p.ID(), channelID); err == nil {
			video.ChannelName = channel.GetString("name")
			video.ChannelRecordID = channel.Id
		}
		if err := s.store.StoreVideo(&video, ""); err != nil {
			// A stream that cannot be addressed by record id is not shown: no client may be handed a native id.
			s.log.Warn("Failed to store a live stream", "platform", p.ID(), "id", channelID, "error", err)
			return platform.LiveStream{}, err
		}
		stream.RecordID = video.RecordID
	}
	return stream, nil
}

// Stream is one channel that is broadcasting now.
type Stream struct {
	Platform string `json:"platform"`
	// ChannelID is the PocketBase id of the channel record.
	ChannelID string `json:"channelId"`
	platform.LiveStream
	Thumbnail string `json:"thumbnail"`
}

// Subscribed looks up the channels, a few at a time, and returns those broadcasting now, most watched first.
// Every lookup goes through the shared cache, so a page can ask again every couple of minutes without
// hitting the platforms each time. It returns ctx.Err() when the context ends first.
func (s *Service) Subscribed(ctx context.Context, channels []*core.Record) ([]Stream, error) {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		slots   = make(chan struct{}, s.concurrency)
		results = []Stream{}
	)
	for _, channel := range channels {
		platformID, id := channel.GetString("platform"), channel.GetString("platform_id")
		p, ok := s.platforms.Get(platformID)
		if !ok || !p.ValidLiveChannel(id) {
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			if stream := s.Lookup(ctx, platformID, id); stream.Live && stream.RecordID != "" {
				mu.Lock()
				results = append(results, Stream{Platform: platformID, ChannelID: channel.Id, LiveStream: stream, Thumbnail: p.LiveThumbnail(stream)})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Viewers != results[j].Viewers {
			return results[i].Viewers > results[j].Viewers
		}
		return results[i].Platform+results[i].ChannelID < results[j].Platform+results[j].ChannelID
	})
	return results, nil
}

// Reconcile checks the channels that still have a video flagged live: the lookup (cached for a minute)
// clears the flag of those that are not live any more. Channels without a flagged video cost one query and
// no request to the platform.
func (s *Service) Reconcile(ctx context.Context, channels []*core.Record) {
	for _, channel := range channels {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.app.FindFirstRecordByFilter("videos", "channel = {:channel} && live = true", dbx.Params{"channel": channel.Id}); err != nil {
			continue
		}
		s.Lookup(ctx, channel.GetString("platform"), channel.GetString("platform_id"))
	}
}

// Purge drops the statuses that expired.
func (s *Service) Purge() { s.statuses.Purge() }
