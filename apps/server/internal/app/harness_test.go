package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/all"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/peertube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/twitch"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/youtube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// offline is an address nothing listens on: the platforms of a test application never reach the Internet.
const offline = "http://127.0.0.1:1"

// offlineOptions are the options of an application whose platforms are all unreachable, with the channel
// profile workers off: tests point the parts they exercise at fixtures through the option mutators.
func offlineOptions(dir string) Options {
	return Options{
		DataDir:                dir,
		DisableChannelProfiles: true,
		YouTube:                youtube.Options{FeedURL: offline + "/feed"},
		Twitch: twitch.Options{
			ValidateURL: offline + "/validate", APIURL: offline + "/helix", GraphQLURL: offline + "/graphql",
			IRCDial: func(context.Context) (net.Conn, error) { return nil, errors.New("offline") },
		},
		PeerTube: peertube.Options{SepiaURL: offline},
	}
}

// newApp builds an application as production does, bootstraps it with the real schema and returns it with its
// HTTP handler. Every request to an account route is signed in as an ordinary viewer account unless it carries
// its own Authorization header (see viewerHandler); anonymousRequest opts out. The application is shut down when
// the test ends.
func newApp(t *testing.T, mutate ...func(*Options)) (*pocketbase.PocketBase, http.Handler) {
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
	// The suites send many requests from one address; production keeps the limits migrations set up.
	settings := app.Settings()
	settings.RateLimits.Enabled = false
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Shutdown(app) })
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router, Server: &http.Server{}}, func(e *core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return app, &viewerHandler{Handler: handler, app: app}
}

// withDownloader makes the application run script (a shell script body, see testutil.Script semantics: it
// receives yt-dlp's arguments) instead of yt-dlp. Unlike changing PATH it is private to the application, so
// tests that use it may run in parallel.
func withDownloader(t *testing.T, body string) func(*Options) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return func(o *Options) { o.YTDLP = ytdlp.Options{Binary: path} }
}

// Every route that can reach the network requires an account, but most behavior tests are about what the route
// does, not about who asks. The handler newApp returns therefore signs requests to those routes in as an
// ordinary viewer account when they carry no Authorization header. A test that is about the absence of an
// account asks with anonymousRequest, which opts out.
const anonymousHeader = "X-Test-Anonymous"

var accountRoutes = []string{"/api/search", "/api/resolve", "/api/video/", "/api/channel/", "/api/chat/", "/api/stream/"}

func requiresAccount(path string) bool {
	for _, prefix := range accountRoutes {
		if path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

type viewerHandler struct {
	http.Handler
	app   core.App
	once  sync.Once
	user  *core.Record
	token string
}

func (h *viewerHandler) viewer() *viewerHandler {
	h.once.Do(func() {
		h.user = testUser(nil, h.app, "viewer@example.test")
		var err error
		if h.token, err = h.user.NewAuthToken(); err != nil {
			panic(err)
		}
	})
	return h
}

func (h *viewerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" && r.Header.Get(anonymousHeader) == "" && requiresAccount(r.URL.Path) {
		r.Header.Set("Authorization", h.viewer().token)
	}
	h.Handler.ServeHTTP(w, r)
}

// viewerOf returns the account and token the handler signs anonymous-looking requests in as.
func viewerOf(t *testing.T, handler http.Handler) (*core.Record, string) {
	t.Helper()
	h, ok := handler.(*viewerHandler)
	if !ok {
		t.Fatal("handler was not built by newApp")
	}
	h.viewer()
	return h.user, h.token
}

// testUser creates an account with a default profile the way registration does. t may be nil when the caller
// has no test to fail (the viewer is made lazily from inside a handler).
func testUser(t *testing.T, app core.App, email string) *core.Record {
	collection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		panic(err)
	}
	user := core.NewRecord(collection)
	user.SetEmail(email)
	user.SetPassword("test-password-0123456789")
	user.Set("role", "user")
	if err := app.Save(user); err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return user
}

// tokenOf returns an auth token of the account.
func tokenOf(t *testing.T, user *core.Record) string {
	t.Helper()
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// profileOf returns the default profile of an account (the one registration creates).
func profileOf(t *testing.T, app core.App, user *core.Record) *core.Record {
	t.Helper()
	profile, err := app.FindFirstRecordByFilter("profiles", "user = {:user}", map[string]any{"user": user.Id})
	if err != nil {
		t.Fatalf("profile of %s: %v", user.Email(), err)
	}
	return profile
}

// request performs a GET; token is sent as the Authorization header when not empty.
func request(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, h, http.MethodGet, path, token, "")
}

