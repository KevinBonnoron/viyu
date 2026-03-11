package youtube

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func testCookies(secret string, expiry int64) string {
	return fmt.Sprintf("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t%d\tSAPISID\t%s\n#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t%d\tLOGIN_INFO\tlogin-%s\n.example.net\tTRUE\t/\tTRUE\t%d\tunrelated\tother-secret\n", expiry, secret, expiry, secret, expiry)
}

// sessionScript is a yt-dlp that enforces a private per-process cookie jar and rejects non-YouTube cookies.
// It records the path of every jar it received in $paths, and misbehaves as the mode says.
func sessionScript(mode, paths string) string {
	return `
mode='` + mode + `'
paths='` + paths + `'
cookies=''
single=''
mark=''
previous=''
url=''
for arg do
  if [ "$previous" = '--cookies' ]; then cookies="$arg"; fi
  case "$arg" in
    --dump-single-json) single=yes ;;
    --mark-watched) mark=yes ;;
    --no-mark-watched) mark='' ;;
    https://*) url="$arg" ;;
  esac
  previous="$arg"
done
if [ -z "$cookies" ] || [ ! -f "$cookies" ] || [ "$(stat -c %a "$cookies")" != 600 ]; then
  echo 'fixture expected a private cookie file' >&2; exit 11
fi
case "$(cat "$cookies")" in *example.net*|*other-secret*) echo 'fixture received unrelated cookies' >&2; exit 12 ;; esac
printf '%s\n' "$cookies" >> "$paths"
case "$mode" in
  auth_error) echo 'WARNING: The provided YouTube account cookies are no longer valid: private-cookie-value /tmp/viyu-source-secret' >&2 ;;
  auth_failure) echo 'ERROR: This page requires authentication; please sign in: private-cookie-value' >&2; exit 1 ;;
  failure) echo 'ERROR: remote failure private-cookie-value /tmp/viyu-source-secret' >&2; exit 7 ;;
  rate_limited) echo 'ERROR: unable to download webpage: HTTP Error 429: Too Many Requests' >&2; exit 1 ;;
  malformed) printf '{broken'; exit 0 ;;
  no_output) exit 0 ;;
  trailing) printf '%s\n' '{"_type":"playlist","id":"subscriptions","extractor_key":"YoutubeTab","entries":[]}' 'not-json'; exit 0 ;;
  wrong_wrapper) printf '%s\n' '{"_type":"playlist","id":"recommended","extractor_key":"YoutubeTab","entries":[]}'; exit 0 ;;
  null_entries) printf '%s\n' '{"_type":"playlist","id":"subscriptions","extractor_key":"YoutubeTab","entries":null}'; exit 0 ;;
  bad_entry) printf '%s\n' '{"_type":"playlist","id":"subscriptions","extractor_key":"YoutubeTab","entries":[{"id":"x"}]}'; exit 0 ;;
  cancelled) sleep 30; exit 0 ;;
esac
if [ "$single" = yes ]; then
  printf '%s\n' '{"_type":"playlist","id":"subscriptions","extractor_key":"YoutubeTab","extractor":"youtube:tab","entries":[]}'
  exit 0
fi
if [ "$mark" = yes ]; then
  case "$mode" in
    mark_failure) echo 'WARNING: Unable to mark fully watched: private-cookie-value' >&2 ;;
    other_video) printf '{"id":"zzzzzzzzzzz","title":"Another video"}\n'; exit 0 ;;
  esac
  id="${url#https://www.youtube.com/watch?v=}"
  printf '{"id":"%s","title":"Marked video","duration":120}\n' "$id"
  exit 0
fi
case "$mode" in
  empty) exit 0 ;;
  malformed_feed) printf '%s\n' '{"id":"abcdefghijk","title":"First"}' 'malformed'; exit 0 ;;
  invalid_entry) printf '%s\n' '{"id":"not-a-video-id-at-all","title":"First"}'; exit 0 ;;
  unauthenticated_feed) echo 'WARNING: You are not logged in: private-cookie-value' >&2; exit 0 ;;
esac
printf '%s\n' '{"id":"abcdefghijk","title":"First viewing","duration":120}' '{"id":"abcdefghijk","title":"Repeated viewing","duration":120}' '{"id":"lmnopqrstuv","title":"Third viewing","duration":60}'
`
}

// session is a platform whose yt-dlp behaves as mode says; cleanup asserts every cookie jar it was given
// was private to its run and is gone.
func session(t *testing.T, mode string) (platform.Connector, string, func()) {
	t.Helper()
	paths := filepath.Join(t.TempDir(), "cookie-paths")
	p, log := fakeYTDLP(t, sessionScript(mode, paths))
	check := func() {
		t.Helper()
		data, err := os.ReadFile(paths)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, path := range strings.Fields(string(data)) {
			if seen[path] {
				t.Fatalf("cookie path reused between subprocesses: %s", path)
			}
			seen[path] = true
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cookie file not removed after subprocess: %v", err)
			}
		}
	}
	return p.Connector(), log, check
}

