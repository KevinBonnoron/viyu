package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/youtube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// env is a bootstrapped application with the account rules bound, and the service on top of it. The real
// YouTube platform validates the identifiers; it never reaches the network here.
type env struct {
	app      core.App
	store    *catalog.Store
	svc      *Service
	platform platform.Platform
	wakes    *wakes
}

// wakes counts the backfill wake-ups the store asked for.
type wakes struct{ profiles, publications int }

func newEnv(t *testing.T) *env {
	t.Helper()
	app := testutil.NewApp(t)
	RegisterHooks(app)
	registerRoleGuards(app)
	catalog.RegisterHooks(app)
	yt := youtube.New(youtube.Options{Runner: ytdlp.New(ytdlp.Options{Binary: "/nonexistent/yt-dlp"})})
	registry := platform.NewRegistry(yt)
	w := &wakes{}
	store := catalog.New(app, registry, catalog.Options{
		OnProfilesWanted:     func() { w.profiles++ },
		OnPublicationsWanted: func() { w.publications++ },
	})
	return &env{app: app, store: store, svc: New(app, store, nil, registry), platform: yt, wakes: w}
}

// handler serves the records API of the application, with the account rules bound.
func (e *env) handler(t *testing.T) http.Handler {
	t.Helper()
	// The suites send many requests from one address; production keeps the limits migrations set up.
	settings := e.app.Settings()
	settings.RateLimits.Enabled = false
	if err := e.app.Save(settings); err != nil {
		t.Fatal(err)
	}
	router, err := apis.NewRouter(e.app)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.app.OnServe().Trigger(&core.ServeEvent{App: e.app, Router: router, Server: &http.Server{}}, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

// user creates an account with the given role the way trusted code does and returns it with an auth token.
func (e *env) user(t *testing.T, email, role string) (*core.Record, string) {
	t.Helper()
	collection, err := e.app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("account-test-password-123")
	record.Set("name", email)
	if role != "" {
		record.Set("role", role)
	}
	if err := e.app.Save(record); err != nil {
		t.Fatal(err)
	}
	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	return record, token
}

func (e *env) profile(t *testing.T, user *core.Record, name string) *core.Record {
	t.Helper()
	return e.record(t, "profiles", map[string]any{"user": user.Id, "name": name})
}

func (e *env) record(t *testing.T, collection string, values map[string]any) *core.Record {
	t.Helper()
	schema, err := e.app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := e.app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// video stores a public video (and its channel) the way a listing does and returns its record.
func (e *env) video(t *testing.T, id string) *core.Record {
	t.Helper()
	v := platform.Video{ID: id, Platform: e.platform.ID(), ChannelID: "UCactivity", Title: id}
	if err := e.store.StoreVideo(&v, ""); err != nil {
		t.Fatal(err)
	}
	record, err := e.store.FindVideo(e.platform.ID(), id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func (e *env) firstProfile(t *testing.T, userID string) *core.Record {
	t.Helper()
	profile, err := e.app.FindFirstRecordByFilter("profiles", "user = {:user}", map[string]any{"user": userID})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func send(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = strings.NewReader(string(data))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
