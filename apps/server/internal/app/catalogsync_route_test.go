package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// csyncScript is a yt-dlp stand-in that lists the whole catalog of a channel the way a lazy extraction does.
// state is a directory the test shares with it: the script appends the channel of every catalog extraction to
// "started" and the cookie jar of every run as a connected account to "cookie-paths"; it holds the end of the
// catalog back until a file "release" exists, and fails when a file "fail" exists (what a failure says on
// stderr must never reach a client). "connected-prefix" names the videos a connected run lists, a run
// without a jar lists "public-N": the first five videos come at once, the rest after the release, 65 in all
// (one of them repeated).
func csyncScript(state string) string {
	return fmt.Sprintf(`state='%s'
cookie_file=
previous=
channel=UCcatalog
for argument in "$@"; do
  if [ "$previous" = --cookies ]; then cookie_file=$argument; fi
  case "$argument" in
    https://www.youtube.com/channel/*/videos) channel=${argument#https://www.youtube.com/channel/}; channel=${channel%%/videos} ;;
  esac
  previous=$argument
done
case " $* " in
  *feed/subscriptions*)
    printf '%%s\n' '{"_type":"playlist","id":"subscriptions","extractor_key":"YoutubeTab","entries":[]}'
    exit 0 ;;
esac
prefix=public
if [ -n "$cookie_file" ]; then
  test -r "$cookie_file"
  printf '%%s\n' "$cookie_file" >> "$state/cookie-paths"
  if [ -f "$state/connected-prefix" ]; then prefix=$(cat "$state/connected-prefix"); else prefix=connected; fi
fi
case " $* " in
  *' --dump-single-json '*)
    printf '%%s\n' "{\"_type\":\"playlist\",\"channel_id\":\"$channel\",\"channel\":\"Catalog channel\",\"description\":\"Complete catalog fixture\",\"thumbnails\":[{\"id\":\"avatar_uncropped\",\"url\":\"https://yt3.googleusercontent.com/catalog=s176-c\"}],\"entries\":[{\"id\":\"$prefix-1\",\"title\":\"Listing title 1\",\"upload_date\":\"20260101\"}]}"
    exit 0 ;;
  *' --lazy-playlist '*)
    printf '%%s\n' "$channel" >> "$state/started"
    index=1
    while [ "$index" -le 5 ]; do
      printf '%%s\n' "{\"id\":\"$prefix-$index\",\"title\":\"Listing title $index\",\"upload_date\":\"20260101\"}"
      index=$((index + 1))
    done
    printf '%%s\n' "{\"id\":\"$prefix-3\",\"title\":\"Duplicate listing\"}"
    while [ ! -f "$state/release" ]; do sleep 0.02; done
    if [ -f "$state/fail" ]; then
      printf '%%s\n' 'fixture downloader detail MUST NOT appear in API responses' >&2
      exit 9
    fi
    if [ -n "$cookie_file" ]; then test -r "$cookie_file"; fi
    while [ "$index" -le 65 ]; do
      printf '%%s\n' "{\"id\":\"$prefix-$index\",\"title\":\"Listing title $index\",\"upload_date\":\"20260101\"}"
      index=$((index + 1))
    done
    exit 0 ;;
esac
printf '%%s\n' 'Unexpected fixture invocation' >&2
exit 8`, state)
}

// csyncOptions makes the application list catalogs with the script. The streamed videos are stored four at a
// time, so the five first ones are visible while the rest is held back.
func csyncOptions(t *testing.T, state string) []func(*Options) {
	t.Helper()
	return []func(*Options){
		withDownloader(t, csyncScript(state)),
		func(o *Options) {
			o.Jobs.Catalogs.BatchSize = 4
			o.Jobs.Catalogs.ScopeInterval = 5 * time.Millisecond
			o.Jobs.Analysis.Debounce = time.Millisecond
		},
	}
}

