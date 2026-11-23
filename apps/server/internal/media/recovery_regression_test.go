package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestResourceIdentityKeepsEscapedDelimiters(t *testing.T) {
	a := resourceIdentity("https://example.com/clips/a%2Fb.ts?sig=old")
	if a != resourceIdentity("https://example.com/clips/a%2Fb.ts?sig=new") || a == resourceIdentity("https://example.com/clips/a/b.ts?sig=new") {
		t.Fatalf("escaped path identity: %q", a)
	}
}

func TestRecoveredByteRangesAndVODIndex(t *testing.T) {
	var generation, playlists atomic.Int32
	generation.Store(1)
	data := strings.Repeat("a", 100) + strings.Repeat("b", 100) + strings.Repeat("c", 100)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			playlists.Add(1)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:1,\n#EXT-X-BYTERANGE:100@0\nfile.ts?sig=%d\n#EXTINF:1,\n#EXT-X-BYTERANGE:100\nfile.ts?sig=%d\n#EXTINF:1,\n#EXT-X-BYTERANGE:100\nfile.ts?sig=%d\n#EXT-X-ENDLIST\n", generation.Load(), generation.Load(), generation.Load())
			return
		}
		if r.URL.Query().Get("sig") != fmt.Sprint(generation.Load()) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "file.ts", time.Time{}, strings.NewReader(data))
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	master := get(t, handler, "/api/stream/record1")
	paths := resources(master.Body.String())
	if len(paths) != 3 || paths[0] == paths[1] || !strings.Contains(master.Body.String(), "#EXT-X-BYTERANGE:100@100") || !strings.Contains(master.Body.String(), "#EXT-X-BYTERANGE:100@200") {
		t.Fatalf("playlist: %s", master.Body)
	}
	generation.Store(2)
	second := get(t, handler, paths[1], "Range", "bytes=100-199")
	if second.Code != http.StatusPartialContent || second.Body.String() != strings.Repeat("b", 100) || second.Header().Get("Content-Range") != "bytes 100-199/300" {
		t.Fatalf("second range: %d %q %v", second.Code, second.Body, second.Header())
	}
	third := get(t, handler, paths[2], "Range", "bytes=200-299")
	if third.Code != http.StatusPartialContent || third.Body.String() != strings.Repeat("c", 100) {
		t.Fatalf("third range: %d %q", third.Code, third.Body)
	}
	if playlists.Load() != 2 || plat.calls.Load() != 2 {
		t.Fatalf("playlist downloads %d, lookups %d", playlists.Load(), plat.calls.Load())
	}
}

func TestOversizedResourceUsesBoundedRoute(t *testing.T) {
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	key := Key{RecordID: "rec", Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: "https://example.com/master.m3u8"})
	base, _ := url.Parse(found.manifest.URL)
	body := "#EXTM3U\n#EXT-X-BYTERANGE:100@0\nfile.ts?q=" + strings.Repeat("x", 25000) + "\n#EXT-X-BYTERANGE:100\nfile.ts?q=" + strings.Repeat("x", 25000) + "\n"
	rewritten, err := proxy.rewrite(key, found, base, []byte(body), "")
	if err != nil {
		t.Fatal(err)
	}
	paths := resources(rewritten)
	if len(paths) != 2 || paths[0] == paths[1] || len(paths[0]) > 256 || len(paths[1]) > 256 {
		t.Fatalf("fallback paths: %v", paths)
	}
	for _, path := range paths {
		id := strings.Split(path, "/resource/")[1]
		if item, ok := found.resource(id); !ok || !strings.Contains(item.url, strings.Repeat("x", 25000)) {
			t.Fatalf("lost oversized resource")
		}
	}
}

func TestResourceSetPreservesNewerGeneration(t *testing.T) {
	s := newResourceSet(4)
	old, current := &generation{id: 100}, &generation{id: 101}
	s.rememberChild("id", "https://example.com/new", "parent", "", current, false)
	s.rememberChild("id", "https://example.com/old", "parent", "", old, false)
	got := s.rememberIfAbsent(resource{id: "id", url: "https://example.com/token", generation: old})
	if got.url != "https://example.com/new" || got.generation != current {
		t.Fatalf("lost newer route: %+v", got)
	}
}

func TestConcurrentVODIndexFetch(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "#EXTM3U\none.ts?sig=new\ntwo.ts?sig=new\n#EXT-X-ENDLIST\n")
	}))
	defer upstream.Close()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	key := Key{Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: upstream.URL})
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			got, err := proxy.findResource(context.Background(), key, found, upstream.URL, resource{url: upstream.URL + "/" + name + ".ts?sig=old"})
			if err != nil || got != upstream.URL+"/"+name+".ts?sig=new" {
				t.Errorf("match: %q %v", got, err)
			}
		}(name)
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("downloads: %d", calls.Load())
	}
}

func TestStaleTokenRefreshesRefusedCurrentRoot(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.URL.Query().Get("sig")
		if r.URL.Path == "/master.m3u8" {
			if sig != fmt.Sprint(generation.Load()) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\none.ts?sig=%s\ntwo.ts?sig=%s\n", sig, sig)
			return
		}
		if sig != fmt.Sprint(generation.Load()) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, r.URL.Path)
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	paths := resources(get(t, handler, "/api/stream/record1").Body.String())
	if len(paths) != 2 {
		t.Fatal("missing segments")
	}
	generation.Store(2)
	if response := get(t, handler, paths[0]); response.Code != http.StatusOK || response.Body.String() != "/one.ts" {
		t.Fatalf("G2: %d %s", response.Code, response.Body)
	}
	generation.Store(3)
	if response := get(t, handler, paths[1]); response.Code != http.StatusOK || response.Body.String() != "/two.ts" {
		t.Fatalf("G3: %d %s", response.Code, response.Body)
	}
	if plat.calls.Load() != 3 {
		t.Fatalf("lookups: %d", plat.calls.Load())
	}
}

