package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// The helpers of the library_* tests: the routes of the accounts' sources, subscriptions, notifications,
// feeds, statistics and activity. Their names start with lib so that no sibling suite collides with them.

// libApp is newApp with the background analysis held back: subscribing a channel wakes it, and a test about
// a library route must not have the catalog of the channel synchronized (and the downloader started) behind
// its back. A test that wants the analysis passes its own mutator after this one.
func libApp(t *testing.T, mutate ...func(*Options)) (core.App, http.Handler) {
	t.Helper()
	quiet := func(o *Options) { o.Jobs.Analysis.Debounce = time.Hour }
	app, handler := newApp(t, append([]func(*Options){quiet}, mutate...)...)
	return app, handler
}

// libDownloaderFailing is a downloader that always fails: for the tests whose routes must not need one, so a
// real yt-dlp on the PATH is never started by accident.
func libDownloaderFailing(t *testing.T) func(*Options) {
	t.Helper()
	return withDownloader(t, "echo 'no downloader in this test' >&2; exit 1")
}

// libDo sends a request with a JSON body (nil for none) as the holder of token.
func libDo(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload := ""
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = string(data)
	}
	return send(t, h, method, path, token, payload)
}

// libFail asserts the status of an answer and, when code is set, that its body is the error envelope with
// that code (and a message, and nothing that looks like a private value).
func libFail(t *testing.T, res *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status=%d want=%d body=%s", res.Code, status, res.Body)
	}
	if code == "" {
		return
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["code"] != code || body["message"] == "" || len(body) != 2 {
		t.Fatalf("not the {code,message} envelope for %s: %s (%v)", code, res.Body, err)
	}
	for _, secret := range []string{"private-cookie-value", "viyu-source-secret"} {
		if strings.Contains(res.Body.String(), secret) {
			t.Fatalf("an error leaked private downloader data: %s", res.Body)
		}
	}
}

// libOK asserts a 200 answer and decodes its JSON into T.
func libOK[T any](t *testing.T, res *httptest.ResponseRecorder) T {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body)
	}
	var value T
	if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
		t.Fatalf("%v: %s", err, res.Body)
	}
	return value
}

// libProfile creates another profile of the account.
func libProfile(t *testing.T, app core.App, user *core.Record, name string) *core.Record {
	t.Helper()
	return libRecord(t, app, "profiles", map[string]any{"user": user.Id, "name": name})
}

// libRecord saves a record of the collection.
func libRecord(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	schema, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// libSubscribe subscribes the profile to a channel stored the way a listing stores it (its listing is
// fresh for a day unless the test expires it) and returns the subscription.
func libSubscribe(t *testing.T, app core.App, profile *core.Record, platformID, nativeID, name string) *core.Record {
	t.Helper()
	channel := storedChannel(t, app, platformID, nativeID, name)
	return libRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel})
}

// libChannelOf returns the channel record a subscription follows.
func libChannelOf(t *testing.T, app core.App, subscription *core.Record) *core.Record {
	t.Helper()
	channel, err := app.FindRecordById("channels", subscription.GetString("channel"))
	if err != nil {
		t.Fatal(err)
	}
	return channel
}

