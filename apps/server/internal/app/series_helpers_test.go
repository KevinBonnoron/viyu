package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// serQuickAnalysis makes the analysis worker act on a wake-up at once instead of after its debounce.
func serQuickAnalysis(o *Options) { o.Jobs.Analysis.Debounce = time.Millisecond }

// serNoDownloader makes every downloader run fail: a test whose catalogs are all stored must never need one.
func serNoDownloader(t *testing.T) func(*Options) {
	t.Helper()
	return withDownloader(t, "echo 'unexpected downloader run' >&2; exit 1")
}

// serChannel is a channel id of the default platform's real shape ("UC" and 22 characters) named after name.
func serChannel(name string) string {
	return ("UC" + strings.Repeat(name+"-", 24))[:24]
}

// serSeedCatalog stores the whole public catalog of a channel the way a finished synchronization leaves it:
// one video per title, in the catalog, and the catalog complete. It returns the channel's record id and the
// videos' record ids, in title order.
func serSeedCatalog(t *testing.T, app core.App, nativeID string, titles ...string) (string, []string) {
	t.Helper()
	yt := testPlatforms().Default().ID()
	store := newStore(app)
	videos := make([]platform.Video, len(titles))
	for i, title := range titles {
		videos[i] = platform.Video{
			ID: fmt.Sprintf("%.9s%02d", strings.ToLower(nativeID[2:]), i), Platform: yt, Title: title,
			ChannelID: nativeID, ChannelName: nativeID, Published: fmt.Sprintf("2026-01-%02d", i+1),
			Thumbnails: []string{fmt.Sprintf("https://example.test/thumbs/%d.jpg", i+1)},
		}
	}
	if err := store.StoreVideos(videos, ""); err != nil {
		t.Fatal(err)
	}
	channelID := recordIDOf(t, app, "channels", nativeID, yt)
	catalog := acctRecord(t, app, "channel_catalogs", map[string]any{"platform": yt, "platform_id": nativeID, "channel": channelID, "status": "complete", "completed_at": time.Now()})
	ids := make([]string, len(videos))
	for i, v := range videos {
		ids[i] = v.RecordID
		acctRecord(t, app, "channel_catalog_entries", map[string]any{"catalog": catalog.Id, "video": v.RecordID, "generation": "fixture"})
	}
	return channelID, ids
}

// serCatalogRow returns the stored catalog of a channel.
func serCatalogRow(t *testing.T, app core.App, nativeID string) *core.Record {
	t.Helper()
	row, err := app.FindFirstRecordByFilter("channel_catalogs", "platform_id = {:id}", map[string]any{"id": nativeID})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// serSubscribe makes the profile subscribe to the stored channel the way the records do, which wakes the
// analysis through the application's hook.
func serSubscribe(t *testing.T, app core.App, profile *core.Record, channelRecordID string) *core.Record {
	t.Helper()
	return acctRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": channelRecordID})
}

// serAnswer is the answer of GET /api/series.
type serAnswer struct {
	Series   []series.Summary `json:"series"`
	Sync     jobs.Sync        `json:"sync"`
	Analysis jobs.Overview    `json:"analysis"`
}

// serDetail is the answer of GET /api/series/{id}.
type serDetail struct {
	Series series.Series `json:"series"`
	Sync   jobs.Sync     `json:"sync"`
}

func serList(t *testing.T, handler http.Handler, token, profileID, query string) serAnswer {
	t.Helper()
	res := request(t, handler, "/api/series?profile="+profileID+query, token)
	acctExpect(t, res, http.StatusOK, "")
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a series answer is cacheable")
	}
	var answer serAnswer
	acctDecode(t, res, &answer)
	return answer
}

// serWaitList polls the series list until ready holds: the analysis is asynchronous, a test waits for its
// effect.
func serWaitList(t *testing.T, handler http.Handler, token, profileID string, ready func(serAnswer) bool) serAnswer {
	t.Helper()
	var answer serAnswer
	waitFor(t, "the series analysis", func() bool {
		answer = serList(t, handler, token, profileID, "")
		return ready(answer)
	})
	return answer
}

func serGet(t *testing.T, handler http.Handler, token, id, profileID string) serDetail {
	t.Helper()
	res := request(t, handler, "/api/series/"+id+"?profile="+profileID, token)
	acctExpect(t, res, http.StatusOK, "")
	var detail serDetail
	acctDecode(t, res, &detail)
	return detail
}

// serComplete is the state of an analysis that has finished: nothing is pending or failed.
func serComplete(a serAnswer) bool { return a.Sync.Status == jobs.Complete }

// serStarted builds an application whose services are not running yet, bootstraps it with the real schema
// and hands it to prepare to store what the instance already held before the server started; the server is
// then started and the application and its handler are returned. newApp starts the server before a test can
// store anything.
func serStarted(t *testing.T, prepare func(app core.App), mutate ...func(*Options)) (*pocketbase.PocketBase, http.Handler) {
	t.Helper()
	opts := offlineOptions(t.TempDir())
	for _, change := range mutate {
		change(&opts)
	}
	app := New(opts)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Shutdown(app) })
	settings := app.Settings()
	settings.RateLimits.Enabled = false
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	prepare(app)
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router, Server: &http.Server{}}, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return app, handler
}
