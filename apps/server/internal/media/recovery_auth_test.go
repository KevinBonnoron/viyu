package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestRecoveryDoesNotSendManifestCredentialsToForeignResource(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	var leaked atomic.Bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			leaked.Store(true)
		}
		if r.URL.Query().Get("sig") != fmt.Sprint(generation.Load()) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "foreign media")
	}))
	defer foreign.Close()
	address, _ := url.Parse(foreign.URL)
	foreignURL := "http://localhost:" + address.Port()
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprintf(w, "#EXTM3U\n%s/segment.ts?sig=%d\n", foreignURL, generation.Load())
	}))
	defer master.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", master.URL, generation.Load()), Headers: map[string]string{"Authorization": "Bearer secret", "Cookie": "session=secret"}}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	paths := resources(get(t, handler, "/api/stream/record1").Body.String())
	if len(paths) != 1 {
		t.Fatal("missing resource")
	}
	generation.Store(2)
	r := get(t, handler, paths[0])
	if r.Code != http.StatusOK || strings.TrimSpace(r.Body.String()) != "foreign media" || leaked.Load() {
		t.Fatalf("recovery: %d %q, credentials leaked: %v", r.Code, r.Body.String(), leaked.Load())
	}
}
