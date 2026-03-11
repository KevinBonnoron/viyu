package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// serve builds a server over a real application and serves the routes the test registers.
func serve(t *testing.T, register func(s *Server, r *router.Router[*core.RequestEvent])) http.Handler {
	t.Helper()
	app := testutil.NewApp(t)
	group := jobs.NewGroup()
	t.Cleanup(group.Stop)
	server := New(Deps{App: app, Group: group})
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	register(server, r)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
	return res
}

func envelope(t *testing.T, res *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %q", res.Body)
	}
	return body
}

func TestEveryErrorIsTheCodeMessageEnvelope(t *testing.T) {
	t.Parallel()
	h := serve(t, func(s *Server, r *router.Router[*core.RequestEvent]) {
		api := s.API(r)
		api.PublicGET("/typed", func(*core.RequestEvent) error { return apierr.NotFound("Video") })
		api.PublicGET("/cause", func(*core.RequestEvent) error {
			return apierr.RequestFailed().WithCause(errors.New("yt-dlp --cookies /tmp/secret failed: stderr"))
		})
		api.PublicGET("/plain", func(*core.RequestEvent) error { return errors.New("sql: connection refused on /data/db") })
		api.PublicGET("/data", func(*core.RequestEvent) error {
			err := apierr.Invalid("Check the form.")
			err.Data = map[string]any{"email": map[string]string{"code": "validation_required"}}
			return err
		})
		api.PublicGET("/bad", func(e *core.RequestEvent) error { return router.NewBadRequestError("Bad body.", nil) })
	})
	cases := []struct {
		path   string
		status int
		code   string
	}{
		{"/typed", 404, apierr.CodeNotFound},
		{"/cause", 502, apierr.CodeRequestFailed},
		{"/plain", 500, apierr.CodeInternal},
		{"/data", 400, apierr.CodeInvalidRequest},
		{"/bad", 400, apierr.CodeInvalidRequest},
	}
	for _, c := range cases {
		res := get(h, c.path)
		body := envelope(t, res)
		if res.Code != c.status || body["code"] != c.code || body["message"] == "" {
			t.Fatalf("%s: %d %s", c.path, res.Code, res.Body)
		}
		if _, old := body["error"]; old {
			t.Fatalf("%s: the old error shape is back: %s", c.path, res.Body)
		}
		for _, leak := range []string{"secret", "stderr", "sql:", "/data/db"} {
			if strings.Contains(res.Body.String(), leak) {
				t.Fatalf("%s: %q leaked: %s", c.path, leak, res.Body)
			}
		}
	}
	if body := envelope(t, get(h, "/data")); body["data"] == nil {
		t.Fatal("structured detail was dropped")
	}
	if body := envelope(t, get(h, "/typed")); body["data"] != nil || len(body) != 2 {
		t.Fatalf("an error without detail must be exactly code and message: %v", body)
	}
}

func TestSourceRateLimitPublishesCooldownWithoutLeakingDownloaderOutput(t *testing.T) {
	t.Parallel()
	h := serve(t, func(s *Server, r *router.Router[*core.RequestEvent]) {
		s.API(r).PublicGET("/limited", func(*core.RequestEvent) error {
			return apierr.RateLimited().WithCause(&ytdlp.Error{
				Kind: ytdlp.KindRateLimited, RetryAfter: 1500 * time.Millisecond, Stderr: "secret",
			})
		})
	})
	res := get(h, "/limited")
	if res.Code != http.StatusServiceUnavailable || res.Header().Get("Retry-After") != "2" {
		t.Fatalf("cooldown must round up: %d %+v", res.Code, res.Header())
	}
	body := envelope(t, res)
	if body["code"] != apierr.CodeUpstreamRateLimit || strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("rate limit envelope: %s", res.Body)
	}
}

func TestAValueThatWasNeverStoredIsAnErrorNotAHalfWrittenAnswer(t *testing.T) {
	t.Parallel()
	h := serve(t, func(s *Server, r *router.Router[*core.RequestEvent]) {
		s.API(r).PublicGET("/video", func(e *core.RequestEvent) error {
			return s.respond(e, http.StatusOK, platform.Video{ID: "native-id", Title: "never stored"})
		})
		s.API(r).PublicGET("/stored", func(e *core.RequestEvent) error {
			return s.respond(e, http.StatusOK, platform.Video{ID: "native-id", RecordID: "rec1"})
		})
	})
	res := get(h, "/video")
	if res.Code != http.StatusInternalServerError || strings.Contains(res.Body.String(), "native-id") {
		t.Fatalf("an unstored video was served: %d %s", res.Code, res.Body)
	}
	if body := envelope(t, res); body["code"] != apierr.CodeInternal {
		t.Fatalf("body: %v", body)
	}
	res = get(h, "/stored")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"id":"rec1"`) || strings.Contains(res.Body.String(), "native-id") {
		t.Fatalf("a stored video: %d %s", res.Code, res.Body)
	}
}

func TestRoutesNeedAnAccountUnlessPublic(t *testing.T) {
	t.Parallel()
	h := serve(t, func(s *Server, r *router.Router[*core.RequestEvent]) {
		api := s.API(r)
		api.GET("/private", func(e *core.RequestEvent) error { return s.respond(e, 200, map[string]string{"ok": "yes"}) })
		api.PublicGET("/open", func(e *core.RequestEvent) error { return s.respond(e, 200, map[string]string{"ok": "yes"}) })
	})
	if res := get(h, "/private"); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous private route: %d %s", res.Code, res.Body)
	}
	if res := get(h, "/open"); res.Code != http.StatusOK {
		t.Fatalf("public route: %d", res.Code)
	}
}

func TestARouteRefusesNewWorkOnceTheServerStops(t *testing.T) {
	t.Parallel()
	app := testutil.NewApp(t)
	group := jobs.NewGroup()
	server := New(Deps{App: app, Group: group})
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	server.API(r).PublicGET("/x", func(e *core.RequestEvent) error { return noContent(e) })
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	if res := get(mux, "/x"); res.Code != http.StatusNoContent {
		t.Fatalf("running: %d", res.Code)
	}
	group.Stop()
	res := get(mux, "/x")
	if res.Code != http.StatusServiceUnavailable || envelope(t, res)["code"] != apierr.CodeUnavailable {
		t.Fatalf("stopped: %d %s", res.Code, res.Body)
	}
}