// libCreatorOf returns the creator of a subscription.
func libCreatorOf(t *testing.T, app core.App, subscriptionID string) string {
	t.Helper()
	record, err := app.FindRecordById("subscriptions", subscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("creator")
}

// libLink stores a creator of the profile and puts the subscriptions in it.
func libLink(t *testing.T, app core.App, profile *core.Record, name string, subscriptions ...*core.Record) string {
	t.Helper()
	creator := acctRecord(t, app, "creators", map[string]any{"profile": profile.Id, "name": name})
	for _, subscription := range subscriptions {
		subscription.Set("creator", creator.Id)
		if err := app.Save(subscription); err != nil {
			t.Fatal(err)
		}
	}
	return creator.Id
}

// libFresh gives a channel a listing that stays current for a long time.
func libFresh(t *testing.T, app core.App, channel *core.Record) {
	t.Helper()
	channel.Set("expires_at", "2099-01-01 00:00:00.000Z")
	if err := app.Save(channel); err != nil {
		t.Fatal(err)
	}
}

// libExpire makes a channel's listing stale.
func libExpire(t *testing.T, app core.App, channel *core.Record) {
	t.Helper()
	channel.Set("expires_at", time.Now().Add(-time.Hour))
	if err := app.Save(channel); err != nil {
		t.Fatal(err)
	}
}

// libVideoIDs lists the record ids of videos an API answered.
func libVideoIDs(videos []platform.Video) []string {
	ids := make([]string, 0, len(videos))
	for _, video := range videos {
		ids = append(ids, video.ID)
	}
	return ids
}

// libCookies is a Netscape cookie export holding a YouTube session (secret is its value) and one unrelated
// cookie the application must drop. expiry 0 is a session cookie.
func libCookies(secret string, expiry int64) string {
	return fmt.Sprintf("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t%d\tSAPISID\t%s\n#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t%d\tLOGIN_INFO\tlogin-%s\n.example.net\tTRUE\t/\tTRUE\t%d\tunrelated\tother-secret\n", expiry, secret, expiry, secret, expiry)
}

// libDownloader is a yt-dlp stand-in for the connected-source routes. It insists on a private cookie jar
// holding only YouTube cookies, records what it was asked (never the cookies) and plays the failure named in
// its mode file, which a test changes at any time: the state lives next to the script, so applications built
// side by side never share it.
type libDownloader struct{ dir, binary string }

func newLibDownloader(t *testing.T) *libDownloader {
	t.Helper()
	binary := testutil.Script(t, "yt-dlp", `dir=$(dirname "$0")
mode=$(cat "$dir/mode" 2>/dev/null)
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
printf '%s\n' "$*" >> "$dir/arguments"
printf '%s\n' "$cookies" >> "$dir/cookie-paths"
case "$mode" in
  auth_error) echo 'WARNING: The provided YouTube account cookies are no longer valid: private-cookie-value /tmp/viyu-source-secret' >&2 ;;
  failure) echo 'ERROR: remote failure private-cookie-value /tmp/viyu-source-secret' >&2; exit 7 ;;
esac
if [ "$single" = yes ]; then
  printf '%s\n' '{"_type":"playlist","id":"subscriptions","extractor_key":"YoutubeTab","extractor":"youtube:tab","entries":[]}'
  exit 0
fi
if [ "$mark" = yes ]; then
  id="${url#https://www.youtube.com/watch?v=}"
  printf '{"id":"%s","title":"Marked video","duration":120}\n' "$id"
  exit 0
fi
printf '%s\n' '{"id":"abcdefghijk","title":"First viewing","duration":120}' '{"id":"abcdefghijk","title":"Repeated viewing","duration":120}' '{"id":"lmnopqrstuv","title":"Third viewing","duration":60}'`)
	return &libDownloader{dir: filepath.Dir(binary), binary: binary}
}

// option points an application at the downloader.
func (d *libDownloader) option() func(*Options) {
	return func(o *Options) { o.YTDLP = ytdlp.Options{Binary: d.binary} }
}

// mode makes the following runs play the named failure ("" for none).
func (d *libDownloader) mode(t *testing.T, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d.dir, "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (d *libDownloader) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(d.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// arguments is every argument line the downloader was started with.
func (d *libDownloader) arguments(t *testing.T) string { return d.read(t, "arguments") }

// assertJarsRemoved checks that each process had a cookie jar of its own and that none outlived it.
func (d *libDownloader) assertJarsRemoved(t *testing.T) {
	t.Helper()
	seen := map[string]bool{}
	for _, path := range strings.Fields(d.read(t, "cookie-paths")) {
		if seen[path] {
			t.Fatalf("cookie path reused between subprocesses: %s", path)
		}
		seen[path] = true
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cookie file not removed after subprocess: %v", err)
		}
	}
}

// libSources decodes the catalog of the sources an account sees, by platform.
func libSources(t *testing.T, handler http.Handler, token string) map[string]libSource {
	t.Helper()
	res := request(t, handler, "/api/sources", token)
	byPlatform := map[string]libSource{}
	for _, source := range libOK[[]libSource](t, res) {
		byPlatform[source.Platform] = source
	}
	return byPlatform
}

// libSource is a source as the client reads it.
type libSource struct {
	Platform     string `json:"platform"`
	Name         string `json:"name"`
	Connected    bool   `json:"connected"`
	Enabled      bool   `json:"enabled"`
	ConnectionID string `json:"connectionId"`
	Label        string `json:"label"`
}

// libActivate sets the account's choice for a source and returns the answer.
func libActivate(t *testing.T, handler http.Handler, token, platformID string, enabled bool) *httptest.ResponseRecorder {
	t.Helper()
	return libDo(t, handler, http.MethodPut, "/api/sources/"+platformID+"/activation", token, map[string]bool{"enabled": enabled})
}

// libChannelID is a YouTube channel id of the real shape ("UC" and 22 characters) built from seed, which
// must hold only letters, digits, '_' and '-': subscribing validates the shape with the platform.
func libChannelID(seed string) string { return "UC" + (seed + strings.Repeat("0", 22))[:22] }

// libListing is a yt-dlp stand-in that lists channels: every channel it is asked about has one video of its
// own. Its mode, which a test changes at any time, is "" (answer), "fail" (fail) or "hold" (wait until the
// mode changes, then behave as the new mode says). It refuses the catalog listings (nothing in the library
// tests is about the catalogs) and counts the channel listings it was started for.
type libListing struct{ dir, binary string }

func newLibListing(t *testing.T) *libListing {
	t.Helper()
	binary := testutil.Script(t, "yt-dlp", `dir=$(dirname "$0")
case "$*" in *--lazy-playlist*) exit 1 ;; *ytsearch*|*results?search_query*) cat "$dir/card" 2>/dev/null; exit 0 ;; esac
echo listing >> "$dir/listings"
while [ "$(cat "$dir/mode" 2>/dev/null)" = hold ]; do sleep 0.02; done
if [ "$(cat "$dir/mode" 2>/dev/null)" = fail ]; then echo 'ERROR: listing failed' >&2; exit 1; fi
url=''
for arg do case "$arg" in https://*) url="$arg" ;; esac; done
id="${url#https://www.youtube.com/channel/}"; id="${id%/videos}"
video="v$(printf %s "${id#UC}" | cut -c1-10)"
printf '{"_type":"playlist","id":"%s","channel_id":"%s","channel":"Listed %s","title":"Listed","entries":[{"id":"%s","title":"Video of %s","duration":60,"upload_date":"20260102"}]}\n' "$id" "$id" "$id" "$video" "$id"`)
	return &libListing{dir: filepath.Dir(binary), binary: binary}
}

func (l *libListing) option() func(*Options) { return libBinary(l.binary) }

// card makes the searches answer with the JSON line of one search hit.
func (l *libListing) card(t *testing.T, line string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.dir, "card"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// mode changes what the following (and the waiting) listings do.
func (l *libListing) mode(t *testing.T, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.dir, "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
}

// listings is how many channel listings were started.
func (l *libListing) listings() int {
	data, err := os.ReadFile(filepath.Join(l.dir, "listings"))
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "listing")
}

func libBinary(binary string) func(*Options) {
	return func(o *Options) { o.YTDLP.Binary = binary }
}