// send performs a request with a JSON body.
func send(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}

// anonymousRequest asks a route with no account at all.
func anonymousRequest(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(anonymousHeader, "1")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

// platforms is the registry a test uses to build stores of its own (the platforms are never called: they
// are only asked about themselves).
func testPlatforms() *platform.Registry {
	runner := ytdlp.New(ytdlp.Options{})
	return all.Registry(all.Options{YouTube: youtube.Options{Runner: runner}, Twitch: twitch.Options{Runner: runner}})
}

// newStore returns a catalog store over the application, for tests that store videos and channels the way a
// listing would.
func newStore(app core.App) *catalog.Store {
	return catalog.New(app, testPlatforms(), catalog.Options{})
}

// storedVideo persists a video (and its channel) the way a listing would and returns its record id. The
// default platform of the registry is used when the video names none.
func storedVideo(t *testing.T, app core.App, v platform.Video) string {
	t.Helper()
	store := newStore(app)
	if v.Platform == "" {
		v.Platform = testPlatforms().Default().ID()
	}
	if v.ChannelID == "" {
		v.ChannelID = "UCstored"
		v.ChannelName = platform.First(v.ChannelName, "Stored channel")
	}
	if err := store.StoreVideo(&v, ""); err != nil {
		t.Fatal(err)
	}
	return v.RecordID
}

// storedChannel persists a channel the way a listing would and returns its record id.
func storedChannel(t *testing.T, app core.App, platformID, nativeID, name string) string {
	t.Helper()
	record, err := newStore(app).UpsertChannel(platformID, nativeID, platform.First(name, nativeID), "", "")
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// recordIDOf returns the record id the API addresses a stored video or channel by.
func recordIDOf(t *testing.T, app core.App, collection, nativeID, platformID string) string {
	t.Helper()
	store := newStore(app)
	find := store.FindVideo
	if collection == "channels" {
		find = store.FindChannel
	}
	record, err := find(platformID, nativeID)
	if err != nil {
		t.Fatalf("%s %s/%s is not stored: %v", collection, platformID, nativeID, err)
	}
	return record.Id
}

// enableSource turns a source on for the account (opt-in platforms such as PeerTube are off by default).
func enableSource(t *testing.T, app core.App, userID, platformID string) {
	t.Helper()
	if err := sources.WriteActivation(app, userID, platformID, true); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls until condition holds, failing the test after a minute (the cost is paid only on failure; a
// loaded machine running the race detector over the whole package is slow). Background work (the series
// analysis, the backfills) is asynchronous: tests wait for its effect, never for a fixed delay.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fixtureDownloaderBody is a yt-dlp stand-in answering the fixture channel and its one video; it fails or
// hangs for the video ids named "failed" and "cancel".
const fixtureDownloaderBody = `case "$*" in
 *watch?v=failed*) echo 'fixture download failed' >&2; exit 9 ;;
 *watch?v=cancel*) sleep 30; exit 0 ;;
 *' -o - '*) printf 'fixture-media'; exit 0 ;;
 *--dump-single-json*) printf '%s\n' '{"_type":"playlist","id":"UCfixture","channel_id":"UCfixture","channel":"Fixture channel","title":"Fixture channel","description":"Channel description","thumbnail":"https://i.ytimg.com/vi/fixture-video/hqdefault.jpg","thumbnails":[{"id":"banner_uncropped","url":"https://yt3.googleusercontent.com/banner=s0","preference":10},{"id":"avatar_uncropped","url":"https://yt3.googleusercontent.com/fixture-avatar=s0","preference":1}],"entries":[{"id":"fixture-video","title":"Fixture video","thumbnail":"https://i.ytimg.com/vi/fixture-video/hqdefault.jpg","duration":120,"upload_date":"20260102","view_count":42}]}'; exit 0 ;;
 *'/channel/UCfixture/videos'*) printf '%s\n' '{"id":"fixture-video","title":"Fixture video","duration":120,"upload_date":"20260102"}'; exit 0 ;;
esac
printf '%s\n' '{"id":"fixture-video","title":"Fixture video","fulltitle":"Original fixture","channel_id":"UCfixture","channel":"Fixture channel","thumbnail":"https://example.test/video.jpg","duration":120,"tags":["music"],"upload_date":"20260102","view_count":42}'`

// withFixtureDownloader is withDownloader(fixtureDownloaderBody).
func withFixtureDownloader(t *testing.T) func(*Options) {
	t.Helper()
	return withDownloader(t, fixtureDownloaderBody)
}
