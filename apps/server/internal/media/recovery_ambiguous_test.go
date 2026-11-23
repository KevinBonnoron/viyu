package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestRecoveryRejectsAmbiguousRenditionIdentity(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			if generation.Load() == 1 {
				fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\nold.m3u8\n")
			} else {
				fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\none.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\ntwo.m3u8\n")
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, "/old.m3u8") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "wrong rendition")
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	paths := resources(get(t, handler, "/api/stream/record1").Body.String())
	if len(paths) != 1 {
		t.Fatal("missing rendition")
	}
	generation.Store(2)
	response := get(t, handler, paths[0])
	if response.Code != http.StatusBadGateway || errorCode(t, response) != apierr.CodeStreamUpstream {
		t.Fatalf("ambiguous rendition: %d %s", response.Code, response.Body)
	}
}
