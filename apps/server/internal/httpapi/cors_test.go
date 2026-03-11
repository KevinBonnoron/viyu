package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

var testNativeOrigins = []string{"https://localhost", "capacitor://localhost", "wails://localhost", "http://wails.localhost"}

// corsHandler serves PocketBase's default router (health and auth routes) with the server's CORS
// middleware bound over the default one, as the application does.
func corsHandler(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	app := testutil.NewApp(t)
	// The requests of a suite come from one address; production keeps the limits migrations set up.
	settings := app.Settings()
	settings.RateLimits.Enabled = false
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	router.Bind(New(Deps{App: app}).CORS())
	handler, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return app, handler
}

func corsRequest(handler http.Handler, method, path, origin string, headers map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func TestNativeOriginsGetCredentialedPreflight(t *testing.T) {
	t.Parallel()
	_, handler := corsHandler(t)
	for _, origin := range testNativeOrigins {
		for _, path := range []string{"/api/health", "/api/collections/users/auth-with-password"} {
			res := corsRequest(handler, http.MethodOptions, path, origin, map[string]string{
				"Access-Control-Request-Method":  "POST",
				"Access-Control-Request-Headers": "authorization,content-type",
			}, "")
			header := res.Header()
			if res.Code != http.StatusNoContent || header.Get("Access-Control-Allow-Origin") != origin || header.Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatalf("%s preflight %s: %d %v", origin, path, res.Code, header)
			}
			if got := header.Get("Access-Control-Allow-Headers"); got != "Authorization,Content-Type" {
				t.Fatalf("%s preflight allowed headers: %q", origin, got)
			}
			if methods := header.Get("Access-Control-Allow-Methods"); !strings.Contains(methods, "POST") || !strings.Contains(methods, "DELETE") {
				t.Fatalf("%s preflight allowed methods: %q", origin, methods)
			}
			if !strings.Contains(header.Get("Vary"), "Origin") {
				t.Fatalf("%s preflight is cacheable across origins: %v", origin, header)
			}
		}
	}
}

func TestNativeOriginsReachAuthAndDataRoutesWithCredentials(t *testing.T) {
	t.Parallel()
	app, handler := corsHandler(t)
	collection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	user := core.NewRecord(collection)
	user.SetEmail("cors@example.test")
	user.SetPassword("cors-test-password")
	user.Set("role", "user")
	if err := app.Save(user); err != nil {
		t.Fatal(err)
	}
	json := map[string]string{"Content-Type": "application/json"}
	for _, origin := range testNativeOrigins {
		res := corsRequest(handler, http.MethodPost, "/api/collections/users/auth-with-password", origin, json,
			`{"identity":"cors@example.test","password":"cors-test-password"}`)
		if res.Code != http.StatusOK || res.Header().Get("Access-Control-Allow-Origin") != origin || res.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatalf("%s auth: %d %v %s", origin, res.Code, res.Header(), res.Body)
		}
		// Failures must also be readable by the WebView, otherwise sign-in errors are opaque.
		res = corsRequest(handler, http.MethodPost, "/api/collections/users/auth-with-password", origin, json,
			`{"identity":"cors@example.test","password":"wrong-password"}`)
		if res.Code != http.StatusBadRequest || res.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatalf("%s failed auth: %d %v", origin, res.Code, res.Header())
		}
		if res := corsRequest(handler, http.MethodGet, "/api/health", origin, nil, ""); res.Code != http.StatusOK || res.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatalf("%s health: %d %v", origin, res.Code, res.Header())
		}
	}
}

func TestUnknownOriginsNeverGetCredentialedCORS(t *testing.T) {
	t.Parallel()
	_, handler := corsHandler(t)
	origins := []string{
		"https://evil.example",
		"https://localhost.evil.example",
		"http://localhost:8080",
		"http://localhost",
		"https://wails.localhost",
		"wails://wails.evil.example",
		"https://localhost:3000",
		"capacitor://localhost.evil.example",
		"http://localhost.",
		"null",
	}
	for _, origin := range origins {
		for _, method := range []string{http.MethodOptions, http.MethodGet} {
			res := corsRequest(handler, method, "/api/health", origin, map[string]string{
				"Access-Control-Request-Method":  "GET",
				"Access-Control-Request-Headers": "authorization",
			}, "")
			header := res.Header()
			if header.Get("Access-Control-Allow-Credentials") != "" {
				t.Fatalf("%s %s received credentialed CORS: %v", method, origin, header)
			}
			if allowed := header.Get("Access-Control-Allow-Origin"); allowed == origin {
				t.Fatalf("%s %s origin was reflected: %v", method, origin, header)
			}
		}
	}
}

func TestPlainLocalhostIsAnOriginOnlyInDevMode(t *testing.T) {
	t.Parallel()
	for origin, want := range map[string]struct{ production, dev bool }{
		"http://localhost":         {false, true},
		"https://localhost":        {true, true},
		"wails://localhost":        {true, true},
		"wails://wails":            {false, false},
		"http://wails.localhost":   {true, true},
		"http://localhost:8080":    {false, false},
		"https://wails.localhost":  {false, false},
		"wails://localhost.evil":   {false, false},
		"http://wails.localhost.x": {false, false},
	} {
		if got := isNativeOrigin(origin, false); got != want.production {
			t.Errorf("production isNativeOrigin(%q) = %v, want %v", origin, got, want.production)
		}
		if got := isNativeOrigin(origin, true); got != want.dev {
			t.Errorf("dev isNativeOrigin(%q) = %v, want %v", origin, got, want.dev)
		}
	}
}
