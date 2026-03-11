package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series/ocr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// Only fixed codes are stored on jobs: provider and process errors may contain command lines, cookies, URLs
// or other private data. fallback names the failure of the kind of job ("catalog", "metadata", "ocr").
func taskCode(fallback string) func(err error) string {
	return func(err error) string {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return "timeout"
		case errors.Is(err, context.Canceled):
			return "cancelled"
		case errors.Is(err, ytdlp.ErrRateLimited):
			return "rate_limited"
		case errors.Is(err, ocr.ErrImage):
			return "image"
		case gone(err):
			return "unavailable"
		}
		return fallback
	}
}

// gone tells whether the platform said that what was asked for does not exist, was removed, is private, or
// is reserved to subscribers or members.
func gone(err error) bool {
	var known *apierr.Error
	if errors.As(err, &known) {
		switch known.Code {
		case apierr.CodeNotFound, apierr.CodeStreamNotFound, apierr.CodeSubscriberOnly, apierr.CodeInvalidSourceArg, apierr.CodeInvalidRequest:
			return true
		}
	}
	return errors.Is(err, ytdlp.ErrUnavailable)
}

// permanentFailure tells whether asking again cannot succeed: what was asked for is gone (see gone), or what
// came back is not what was asked for, or nothing here can do the job. Every other failure (a network
// error, a timeout, a rate limit, a platform that answered badly) may pass, and the job is tried again later.
func permanentFailure(err error) bool {
	return gone(err) || errors.Is(err, errOwnerMismatch) || errors.Is(err, errNoReader)
}

// rateLimitPause is the Queue.Retry of the jobs that call yt-dlp: a rate-limited run is tried again after
// the runner's shared backoff, or after fallback when it did not say how long it lasts. Every other
// failure is not a matter of the shared backoff (the queue's Backoff decides when it is tried again).
func rateLimitPause(fallback time.Duration) func(err error) (time.Duration, bool) {
	return func(err error) (time.Duration, bool) {
		if !errors.Is(err, ytdlp.ErrRateLimited) {
			return 0, false
		}
		var failed *ytdlp.Error
		if errors.As(err, &failed) && failed.RetryAfter > fallback {
			return failed.RetryAfter, true
		}
		return fallback, true
	}
}

var (
	errUnavailable   = errors.New("jobs: the owner is shutting down")
	errInvalidEntry  = errors.New("invalid channel catalog entry")
	errOwnerMismatch = errors.New("public metadata owner does not match catalog")
	errNotCached     = errors.New("public metadata was not cached")
	errNoReader      = errors.New("thumbnail recognition is unavailable")
)