func normalized(t *testing.T, connector platform.Connector, secret string, expiry int64) string {
	t.Helper()
	cookies, err := connector.NormalizeCookies(testCookies(secret, expiry))
	if err != nil {
		t.Fatal(err)
	}
	return cookies
}

func TestCookieImportValidatesAuthSyntaxDomainsAndExpiry(t *testing.T) {
	t.Parallel()
	connector := bare().Connector()
	valid := testCookies("private-cookie-value", time.Now().Add(time.Hour).Unix())
	cookies, err := connector.NormalizeCookies(valid)
	if err != nil || !strings.Contains(cookies, "#HttpOnly_.youtube.com") || strings.Contains(cookies, "example.net") || strings.Contains(cookies, "other-secret") {
		t.Fatalf("valid import was not normalized safely: %v", err)
	}
	if _, err := connector.NormalizeCookies(strings.ReplaceAll(valid, "\n", "\r\n")); err != nil {
		t.Fatalf("CRLF cookie export rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		raw  string
		code string
	}{
		{"empty", "", "INVALID_SOURCE_INPUT"},
		{"oversized", strings.Repeat("x", platform.CookieLimit+1), "INVALID_SOURCE_INPUT"},
		{"json", `[{"name":"SAPISID","value":"private-cookie-value"}]`, "INVALID_SOURCE_INPUT"},
		{"invalid-row", "# Netscape HTTP Cookie File\n.youtube.com\tnot-seven-fields\n", "INVALID_SOURCE_INPUT"},
		{"invalid-expiry", strings.Replace(valid, "\tTRUE\t", "\tinvalid\t", 1), "INVALID_SOURCE_INPUT"},
		{"expired", testCookies("expired", time.Now().Add(-time.Hour).Unix()), "SOURCE_SESSION_INVALID"},
		{"foreign-domain", strings.ReplaceAll(valid, "youtube.com", "youtube.com.evil.test"), "INVALID_SOURCE_INPUT"},
		{"missing-login", strings.ReplaceAll(valid, "LOGIN_INFO", "PREF"), "INVALID_SOURCE_INPUT"},
		{"missing-auth-header", strings.ReplaceAll(valid, "SAPISID", "PREF"), "INVALID_SOURCE_INPUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := connector.NormalizeCookies(tc.raw)
			assertCode(t, err, tc.code)
		})
	}
}

func TestAuthenticatedEmptyFeedsAndStrictFailures(t *testing.T) {
	t.Parallel()
	connector, log, cleanedUp := session(t, "")
	cookies := normalized(t, connector, "private-cookie-value", 0)
	// A successful empty wrapper validation must work for a newly connected account; empty NDJSON alone
	// is never the authentication proof.
	if err := connector.Validate(context.Background(), cookies); err != nil {
		t.Fatalf("authenticated empty subscription feed rejected: %v", err)
	}
	if args := calls(t, log); strings.Contains(args, " --mark-watched ") || !strings.Contains(args, "--dump-single-json --flat-playlist --skip-download --no-mark-watched --playlist-end 1 https://www.youtube.com/feed/subscriptions") {
		t.Fatalf("validation could mutate history or ignored required flags: %s", args)
	}
	cleanedUp()

	empty, emptyLog, emptyCleanup := session(t, "empty")
	feed, err := empty.Feed(context.Background(), cookies, "home", 1, 30)
	// "empty" mode answers the wrapper too (single-json), so the feed is the plain NDJSON that is empty.
	if err != nil || feed.Videos == nil || len(feed.Videos) != 0 || feed.HasMore {
		t.Fatalf("authenticated empty home feed: %+v %v", feed, err)
	}
	if args := calls(t, emptyLog); strings.Contains(args, " --mark-watched ") || !strings.Contains(args, "--dump-json --flat-playlist --skip-download --no-mark-watched --playlist-start 1 --playlist-end 31 https://www.youtube.com/feed/recommended") {
		t.Fatalf("feed extraction could mutate history or ignored required flags: %s", args)
	}
	emptyCleanup()

	for _, tc := range []struct{ mode, code string }{
		{"no_output", "SOURCE_REQUEST_FAILED"},
		{"malformed", "SOURCE_REQUEST_FAILED"},
		{"trailing", "SOURCE_REQUEST_FAILED"},
		{"wrong_wrapper", "SOURCE_REQUEST_FAILED"},
		{"null_entries", "SOURCE_REQUEST_FAILED"},
		{"bad_entry", "SOURCE_REQUEST_FAILED"},
		{"malformed_feed", "SOURCE_REQUEST_FAILED"},
		{"invalid_entry", "SOURCE_REQUEST_FAILED"},
		{"auth_error", "SOURCE_SESSION_INVALID"},
		{"auth_failure", "SOURCE_SESSION_INVALID"},
		{"unauthenticated_feed", "SOURCE_SESSION_INVALID"},
		{"failure", "SOURCE_REQUEST_FAILED"},
		{"rate_limited", "SOURCE_RATE_LIMITED"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			connector, _, cleanedUp := session(t, tc.mode)
			_, err := connector.Feed(context.Background(), cookies, "subscriptions", 1, 30)
			assertCode(t, err, tc.code)
			cleanedUp()
		})
	}
}

