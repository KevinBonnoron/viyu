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
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprint(ambiguous), func(t *testing.T) {
			var generation, manifests atomic.Int32
			generation.Store(1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/master.m3u8" {
					manifests.Add(1)
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					if generation.Load() == 1 {
						fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\nold.m3u8\n")
					} else {
						fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\none.m3u8\n")
						if ambiguous {
							fmt.Fprint(w, "#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\ntwo.m3u8\n")
						}
					}
					return
				}
				if strings.HasSuffix(r.URL.Path, "/old.m3u8") {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				fmt.Fprint(w, "#EXTM3U\nsegment.ts\n")
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
			if ambiguous {
				if response.Code != http.StatusBadGateway || errorCode(t, response) != apierr.CodeStreamUpstream {
					t.Fatalf("ambiguous rendition: %d %s", response.Code, response.Body)
				}
			} else if response.Code != http.StatusOK || len(resources(response.Body.String())) != 1 {
				t.Fatalf("single rendition: %d %s", response.Code, response.Body)
			}
			if manifests.Load() != 2 || plat.calls.Load() != 2 {
				t.Fatalf("manifests %d, lookups %d", manifests.Load(), plat.calls.Load())
			}
		})
	}
}