// csyncMarker creates one of the files the script reacts to.
func csyncMarker(t *testing.T, state, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(state, name), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// csyncStarts counts the catalog extractions the script started.
func csyncStarts(t *testing.T, state string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "started"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

// csyncGet collects bounded pages of a catalog synchronization response.
func csyncGet(t *testing.T, handler http.Handler, path, token string) jobs.Catalog {
	t.Helper()
	res := request(t, handler, path, token)
	acctExpect(t, res, http.StatusOK, "")
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("catalog synchronization state is cacheable")
	}
	if strings.Contains(res.Body.String(), "fixture downloader detail") {
		t.Fatal("the catalog exposed the downloader's stderr")
	}
	var result jobs.CatalogPage
	acctDecode(t, res, &result)
	answer := result.Catalog
	for page := 2; result.HasMore; page++ {
		res = request(t, handler, fmt.Sprintf("%s&page=%d", path, page), token)
		acctExpect(t, res, http.StatusOK, "")
		var next jobs.CatalogPage
		acctDecode(t, res, &next)
		answer.Videos = append(answer.Videos, next.Videos...)
		result = next
	}
	return answer
}

func csyncWait(t *testing.T, handler http.Handler, path, token string, ready func(jobs.Catalog) bool) jobs.Catalog {
	t.Helper()
	var answer jobs.Catalog
	waitFor(t, "the channel catalog", func() bool {
		answer = csyncGet(t, handler, path, token)
		return ready(answer)
	})
	return answer
}

// csyncNative resolves the native id behind a video the API addressed by record id.
func csyncNative(t *testing.T, app core.App, recordID string) string {
	t.Helper()
	record, err := app.FindRecordById("videos", recordID)
	if err != nil {
		t.Fatalf("video %q is not addressed by a stored record id: %v", recordID, err)
	}
	return record.GetString("platform_id")
}

// csyncExpectMembers checks that the answer lists exactly the videos of the stored catalog.
func csyncExpectMembers(t *testing.T, app core.App, channelNative string, answer jobs.Catalog) {
	t.Helper()
	row := serCatalogRow(t, app, channelNative)
	entries, err := app.FindRecordsByFilter("channel_catalog_entries", "catalog = {:catalog}", "", 0, 0, dbx.Params{"catalog": row.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(answer.Videos) {
		t.Fatalf("catalog entries=%d videos=%d", len(entries), len(answer.Videos))
	}
	members := map[string]bool{}
	for _, entry := range entries {
		members[entry.GetString("video")] = true
	}
	for _, video := range answer.Videos {
		if !members[video.ID] {
			t.Fatalf("%+v is not a video of the stored catalog (videos are addressed by record id)", video)
		}
	}
}

func TestChannelCatalogProgressesBeyondThirtyAndPollingNeverDuplicatesTheExtraction(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	app, handler := newApp(t, csyncOptions(t, state)...)
	native := serChannel("catalog")
	channelID := storedChannel(t, app, testPlatforms().Default().ID(), native, "Catalog channel")
	path := "/api/channel/" + channelID + "?sync=1"

	// The first answer does not wait for the platform; the request that started the synchronization ends
	// without cancelling it.
	ctx, cancel := context.WithCancel(context.Background())
	initial := httptest.NewRecorder()
	handler.ServeHTTP(initial, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	cancel()
	acctExpect(t, initial, http.StatusOK, "")
	var first jobs.Catalog
	acctDecode(t, initial, &first)
	if first.Sync.Status == jobs.Complete || first.Sync.Status == jobs.Failed {
		t.Fatalf("an uncached sync did not return while the platform was still answering: %+v", first.Sync)
	}

	progress := csyncWait(t, handler, path, "", func(a jobs.Catalog) bool { return a.Sync.Loaded == 5 })
	if progress.Sync.Status != jobs.Running {
		t.Fatalf("an unfinished stream claimed %q", progress.Sync.Status)
	}
	csyncExpectMembers(t, app, native, progress)
	for range 5 {
		csyncGet(t, handler, path, "")
	}
	if starts := csyncStarts(t, state); starts != 1 {
		t.Fatalf("polling duplicated the extraction: %d starts", starts)
	}
	// The channel page without ?sync=1 keeps its own contract.
	plain := request(t, handler, "/api/channel/"+channelID, "")
	if plain.Code != http.StatusOK || strings.Contains(plain.Body.String(), `"sync"`) {
		t.Fatalf("the plain channel answer changed: %d %s", plain.Code, plain.Body)
	}

	csyncMarker(t, state, "release")
	complete := csyncWait(t, handler, path, "", func(a jobs.Catalog) bool { return a.Sync.Status == jobs.Complete })
	if complete.Sync.Loaded != 65 {
		t.Fatalf("the catalog kept a first-page cap or counted duplicates: %+v", complete.Sync)
	}
	csyncExpectMembers(t, app, native, complete)
	seen := map[string]bool{}
	for _, video := range complete.Videos {
		if seen[video.ID] || video.ChannelID != channelID {
			t.Fatalf("invalid catalog member: %+v (the channel is addressed by its record id %s)", video, channelID)
		}
		seen[video.ID] = true
	}
	if complete.Channel == nil || complete.Channel.ID != channelID {
		t.Fatalf("the catalog's channel is not addressed by its record id: %+v", complete.Channel)
	}
	// Once complete the catalog is current: a view does not extract it again.
	csyncGet(t, handler, path, "")
	if starts := csyncStarts(t, state); starts != 1 {
		t.Fatalf("a complete catalog was extracted again: %d starts", starts)
	}
}

// A catalog that fails is not an error: the partial cards stay, the answer says it is pending while the
// background retries it after a backoff, and it completes on its own once the failure is gone. No request
// asked for the retry.
func TestChannelCatalogFailureIsRetriedInTheBackgroundAndNeverReportedAsAnError(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	csyncMarker(t, state, "fail")
	csyncMarker(t, state, "release")
	options := append(csyncOptions(t, state), func(o *Options) {
		o.Jobs.Catalogs.RetryBase = 100 * time.Millisecond
		o.Jobs.Catalogs.RetryMax = 100 * time.Millisecond
	})
	app, handler := newApp(t, options...)
	native := serChannel("catalog")
	channelID := storedChannel(t, app, testPlatforms().Default().ID(), native, "Catalog channel")
	path := "/api/channel/" + channelID + "?sync=1"

	// The extraction fails, sleeps and starts again (twice at least) while the answers keep saying pending.
	deadline := time.Now().Add(30 * time.Second)
	var partial jobs.Catalog
	for csyncStarts(t, state) < 3 || partial.Sync.Loaded < 5 {
		if time.Now().After(deadline) {
			t.Fatalf("the failed catalog was not retried in the background: %d starts, %+v", csyncStarts(t, state), partial.Sync)
		}
		partial = csyncGet(t, handler, path, "")
		if partial.Sync.Status == jobs.Failed {
			t.Fatalf("a transient failure was reported as an error: %+v", partial.Sync)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if partial.Sync.Loaded != 5 {
		t.Fatalf("the partial results disappeared after the failure: %+v", partial.Sync)
	}
	csyncExpectMembers(t, app, native, partial)

	if err := os.Remove(filepath.Join(state, "fail")); err != nil {
		t.Fatal(err)
	}
	complete := csyncWait(t, handler, path, "", func(a jobs.Catalog) bool { return a.Sync.Status == jobs.Complete })
	if complete.Sync.Loaded != 65 {
		t.Fatalf("the background retry did not complete the catalog: %+v", complete.Sync)
	}
}

// What the route refuses: a channel nobody stored, a platform that cannot list a catalog and a source the
// account turned off. Every refusal is the one error envelope.
func TestChannelCatalogRouteRefusals(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	app, handler := newApp(t, csyncOptions(t, state)...)
	yt := testPlatforms().Default().ID()
	channelID := storedChannel(t, app, yt, serChannel("catalog"), "Catalog channel")

	acctExpect(t, request(t, handler, "/api/channel/doesnotexist123?sync=1", ""), http.StatusNotFound, "NOT_FOUND")
	acctExpect(t, anonymousRequest(t, handler, "/api/channel/"+channelID+"?sync=1"), http.StatusUnauthorized, "")
	for _, p := range testPlatforms().All() {
		if p.CatalogSync() != nil {
			continue
		}
		other := storedChannel(t, app, p.ID(), "streamer", "Streamer")
		acctExpect(t, request(t, handler, "/api/channel/"+other+"?sync=1", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	}

	_, token := viewerOf(t, handler)
	res := send(t, handler, http.MethodPut, "/api/sources/"+yt+"/activation", token, `{"enabled":false}`)
	if res.Code != http.StatusOK {
		t.Fatalf("turning the source off: %d %s", res.Code, res.Body)
	}
	acctExpect(t, request(t, handler, "/api/channel/"+channelID+"?sync=1", ""), http.StatusConflict, "SOURCE_DISABLED")
	if starts := csyncStarts(t, state); starts != 0 {
		t.Fatalf("a refused request started %d extractions", starts)
	}
}

func TestChannelCatalogPagesSearchAndSeriesScope(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	csyncMarker(t, state, "release")
	options := append(csyncOptions(t, state), func(o *Options) { o.Jobs.Analysis.Debounce = time.Hour })
	app, handler := newApp(t, options...)
	channelID := storedChannel(t, app, testPlatforms().Default().ID(), serChannel("catalog"), "Catalog channel")
	path := "/api/channel/" + channelID
	complete := csyncWait(t, handler, path+"?sync=1", "", func(a jobs.Catalog) bool { return a.Sync.Status == jobs.Complete })
	if complete.Sync.Loaded != 65 {
		t.Fatalf("catalog: %+v", complete.Sync)
	}
	getPage := func(query string) jobs.CatalogPage {
		t.Helper()
		res := request(t, handler, path+"?sync=1"+query, "")
		acctExpect(t, res, http.StatusOK, "")
		var page jobs.CatalogPage
		acctDecode(t, res, &page)
		return page
	}
	first, second, last := getPage(""), getPage("&page=2"), getPage("&page=3")
	if len(first.Videos) != 30 || len(second.Videos) != 30 || len(last.Videos) != 5 || !first.HasMore || !second.HasMore || last.HasMore || first.Total != 65 || first.Sync.Loaded != 65 {
		t.Fatalf("pages: %+v %+v %+v", first, second, last)
	}
	if first.Revision == "" || first.Revision != second.Revision || first.Revision != last.Revision {
		t.Fatalf("public revision not stable across pages: %q %q %q", first.Revision, second.Revision, last.Revision)
	}
	moved, err := app.FindRecordById("videos", last.Videos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	moved.Set("published", "")
	if err := app.Save(moved); err != nil {
		t.Fatal(err)
	}
	before := getPage("")
	moved.Set("published", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := app.Save(moved); err != nil {
		t.Fatal(err)
	}
	after, afterSecond := getPage(""), getPage("&page=2")
	if before.Revision == after.Revision || after.Revision != afterSecond.Revision || after.Videos[0].ID != moved.Id {
		t.Fatalf("date move did not change the shared revision: before=%q after=%+v", before.Revision, after)
	}
	seen := map[string]bool{}
	for _, page := range []jobs.CatalogPage{first, second, last} {
		for _, video := range page.Videos {
			if seen[video.ID] {
				t.Fatalf("duplicate video %s", video.ID)
			}
			seen[video.ID] = true
		}
	}
	for i, video := range complete.Videos {
		record, err := app.FindRecordById("videos", video.ID)
		if err != nil {
			t.Fatal(err)
		}
		record.Set("published", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i))
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	asc, asc2 := getPage("&order=asc"), getPage("&order=asc&page=2")
	desc, desc2 := getPage("&order=desc"), getPage("&order=desc&page=2")
	if asc.Videos[0].ID != complete.Videos[0].ID || asc2.Videos[0].ID != complete.Videos[30].ID ||
		desc.Videos[0].ID != complete.Videos[64].ID || desc2.Videos[0].ID != complete.Videos[34].ID ||
		asc.Total != 65 || asc.Sync.Loaded != 65 {
		t.Fatalf("chronological paging: asc=%+v asc2=%+v desc=%+v desc2=%+v", asc, asc2, desc, desc2)
	}
	searchedAsc := getPage("&order=asc&q=listing")
	if len(searchedAsc.Videos) != 30 || searchedAsc.Videos[0].ID != complete.Videos[0].ID {
		t.Fatalf("chronological search: %+v", searchedAsc)
	}
	acctExpect(t, request(t, handler, path+"?sync=1&order=sideways", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	target := last.Videos[0]
	record, err := app.FindRecordById("videos", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("titles", map[string]string{"original": "Été spécial", "en": "Translated title"})
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	search := getPage("&q=ete%20special")
	if search.Total != 1 || search.Sync.Loaded != 65 || len(search.Videos) != 1 || search.Videos[0].ID != target.ID {
		t.Fatalf("folded search: %+v", search)
	}
	if search.Revision == desc.Revision {
		t.Fatal("title change did not change the catalog revision")
	}
	owner := testUser(t, app, "paging-private@example.test")
	record.Set("owners", []string{owner.Id})
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	hidden := getPage("&q=ete%20special")
	if hidden.Total != 0 || hidden.Sync.Loaded != 64 || len(hidden.Videos) != 0 {
		t.Fatalf("private catalog member leaked: %+v", hidden)
	}
	if hidden.Revision == search.Revision {
		t.Fatal("visibility change did not change the catalog revision")
	}
	acctExpect(t, request(t, handler, path+"?sync=1&limit=51", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	acctExpect(t, request(t, handler, path+"?sync=1&page=0", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	collection, err := app.FindCollectionByNameOrId("series")
	if err != nil {
		t.Fatal(err)
	}
	saveSeries := func(channelID, name string, videos ...string) *core.Record {
		t.Helper()
		record := core.NewRecord(collection)
		record.Set("channel", channelID)
		record.Set("name", name)
		record.Set("videos", videos)
		record.Set("curated", true)
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	mine := saveSeries(channelID, "Visible series", first.Videos[0].ID, target.ID)
	otherNative := serChannel("other")
	otherChannel := storedChannel(t, app, testPlatforms().Default().ID(), otherNative, "Other channel")
	otherVideo := storedVideo(t, app, platform.Video{ID: "other-video", Platform: testPlatforms().Default().ID(), ChannelID: otherNative, Title: "Other video"})
	saveSeries(otherChannel, "Unrelated series", otherVideo)
	seriesResponse := request(t, handler, path+"/series", "")
	acctExpect(t, seriesResponse, http.StatusOK, "")
	var summaries struct {
		Series []series.Summary `json:"series"`
	}
	acctDecode(t, seriesResponse, &summaries)
	if len(summaries.Series) != 1 || summaries.Series[0].ID != mine.Id || summaries.Series[0].ChannelID != channelID || summaries.Series[0].VideoCount != 1 {
		t.Fatalf("channel series scope or private member filtering failed: %+v", summaries.Series)
	}
	acctExpect(t, request(t, handler, "/api/channel/doesnotexist123/series", ""), http.StatusNotFound, "NOT_FOUND")
}
