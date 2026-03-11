package youtube

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

const botCheck = `echo "ERROR: [youtube] abc: Sign in to confirm you're not a bot /tmp/private-cookies" >&2; exit 1`

func TestALoginDemandIsTheSessionsFailureOnlyForAConnectedRequest(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, botCheck)
	_, err := p.VideoMetadata(context.Background(), "dQw4w9WgXcQ", "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
	_, err = p.VideoMetadata(context.Background(), "dQw4w9WgXcQ", "/private/jar.txt")
	assertCode(t, err, "SOURCE_SESSION_INVALID")
	if !errors.Is(err, ytdlp.ErrAuthRequired) {
		t.Fatalf("the cause must stay attached for the log: %v", err)
	}
}

func TestRateLimitIsReportedAsSuchAndKeepsItsCause(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, `echo 'ERROR: unable to download video data: HTTP Error 429: Too Many Requests' >&2; exit 1`)
	_, err := p.PlaybackManifest(context.Background(), "dQw4w9WgXcQ", "")
	assertCode(t, err, "SOURCE_RATE_LIMITED")
	if !errors.Is(err, ytdlp.ErrRateLimited) {
		t.Fatalf("the cause must stay attached for the log: %v", err)
	}
	before := calls(t, log)
	_, err = p.VideoMetadata(context.Background(), "dQw4w9WgXcQ", "")
	assertCode(t, err, "SOURCE_RATE_LIMITED")
	if calls(t, log) != before {
		t.Fatal("yt-dlp was started during the shared backoff")
	}
}

func TestCancellationIsReturnedAsIs(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := p.LiveStream(ctx, "UCabcdefghijklmnopqrstuv")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	var safe *apierr.Error
	if errors.As(err, &safe) {
		t.Fatalf("a cancellation is not a platform failure: %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("yt-dlp did not stop with its context")
	}
}

func TestEveryOtherFailureIsAGenericSafeErrorWithItsCause(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, `echo 'ERROR: secret-cookie /tmp/private-cookies exploded' >&2; exit 3`)
	_, _, err := p.ChannelListing(context.Background(), "UCfixture", 5, "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
	var failed *ytdlp.Error
	if !errors.As(err, &failed) || failed.Stderr == "" {
		t.Fatalf("the downloader's error is the cause: %v", err)
	}
}
