package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestRecoveryPreservesRenditionSegmentAndRange(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sig") != fmt.Sprint(generation.Load()) {
			w.WriteHeader(http.StatusGone)
			return
		}
		sig := r.URL.Query().Get("sig")
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\nlow.m3u8?sig=%s\n#EXT-X-STREAM-INF:BANDWIDTH=200\nhigh.m3u8?sig=%s\n", sig, sig)
		case "/low.m3u8", "/high.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".m3u8")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:6,\n%s-1.ts?sig=%s\n#EXTINF:6,\n%s-2.ts?sig=%s\n", name, sig, name, sig)
		default:
			w.Header().Set("Content-Type", "video/mp2t")
			http.ServeContent(w, r, "segment.ts", time.Time{}, strings.NewReader(strings.TrimPrefix(r.URL.Path, "/")))
		}
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	master := get(t, handler, "/api/stream/record1")
	variants := resources(master.Body.String())
	if master.Code != 200 || len(variants) != 2 {
		t.Fatalf("master: %d %s", master.Code, master.Body)
	}
	variant := get(t, handler, variants[1])
	segments := resources(variant.Body.String())
	if variant.Code != 200 || len(segments) != 2 {
		t.Fatalf("variant: %d %s", variant.Code, variant.Body)
	}
	generation.Store(2)
	segment := get(t, handler, segments[1], "Range", "bytes=0-3")
	if segment.Code != http.StatusPartialContent || segment.Body.String() != "high" || segment.Header().Get("Content-Range") != "bytes 0-3/9" {
		t.Fatalf("recovered segment: %d %q %v", segment.Code, segment.Body.String(), segment.Header())
	}
	if segment.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("recovered response lost hardening")
	}
	firstSegment := get(t, handler, segments[0])
	if firstSegment.Code != http.StatusOK || firstSegment.Body.String() != "high-1.ts" {
		t.Fatalf("second old segment URL: %d %q", firstSegment.Code, firstSegment.Body.String())
	}
	if calls := plat.calls.Load(); calls != 2 {
		t.Fatalf("multiple old segments triggered %d manifest lookups", calls)
	}
	if got := get(t, handler, variants[1]); got.Code != 200 || !strings.Contains(got.Body.String(), "/resource/") {
		t.Fatalf("old variant URL: %d %s", got.Code, got.Body)
	}
}

func TestMasterRefusalRecoversWithoutClientRetry(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sig") == "old" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprint(w, "#EXTM3U\nsegment.ts\n")
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		sig := "new"
		if calls.Add(1) == 1 {
			sig = "old"
		}
		return platform.Manifest{URL: upstream.URL + "/master.m3u8?sig=" + sig}, nil
	}}
	response := get(t, api(newProxy(t, []*fakePlatform{plat}, nil)), "/api/stream/record1")
	if response.Code != 200 || len(resources(response.Body.String())) != 1 || calls.Load() != 2 {
		t.Fatalf("master: %d %s, lookups %d", response.Code, response.Body, calls.Load())
	}
}

func TestRecoveryAtSignatureDeadline(t *testing.T) {
	clock := newClock()
	var generation atomic.Int32
	generation.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\nsegment.ts?sig=%d\n", generation.Load())
			return
		}
		if r.URL.Query().Get("sig") != fmt.Sprint(generation.Load()) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "correct bytes")
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?expire=%d", upstream.URL, clock.Now().Add(time.Minute).Unix())}, nil
	}}
	proxy := newProxy(t, []*fakePlatform{plat}, func(o *Options) { o.Now = clock.Now })
	handler := api(proxy)
	master := get(t, handler, "/api/stream/record1")
	paths := resources(master.Body.String())
	if len(paths) != 1 {
		t.Fatalf("master: %s", master.Body)
	}
	generation.Store(2)
	clock.advance(31 * time.Second)
	response := get(t, handler, paths[0])
	if response.Code != 200 || response.Body.String() != "correct bytes" || plat.calls.Load() != 2 {
		t.Fatalf("expired recovery: %d %q, lookups %d", response.Code, response.Body.String(), plat.calls.Load())
	}
}

func TestConcurrentRecoverySharesManifestLookup(t *testing.T) {
	const clients = 8
	var generation atomic.Int32
	generation.Store(1)
	arrived := make(chan struct{}, clients)
	releaseOld := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\nsegment.ts?sig=%d\n", generation.Load())
			return
		}
		if r.URL.Query().Get("sig") == "1" && generation.Load() == 2 {
			arrived <- struct{}{}
			<-releaseOld
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "recovered bytes")
	}))
	defer upstream.Close()
	started, releaseLookup := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	plat := &fakePlatform{id: defaultID, manifest: func(ctx context.Context, _ string, _ string) (platform.Manifest, error) {
		if count.Add(1) == 2 {
			close(started)
			select {
			case <-releaseLookup:
			case <-ctx.Done():
				return platform.Manifest{}, ctx.Err()
			}
		}
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	paths := resources(get(t, handler, "/api/stream/record1").Body.String())
	if len(paths) != 1 {
		t.Fatal("missing resource")
	}
	generation.Store(2)
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, clients)
	for i := range responses {
		wg.Add(1)
		go func(i int) { defer wg.Done(); responses[i] = get(t, handler, paths[0]) }(i)
	}
	for range clients {
		<-arrived
	}
	close(releaseOld)
	<-started
	close(releaseLookup)
	wg.Wait()
	for _, r := range responses {
		if r.Code != 200 || r.Body.String() != "recovered bytes" {
			t.Fatalf("concurrent recovery: %d %q", r.Code, r.Body.String())
		}
	}
	if count.Load() != 2 {
		t.Fatalf("lookups = %d", count.Load())
	}
}

func TestRecoveryRefusesUnrelatedOrPermanentlyForbiddenResources(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			var generation atomic.Int32
			generation.Store(1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/master.m3u8" {
					name := "segment"
					if changed && generation.Load() == 2 {
						name = "other"
					}
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					fmt.Fprintf(w, "#EXTM3U\n%s.ts?sig=%d\n", name, generation.Load())
					return
				}
				w.WriteHeader(http.StatusForbidden)
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
			r := get(t, handler, paths[0])
			if r.Code != http.StatusBadGateway || errorCode(t, r) != apierr.CodeStreamUpstream || plat.calls.Load() != 2 {
				t.Fatalf("failure: %d %s (%d lookups)", r.Code, r.Body, plat.calls.Load())
			}
		})
	}
}
