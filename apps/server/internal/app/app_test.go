package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestHealthAndUnknownAPIPaths(t *testing.T) {
	t.Parallel()
	_, handler := newApp(t)
	if res := request(t, handler, "/api/health", ""); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"status":"ok"`) {
		t.Fatalf("health: %d %s", res.Code, res.Body)
	}
	for _, path := range []string{"/api", "/api/missing"} {
		res := request(t, handler, path, "")
		if res.Code != http.StatusNotFound || !strings.Contains(res.Header().Get("Content-Type"), "application/json") || !strings.Contains(res.Body.String(), `"code":"NOT_FOUND"`) {
			t.Fatalf("missing API swallowed: %s %d %s", path, res.Code, res.Body)
		}
	}
	if res := send(t, handler, http.MethodPost, "/api/desktop/session", "", `{"secret":"any"}`); res.Code != http.StatusNotFound {
		t.Fatalf("removed desktop session route still served: %d", res.Code)
	}
}

func TestEveryNetworkRouteNeedsAnAccount(t *testing.T) {
	t.Parallel()
	_, handler := newApp(t)
	for _, path := range []string{"/api/search?q=x", "/api/video/x", "/api/channel/x", "/api/feed?profile=x", "/api/series?profile=x", "/api/sources", "/api/stats?profile=x", "/api/account"} {
		if res := anonymousRequest(t, handler, path); res.Code != http.StatusUnauthorized {
			t.Fatalf("%s answered an anonymous request: %d %s", path, res.Code, res.Body)
		}
	}
	if res := anonymousRequest(t, handler, "/api/registration"); res.Code != http.StatusOK {
		t.Fatalf("the registration status is public: %d", res.Code)
	}
}