func TestLateRecoveryCannotPublishOlderGeneration(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	started, release := make(chan struct{}), make(chan struct{})
	var hold atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.URL.Query().Get("sig")
		if r.URL.Path == "/master.m3u8" {
			if sig == "2" && hold.CompareAndSwap(true, false) {
				close(started)
				<-release
			} else if sig != fmt.Sprint(generation.Load()) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\none.ts?sig=%s\ntwo.ts?sig=%s\n", sig, sig)
			return
		}
		if sig != fmt.Sprint(generation.Load()) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, sig)
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	paths := resources(get(t, handler, "/api/stream/record1").Body.String())
	if len(paths) != 2 {
		t.Fatal("missing segments")
	}
	generation.Store(2)
	if response := get(t, handler, paths[0]); response.Code != http.StatusOK {
		t.Fatalf("G2: %d", response.Code)
	}
	hold.Store(true)
	late := make(chan *httptest.ResponseRecorder, 1)
	go func() { late <- get(t, handler, paths[1]) }()
	<-started
	generation.Store(3)
	if response := get(t, handler, paths[0]); response.Code != http.StatusOK || response.Body.String() != "3" {
		t.Fatalf("G3: %d %s", response.Code, response.Body)
	}
	close(release)
	response := <-late
	if response.Code != http.StatusOK || response.Body.String() != "3" {
		t.Fatalf("late: %d %s", response.Code, response.Body)
	}
	if plat.calls.Load() != 3 {
		t.Fatalf("lookups: %d", plat.calls.Load())
	}
}

func TestRetainedRouteIncludesParent(t *testing.T) {
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	key := Key{RecordID: "rec", Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: "https://example.com/master.m3u8"})
	base, _ := url.Parse(found.manifest.URL)
	playlist := []byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4?sig=1\"\n")
	first, err := proxy.rewriteAt(key, found, base, playlist, "", "first-parent")
	if err != nil {
		t.Fatal(err)
	}
	second, err := proxy.rewriteAt(key, found, base, playlist, "", "second-parent")
	if err != nil {
		t.Fatal(err)
	}
	a, b := playlistURI.FindStringSubmatch(first), playlistURI.FindStringSubmatch(second)
	if len(a) != 2 || len(b) != 2 || a[1] == b[1] {
		t.Fatalf("routes: %q %q", first, second)
	}
	for i, path := range []string{a[1], b[1]} {
		item, ok := found.resource(strings.Split(path, "/resource/")[1])
		expected := []string{"first-parent", "second-parent"}[i]
		if !ok || item.parent != expected {
			t.Fatalf("parent %d: %+v", i, item)
		}
	}
}

func TestLivePlaylistIndexIsNotReused(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "#EXTM3U\none.ts?sig=new\n")
		} else {
			fmt.Fprint(w, "#EXTM3U\ntwo.ts?sig=new\n")
		}
	}))
	defer upstream.Close()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	key := Key{Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: upstream.URL})
	for _, name := range []string{"one", "two"} {
		got, err := proxy.findResource(context.Background(), key, found, upstream.URL, resource{url: upstream.URL + "/" + name + ".ts?sig=old"})
		if err != nil || got != upstream.URL+"/"+name+".ts?sig=new" {
			t.Fatalf("live %s: %q %v", name, got, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("downloads: %d", calls.Load())
	}
}

func TestRecoveryMatchesOnlySameEscapedPath(t *testing.T) {
	var replacement atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if replacement.Load() {
			fmt.Fprint(w, "#EXTM3U\n/clips/a/b.ts?sig=new\n")
		} else {
			fmt.Fprint(w, "#EXTM3U\n/clips/a%2Fb.ts?sig=new\n")
		}
	}))
	defer upstream.Close()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	key := Key{Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: upstream.URL})
	original := resource{url: upstream.URL + "/clips/a%2Fb.ts?sig=old"}
	got, err := proxy.findResource(context.Background(), key, found, upstream.URL, original)
	if err != nil || got != upstream.URL+"/clips/a%2Fb.ts?sig=new" {
		t.Fatalf("escaped match: %q %v", got, err)
	}
	replacement.Store(true)
	if got, err := proxy.findResource(context.Background(), key, found, upstream.URL, original); err == nil {
		t.Fatalf("accepted neighboring path %q", got)
	}
}

func TestOversizedVODIndexFallsBackToUncachedMatching(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nfirst.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=1\nsecond.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2\nthird.m3u8\n#EXT-X-ENDLIST\n")
	}))
	defer upstream.Close()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, func(o *Options) { o.MaxResources = 2 })
	key := Key{Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: upstream.URL})
	unique := resource{semantic: "variant:#EXT-X-STREAM-INF:BANDWIDTH=2"}
	got, err := proxy.findResource(context.Background(), key, found, upstream.URL, unique)
	if err != nil || got != upstream.URL+"/third.m3u8" {
		t.Fatalf("unique match: %q %v", got, err)
	}
	if len(found.playlists) != 0 {
		t.Fatal("oversized index was cached")
	}
	ambiguous := resource{semantic: "variant:#EXT-X-STREAM-INF:BANDWIDTH=1"}
	if got, err := proxy.findResource(context.Background(), key, found, upstream.URL, ambiguous); err == nil {
		t.Fatalf("accepted ambiguous match %q", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("downloads: %d", calls.Load())
	}
}
