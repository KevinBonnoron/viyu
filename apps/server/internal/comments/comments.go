// Package comments serves the comment list of a video: one platform lookup shared by every concurrent
// viewer, cached for hours, bound to the account and session revision of a connected request.
package comments

import (
	"context"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// CookieProvider hands out a private cookie jar of an account's connection for one downloader process.
type CookieProvider interface {
	CookieFile(userID, platformID string) (path string, cleanup func(), err error)
}

// Key identifies one cached comment list. Comments are public when no source is selected; a connected
// list stays scoped to the account and the session revision it was fetched with. Cookies are never stored.
type Key struct {
	Platform, VideoID, UserID, Revision string
}

// Options configures a Service.
type Options struct {
	Platforms *platform.Registry
	Cookies   CookieProvider
	// Spawn runs the detached lookups; it is the owner's lifetime.
	Spawn cache.Spawner
	// TTL is how long a list is served from memory; default six hours.
	TTL time.Duration
	// Max bounds the lists kept; default 512 (the one closest to expiry leaves first).
	Max int
	// FetchTimeout bounds one platform lookup; default 60 seconds.
	FetchTimeout time.Duration
}

// Service is the cached comment lookup.
type Service struct {
	platforms *platform.Registry
	cookies   CookieProvider
	lists     *cache.TTL[Key, []platform.Comment]
}

// New builds a Service.
func New(opts Options) *Service {
	if opts.TTL <= 0 {
		opts.TTL = 6 * time.Hour
	}
	if opts.Max <= 0 {
		opts.Max = 512
	}
	if opts.FetchTimeout <= 0 {
		opts.FetchTimeout = 60 * time.Second
	}
	return &Service{
		platforms: opts.Platforms,
		cookies:   opts.Cookies,
		lists:     cache.New[Key, []platform.Comment](cache.Options{TTL: opts.TTL, Max: opts.Max, Spawn: opts.Spawn, LoadTimeout: opts.FetchTimeout}),
	}
}

// List answers from memory or fetches the list once for every concurrent viewer of the same key. The fetch is
// detached from the requests (see cache.Flight): a viewer that leaves does not fail the others, and a
// connected key makes its own cookie jar rather than borrowing the first request's, which ends with that
// request.
func (s *Service) List(ctx context.Context, key Key) ([]platform.Comment, error) {
	p, ok := s.platforms.Get(key.Platform)
	if !ok {
		return nil, apierr.InvalidInput("Unknown source.")
	}
	return s.lists.Do(ctx, key, func(ctx context.Context) ([]platform.Comment, error) {
		cookieFile := ""
		if key.UserID != "" {
			path, cleanup, err := s.cookies.CookieFile(key.UserID, key.Platform)
			if err != nil {
				return nil, err
			}
			defer cleanup()
			cookieFile = path
		}
		return p.Comments(ctx, key.VideoID, cookieFile)
	})
}

// Invalidate forgets the lists fetched with an account's connection, when it is removed.
func (s *Service) Invalidate(userID, platformID string) {
	s.lists.DeleteFunc(func(key Key) bool { return key.UserID == userID && key.Platform == platformID })
}

// Purge drops the lists that expired.
func (s *Service) Purge() { s.lists.Purge() }
