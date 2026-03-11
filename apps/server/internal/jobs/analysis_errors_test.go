package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series/ocr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// What a failed job stores is one of a few fixed codes, never the text of the error: provider and process
// errors may hold command lines, cookies or URLs.
func TestTaskCodesNeverExposeErrorText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("cookie=secret: %w", context.DeadlineExceeded), "timeout"},
		{fmt.Errorf("token=secret: %w", context.Canceled), "cancelled"},
		{fmt.Errorf("lookup: %w", &ytdlp.Error{Kind: ytdlp.KindRateLimited, Stderr: "HTTP Error 429 token=secret"}), "rate_limited"},
		{fmt.Errorf("token=secret: %w", ocr.ErrImage), "image"},
		{errors.New("command --token secret"), "ocr"},
	} {
		if got := taskCode("ocr")(tc.err); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.err, got, tc.want)
		}
	}
}

// A rate-limited run is tried again after the runner's backoff, or after the fallback when it did not say how
// long; every other failure is left to the queue's own backoff.
func TestRateLimitPauseRetriesOnlyRateLimitedRuns(t *testing.T) {
	t.Parallel()
	pause := rateLimitPause(30 * time.Second)
	if _, retry := pause(errors.New("boom")); retry {
		t.Fatal("an ordinary failure was retried")
	}
	if wait, retry := pause(&ytdlp.Error{Kind: ytdlp.KindRateLimited}); !retry || wait != 30*time.Second {
		t.Fatalf("a rate limit without a delay: %v %v", wait, retry)
	}
	if wait, retry := pause(fmt.Errorf("run: %w", &ytdlp.Error{Kind: ytdlp.KindRateLimited, RetryAfter: time.Minute})); !retry || wait != time.Minute {
		t.Fatalf("a rate limit that names its delay: %v %v", wait, retry)
	}
	if wait, retry := pause(&ytdlp.Error{Kind: ytdlp.KindRateLimited, RetryAfter: time.Second}); !retry || wait != 30*time.Second {
		t.Fatalf("a delay shorter than the fallback: %v %v", wait, retry)
	}
}

// A failure is final when the video or channel is gone or closed, or nothing can ever do the job; any other
// one (the network, a timeout, a rate limit, a platform that answered badly) is tried again.
func TestPermanentFailuresAreTheOnesAskingAgainCannotFix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{apierr.NotFound("Video"), true},
		{apierr.SubscriberOnly(), true},
		{fmt.Errorf("lookup: %w", apierr.RequestFailed().WithCause(&ytdlp.Error{Stderr: "ERROR: [youtube] abc: Video unavailable"})), true},
		{fmt.Errorf("lookup: %w", apierr.RequestFailed().WithCause(&ytdlp.Error{Kind: ytdlp.KindAuthRequired, Stderr: "ERROR: Private video. Sign in if you've been granted access"})), true},
		{errOwnerMismatch, true},
		{errNoReader, true},
		{errors.New("network down"), false},
		{apierr.RequestFailed().WithCause(errors.New("connection reset")), false},
		{apierr.RateLimited().WithCause(&ytdlp.Error{Kind: ytdlp.KindRateLimited, Stderr: "HTTP Error 429 video unavailable"}), false},
		{fmt.Errorf("slow: %w", context.DeadlineExceeded), false},
		{errNotCached, false},
	} {
		if got := permanentFailure(tc.err); got != tc.want {
			t.Errorf("%v: permanent=%v, want %v", tc.err, got, tc.want)
		}
	}
	if got := taskCode("catalog")(apierr.NotFound("Channel")); got != "unavailable" {
		t.Errorf("code of a channel that is gone: %q", got)
	}
}