func TestFeedPagingPreservesHistoryEntries(t *testing.T) {
	t.Parallel()
	connector, log, cleanedUp := session(t, "")
	cookies := normalized(t, connector, "private-cookie-value", 0)
	feed, err := connector.Feed(context.Background(), cookies, "history", 2, 2)
	if err != nil || feed.Page != 2 || !feed.HasMore || len(feed.Videos) != 2 {
		t.Fatalf("feed page: %+v %v", feed, err)
	}
	if feed.Videos[0].ID != feed.Videos[1].ID || feed.Videos[0].Title != "First viewing" || feed.Videos[1].Title != "Repeated viewing" || feed.Videos[0].Source != "youtube" {
		t.Fatalf("history was deduplicated, reordered or lost its source: %+v", feed)
	}
	if args := calls(t, log); !strings.Contains(args, "--playlist-start 3 --playlist-end 5 https://www.youtube.com/feed/history") {
		t.Fatalf("pagination did not request one extra item: %s", args)
	}
	cleanedUp()
}

func TestFeedRefusesBadRequestsWithoutRunningAnything(t *testing.T) {
	t.Parallel()
	connector, log, _ := session(t, "")
	cookies := normalized(t, connector, "private-cookie-value", 0)
	for _, values := range [][2]int{{0, 30}, {1, 0}, {1, 51}, {-1, 30}, {math.MaxInt, 50}, {platform.MaxListPage + 1, 30}} {
		_, err := connector.Feed(context.Background(), cookies, "home", values[0], values[1])
		assertCode(t, err, "INVALID_SOURCE_INPUT")
	}
	_, err := connector.Feed(context.Background(), cookies, "playlists", 1, 30)
	assertCode(t, err, "INVALID_SOURCE_INPUT")
	if got := calls(t, log); got != "" {
		t.Fatalf("a refused feed started yt-dlp: %s", got)
	}
}

func TestMarkWatchedMarksExplicitlyAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	connector, log, cleanedUp := session(t, "")
	cookies := normalized(t, connector, "private-cookie-value", 0)
	if err := connector.MarkWatched(context.Background(), cookies, "abcdefghijk"); err != nil {
		t.Fatal(err)
	}
	if args := calls(t, log); !strings.Contains(args, "--mark-watched --simulate https://www.youtube.com/watch?v=abcdefghijk") {
		t.Fatalf("genuine playback did not explicitly mark viewed: %s", args)
	}
	cleanedUp()
	for _, tc := range []struct{ mode, code string }{
		{"mark_failure", "SOURCE_REQUEST_FAILED"},
		{"auth_error", "SOURCE_SESSION_INVALID"},
		{"auth_failure", "SOURCE_SESSION_INVALID"},
		{"other_video", "SOURCE_REQUEST_FAILED"},
		{"malformed", "SOURCE_REQUEST_FAILED"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			connector, _, cleanedUp := session(t, tc.mode)
			assertCode(t, connector.MarkWatched(context.Background(), cookies, "abcdefghijk"), tc.code)
			cleanedUp()
		})
	}
}

func TestMarkWatchedRefusesAnInvalidVideoWithoutRunningAnything(t *testing.T) {
	t.Parallel()
	connector, log, _ := session(t, "")
	err := connector.MarkWatched(context.Background(), normalized(t, connector, "private-cookie-value", 0), "video&injected=1")
	assertCode(t, err, "INVALID_SOURCE_INPUT")
	if got := calls(t, log); got != "" {
		t.Fatalf("an invalid video started yt-dlp: %s", got)
	}
}

func TestSessionRunStopsWithItsContextAndRemovesTheCookies(t *testing.T) {
	t.Parallel()
	connector, _, cleanedUp := session(t, "cancelled")
	cookies := normalized(t, connector, "private-cookie-value", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := connector.Validate(ctx, cookies)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a cancelled request returns the context's error as it is: %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("the downloader did not stop with the request")
	}
	cleanedUp()
}

func TestRateLimitedRunsShareTheBackoff(t *testing.T) {
	t.Parallel()
	connector, log, _ := session(t, "rate_limited")
	cookies := normalized(t, connector, "private-cookie-value", 0)
	assertCode(t, connector.Validate(context.Background(), cookies), "SOURCE_RATE_LIMITED")
	before := calls(t, log)
	assertCode(t, connector.Validate(context.Background(), cookies), "SOURCE_RATE_LIMITED")
	if calls(t, log) != before {
		t.Fatal("yt-dlp was started again during the backoff")
	}
}
