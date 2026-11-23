package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestLateOldRefusalDoesNotRefreshAgain(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	var oldRequests atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\nsegment.ts?sig=%d\n", generation.Load())
			return
		}
		if r.URL.Query().Get("sig") == "1" && generation.Load() == 2 {
			if oldRequests.Add(1) == 1 {
				close(started)
				<-release
			}
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "fresh bytes")
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	paths := resources(get(t, handler, "/api/stream/record1").Body.String())
	if len(paths) != 1 {
		t.Fatal("missing resource")
	}
	generation.Store(2)
	late := make(chan *httptest.ResponseRecorder, 1)
	go func() { late <- get(t, handler, paths[0]) }()
	<-started
	first := get(t, handler, paths[0])
	close(release)
	second := <-late
	if first.Code != 200 || second.Code != 200 || first.Body.String() != "fresh bytes" || second.Body.String() != "fresh bytes" || plat.calls.Load() != 2 {
		t.Fatalf("late refusal: %d %q, %d %q, lookups %d", first.Code, first.Body.String(), second.Code, second.Body.String(), plat.calls.Load())
	}
}
