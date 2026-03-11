package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// hlsUpstream serves a master playlist, a variant with an initialization map and a seekable segment.
func hlsUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/master.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1920x1080\nmedia.m3u8\n"))
		case strings.HasSuffix(r.URL.Path, "/media.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6,\nsegment.ts?parent=master.m3u8\n#EXT-X-ENDLIST\n"))
		default:
			http.ServeContent(w, r, "segment.ts", time.Time{}, strings.NewReader("authenticated-media"))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestHLSBinarySegmentsRemainSeekableUnderManifestPaths(t *testing.T) {
	t.Parallel()
	upstream := hlsUpstream(t)
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/api/manifest/hls_playlist/master.m3u8"})}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))

	master := get(t, handler, "/api/stream/record1")
	if master.Code != 200 || master.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("HLS master unavailable: %d %s", master.Code, master.Body)
	}
	if strings.Contains(master.Body.String(), "native-") || strings.Contains(master.Body.String(), upstream.URL) {
		t.Fatalf("the master leaks upstream or native addresses: %s", master.Body)
	}
	first := resources(master.Body.String())
	if len(first) != 1 {
		t.Fatalf("master resources: %v", first)
	}
	variant := get(t, handler, first[0])
	if variant.Code != 200 {
		t.Fatalf("HLS variant unavailable: %d %s", variant.Code, variant.Body)
	}
	initialization := playlistURI.FindStringSubmatch(variant.Body.String())
	if len(initialization) != 2 || !strings.HasPrefix(initialization[1], "/api/stream/") {
		t.Fatalf("initialization escaped the same-origin proxy: %s", variant.Body)
	}
	if init := get(t, handler, initialization[1]); init.Code != 200 || init.Body.String() != "authenticated-media" {
		t.Fatalf("initialization bytes corrupted: %d %s", init.Code, init.Body)
	}
	segments := resources(variant.Body.String())
	if len(segments) != 1 {
		t.Fatalf("variant resources: %v", segments)
	}
	seek := get(t, handler, segments[0], "Range", "bytes=5-9")
	if seek.Code != http.StatusPartialContent || seek.Header().Get("Content-Range") != "bytes 5-9/19" || seek.Body.String() != "ntica" {
		t.Fatalf("byte-range media was mistaken for a playlist or could not be sought: %d %s %q", seek.Code, seek.Header().Get("Content-Range"), seek.Body.String())
	}
	for _, response := range []*httptest.ResponseRecorder{master, variant, seek} {
		header := response.Header()
		if header.Get("Cache-Control") != "no-store" || header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
			t.Fatalf("response is not hardened: %v", header)
		}
	}
}

func TestHLSResourcesNameThePlatformOnlyWhenItIsNotTheDefault(t *testing.T) {
	t.Parallel()
	manifest := fixedManifest(platform.Manifest{URL: "https://video.example/master.m3u8"})
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}, {id: otherID, manifest: manifest}}, nil)
	key := Key{VideoID: "6528877", RecordID: "recordvod0001", Platform: otherID}
	proxy.Remember(key, platform.Manifest{URL: "https://video.example/master.m3u8"})
	base, err := url.Parse("https://video.example/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	found := proxy.live(key)
	rewritten, err := proxy.rewrite(key, found, base, []byte("#EXTM3U\nsegment.ts\n"), "")
	if err != nil || !strings.Contains(rewritten, "?platform="+otherID) || !strings.Contains(rewritten, "/api/stream/recordvod0001/resource/") || strings.Contains(rewritten, "6528877") {
		t.Fatalf("a non-default platform's resource lost its platform: %s %v", rewritten, err)
	}
	key.Platform = defaultID
	found = proxy.remember(key, platform.Manifest{URL: "https://video.example/master.m3u8"})
	rewritten, err = proxy.rewrite(key, found, base, []byte("#EXTM3U\nsegment.ts\n"), "")
	if err != nil || strings.Contains(rewritten, "platform=") {
		t.Fatalf("the default platform is named in the resource: %s %v", rewritten, err)
	}
	rewritten, err = proxy.rewrite(key, found, base, []byte("#EXTM3U\nsegment.ts\n"), "youtube account")
	if err != nil || !strings.Contains(rewritten, "?source=youtube+account") {
		t.Fatalf("the connected source is lost: %s %v", rewritten, err)
	}
}

func TestDefaultPlatformOptionOverridesTheRegistryDefault(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}, {id: otherID}}, func(o *Options) { o.DefaultPlatform = otherID })
	key := Key{RecordID: "rec", Platform: otherID}
	found := proxy.remember(key, platform.Manifest{URL: "https://video.example/master.m3u8"})
	base, _ := url.Parse("https://video.example/master.m3u8")
	rewritten, err := proxy.rewrite(key, found, base, []byte("#EXTM3U\nsegment.ts\n"), "")
	if err != nil || strings.Contains(rewritten, "platform=") {
		t.Fatalf("configured default platform is named: %s %v", rewritten, err)
	}
}

func TestRewriteRefusesNonHTTPResources(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	key := Key{RecordID: "rec", Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: "https://video.example/master.m3u8"})
	base, _ := url.Parse("https://video.example/master.m3u8")
	for _, playlist := range []string{"#EXTM3U\nfile:///etc/passwd\n", "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"data:text/plain,x\"\n"} {
		if _, err := proxy.rewrite(key, found, base, []byte(playlist), ""); err == nil {
			t.Errorf("playlist was rewritten: %q", playlist)
		}
	}
}

func TestHostileUpstreamContentIsNeverServedAsADocument(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/master.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment.ts\n#EXTINF:6,\npage.html\n#EXTINF:6,\nimage.svg\n#EXTINF:6,\nsubtitles.vtt\n"))
		case strings.HasSuffix(r.URL.Path, "/segment.ts"):
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("media"))
		case strings.HasSuffix(r.URL.Path, "/subtitles.vtt"):
			w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
			_, _ = w.Write([]byte("WEBVTT"))
		case strings.HasSuffix(r.URL.Path, "/image.svg"):
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg onload=alert(1)></svg>"))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<script>alert(document.cookie)</script>"))
		}
	}))
	t.Cleanup(upstream.Close)
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/hls/master.m3u8"})}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	master := get(t, handler, "/api/stream/record1")
	if master.Code != http.StatusOK {
		t.Fatalf("master: %d %s", master.Code, master.Body)
	}
	found := resources(master.Body.String())
	if len(found) != 4 {
		t.Fatalf("master resources: %v", found)
	}
	for index, want := range []struct {
		contentType string
		attachment  bool
	}{{"video/mp2t", false}, {"application/octet-stream", true}, {"application/octet-stream", true}, {"text/vtt", false}} {
		response := get(t, handler, found[index])
		header := response.Header()
		if response.Code != http.StatusOK || header.Get("Content-Type") != want.contentType {
			t.Errorf("resource %d: %d content type %q, want %q", index, response.Code, header.Get("Content-Type"), want.contentType)
		}
		if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
			t.Errorf("resource %d lacks the document-neutralizing headers: %v", index, header)
		}
		if (header.Get("Content-Disposition") == "attachment") != want.attachment {
			t.Errorf("resource %d: Content-Disposition %q, attachment want %v", index, header.Get("Content-Disposition"), want.attachment)
		}
	}
}

// Credentials of the manifest reach only its own host: a playlist naming another host never receives them,
// and the platform's own media hosts get the headers without the session.
func TestManifestCredentialsDoNotLeakToOtherHosts(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	received := map[string]http.Header{}
	record := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			received[name] = r.Header.Clone()
			mu.Unlock()
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte(name))
		})
	}
	foreign := httptest.NewServer(record("foreign"))
	t.Cleanup(foreign.Close)
	cdn := httptest.NewServer(record("cdn"))
	t.Cleanup(cdn.Close)
	// The same loopback server answers under another name, which is what makes it another host.
	named := func(server *httptest.Server, host string) string {
		address, _ := url.Parse(server.URL)
		return "http://" + host + ":" + address.Port()
	}
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/master.m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = fmt.Fprintf(w, "#EXTM3U\nown.ts\n%s/foreign.ts\n%s/cdn.ts\n", named(foreign, "localhost"), named(cdn, "cdn.media.test"))
			return
		}
		mu.Lock()
		received["own"] = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("own"))
	}))
	t.Cleanup(own.Close)
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		// cdn.media.test is only a name: it resolves to the loopback server of the test.
		host, port, _ := strings.Cut(address, ":")
		if host == "cdn.media.test" {
			address = "127.0.0.1:" + port
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	plat := &fakePlatform{
		id:       defaultID,
		trusted:  func(host string) bool { return host == "cdn.media.test" },
		manifest: fixedManifest(platform.Manifest{URL: own.URL + "/master.m3u8", Headers: map[string]string{"X-Agent": "agent", "Cookie": "session=secret", "Authorization": "Bearer secret"}}),
	}
	handler := api(newProxy(t, []*fakePlatform{plat}, func(o *Options) { o.HTTP = &http.Client{Transport: transport} }))
	master := get(t, handler, "/api/stream/record1")
	found := resources(master.Body.String())
	if master.Code != 200 || len(found) != 3 {
		t.Fatalf("master: %d %s", master.Code, master.Body)
	}
	for _, path := range found {
		if response := get(t, handler, path); response.Code != 200 {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if h := received["own"]; h.Get("Cookie") != "session=secret" || h.Get("Authorization") != "Bearer secret" || h.Get("X-Agent") != "agent" {
		t.Errorf("the manifest's own host lost its headers: %v", h)
	}
	if h := received["cdn"]; h.Get("X-Agent") != "agent" || h.Get("Cookie") != "" || h.Get("Authorization") != "" {
		t.Errorf("the platform media host got credentials or lost its headers: %v", h)
	}
	if h := received["foreign"]; h.Get("X-Agent") != "" || h.Get("Cookie") != "" || h.Get("Authorization") != "" {
		t.Errorf("a foreign host got the manifest's headers: %v", h)
	}
}

func TestTrustedMediaHostComesFromTheKeysPlatform(t *testing.T) {
	t.Parallel()
	manifest := platform.Manifest{URL: "https://manifest.example.test/master.m3u8", Headers: map[string]string{"User-Agent": "agent", "Cookie": "session=secret"}}
	own := &fakePlatform{id: defaultID, trusted: func(host string) bool { return host == "media.alpha.test" }}
	other := &fakePlatform{id: otherID, trusted: func(host string) bool { return host == "media.beta.test" }}
	proxy := newProxy(t, []*fakePlatform{own, other}, nil)
	remote, _ := url.Parse("https://media.beta.test/seg.ts")
	for _, test := range []struct {
		platform string
		want     int
	}{{defaultID, 0}, {otherID, 1}} {
		plat, _ := proxy.platforms.Get(test.platform)
		got := requestHeaders(plat.TrustedMediaHost, manifest, remote)
		if len(got) != test.want {
			t.Errorf("%s: headers %v, want %d", test.platform, got, test.want)
		}
	}
}

func TestMediaEntriesAndResourcesAreBounded(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, func(o *Options) { o.MaxEntries, o.MaxResources = 4, 50 })
	found := proxy.remember(Key{VideoID: "a", RecordID: "a", Platform: defaultID}, platform.Manifest{URL: "https://video.example/master.m3u8"})
	found.resources.remember("keep", "https://video.example/keep.ts")
	for i := range 150 {
		found.resources.remember(fmt.Sprintf("r%d", i), "https://video.example/segment.ts")
		found.resources.lookup("keep") // a resource still in use is never the one evicted
	}
	if found.resources.len() != 50 {
		t.Fatalf("resources are not capped: %d", found.resources.len())
	}
	if found.resources.lookup("keep") == "" || found.resources.lookup("r0") != "" {
		t.Fatal("eviction is not least-recently-used")
	}
	for i := range 20 {
		key := Key{VideoID: fmt.Sprint(i), RecordID: fmt.Sprint(i), Platform: defaultID}
		proxy.Remember(key, platform.Manifest{URL: "https://video.example/master.m3u8"})
	}
	if size := proxy.manifests.Len(); size != 4 {
		t.Fatalf("manifests are not capped: %d", size)
	}
}

func TestDefaultBoundsApply(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	if proxy.maxResources != defaultMaxResources {
		t.Fatalf("resource bound: %d", proxy.maxResources)
	}
	for i := range defaultMaxEntries + 20 {
		proxy.Remember(Key{VideoID: fmt.Sprint(i)}, platform.Manifest{URL: "https://video.example/master.m3u8"})
	}
	if size := proxy.manifests.Len(); size != defaultMaxEntries {
		t.Fatalf("manifests are not capped: %d", size)
	}
}

func TestPurgeDropsExpiredManifests(t *testing.T) {
	t.Parallel()
	clock := newClock()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, func(o *Options) { o.Now = clock.Now })
	proxy.Remember(Key{VideoID: "a"}, platform.Manifest{URL: "https://video.example/master.m3u8"})
	proxy.Remember(Key{VideoID: "b"}, platform.Manifest{URL: "https://video.example/master.m3u8"})
	clock.advance(manifestLife - time.Second)
	proxy.live(Key{VideoID: "b"}) // activity keeps one alive
	clock.advance(2 * time.Second)
	proxy.Purge()
	if proxy.manifests.Len() != 1 || proxy.live(Key{VideoID: "b"}) == nil || proxy.live(Key{VideoID: "a"}) != nil {
		t.Fatalf("purge kept the wrong manifests: %d", proxy.manifests.Len())
	}
}

func TestManifestsAreAddressedPerKey(t *testing.T) {
	t.Parallel()
	upstream := hlsUpstream(t)
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/master.m3u8"})}
	proxy := newProxy(t, []*fakePlatform{plat}, nil)
	key := Key{VideoID: "v", RecordID: "r", Platform: defaultID, UserID: "alice", Revision: "1"}
	recorder := httptest.NewRecorder()
	if err := proxy.ServeManifest(recorder, httptest.NewRequest(http.MethodGet, "/", nil), key, ""); err != nil {
		t.Fatal(err)
	}
	id := strings.Split(strings.Split(resources(recorder.Body.String())[0], "/resource/")[1], "?")[0]
	for name, other := range map[string]Key{
		"another account":  {VideoID: "v", RecordID: "r", Platform: defaultID, UserID: "bob", Revision: "1"},
		"another revision": {VideoID: "v", RecordID: "r", Platform: defaultID, UserID: "alice", Revision: "2"},
	} {
		err := proxy.ServeResource(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), other, id, "")
		var failure *apierr.Error
		if !errors.As(err, &failure) || failure.Code != apierr.CodeResourceExpired {
			t.Errorf("%s reached the manifest's resources: %v", name, err)
		}
	}
	if err := proxy.ServeResource(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), key, id, ""); err != nil {
		t.Fatalf("the owner lost its resource: %v", err)
	}
}

func TestExpiredResourceAsksToRestartPlayback(t *testing.T) {
	t.Parallel()
	plat := &fakePlatform{id: defaultID}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	response := get(t, handler, "/api/stream/record1/resource/unknown")
	if response.Code != http.StatusNotFound || errorCode(t, response) != apierr.CodeResourceExpired {
		t.Fatalf("unknown resource: %d %s", response.Code, response.Body)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("errors are not hardened: %v", response.Header())
	}
}

func TestConcurrentViewersAskThePlatformOnce(t *testing.T) {
	t.Parallel()
	upstream := hlsUpstream(t)
	release := make(chan struct{})
	plat := &fakePlatform{id: defaultID}
	plat.manifest = func(ctx context.Context, id, cookies string) (platform.Manifest, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return platform.Manifest{}, ctx.Err()
		}
		return platform.Manifest{URL: upstream.URL + "/master.m3u8"}, nil
	}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	var group sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		group.Add(1)
		go func() {
			defer group.Done()
			codes[i] = get(t, handler, "/api/stream/record1").Code
		}()
	}
	close(release)
	group.Wait()
	for i, code := range codes {
		if code != 200 {
			t.Errorf("viewer %d: %d", i, code)
		}
	}
	if calls := plat.calls.Load(); calls != 1 {
		t.Fatalf("the platform was asked %d times", calls)
	}
}

func TestLeavingViewerDoesNotFailTheOthers(t *testing.T) {
	t.Parallel()
	upstream := hlsUpstream(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	plat := &fakePlatform{id: defaultID}
	plat.manifest = func(ctx context.Context, id, cookies string) (platform.Manifest, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			return platform.Manifest{}, ctx.Err()
		}
		return platform.Manifest{URL: upstream.URL + "/master.m3u8"}, nil
	}
	proxy := newProxy(t, []*fakePlatform{plat}, nil)
	key := Key{VideoID: "v", RecordID: "r", Platform: defaultID}
	leaving, leave := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(leaving)
		first <- proxy.ServeManifest(httptest.NewRecorder(), request, key, "")
	}()
	within(t, started)
	second := make(chan int, 1)
	go func() { second <- get(t, api(proxy), "/api/stream/r").Code }()
	leave()
	if err := <-first; err == nil {
		t.Fatal("the viewer that left was served")
	}
	close(release)
	select {
	case code := <-second:
		if code != 200 {
			t.Fatalf("the remaining viewer failed: %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}

func TestLookupFailuresAreClientSafe(t *testing.T) {
	t.Parallel()
	secret := errors.New("yt-dlp: /home/secret/cookies.txt: boom")
	for name, test := range map[string]struct {
		lookup  error
		source  string
		status  int
		code    string
		message string
	}{
		"classified failure passes through": {lookup: apierr.SubscriberOnly(), status: 403, code: apierr.CodeSubscriberOnly},
		"wrapped classified failure":        {lookup: fmt.Errorf("lookup: %w", apierr.SessionInvalid()), status: 401, code: apierr.CodeSessionInvalid},
		"unexpected failure":                {lookup: secret, status: 500, code: apierr.CodeInternal, message: "Stream failed"},
		"unexpected failure of a source":    {lookup: secret, source: "src", status: 502, code: apierr.CodeRequestFailed},
		"classified failure of a source":    {lookup: apierr.SessionInvalid(), source: "src", status: 401, code: apierr.CodeSessionInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			plat := &fakePlatform{id: defaultID}
			plat.manifest = func(context.Context, string, string) (platform.Manifest, error) {
				return platform.Manifest{}, test.lookup
			}
			proxy := newProxy(t, []*fakePlatform{plat}, func(o *Options) {
				o.Cookies = cookieFunc(func(string, string) (string, func(), error) { return "/jar", func() {}, nil })
			})
			err := proxy.ServeManifest(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), Key{VideoID: "v", RecordID: "r", Platform: defaultID}, test.source)
			var failure *apierr.Error
			if !errors.As(err, &failure) {
				t.Fatalf("not an API error: %v", err)
			}
			if failure.Status != test.status || failure.Code != test.code || (test.message != "" && failure.Message != test.message) {
				t.Fatalf("got %d %s %q", failure.Status, failure.Code, failure.Message)
			}
			if strings.Contains(failure.Message, "secret") {
				t.Fatalf("the message leaks the cause: %q", failure.Message)
			}
			if test.lookup == secret && !errors.Is(failure, secret) {
				t.Fatalf("the cause is lost: %v", failure.Cause)
			}
		})
	}
}

func TestUnknownPlatformCannotBeStreamed(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, nil)
	err := proxy.ServeManifest(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), Key{VideoID: "v", Platform: "nowhere"}, "")
	var failure *apierr.Error
	if !errors.As(err, &failure) || failure.Code != apierr.CodeStreamNotFound {
		t.Fatalf("unknown platform: %v", err)
	}
}

func TestShutdownRefusesNewLookups(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, func(o *Options) {
		o.Spawn = func(func(context.Context)) bool { return false }
	})
	err := proxy.ServeManifest(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), Key{VideoID: "v", Platform: defaultID}, "")
	var failure *apierr.Error
	if !errors.As(err, &failure) || failure.Code != apierr.CodeShuttingDown {
		t.Fatalf("lookup during shutdown: %v", err)
	}
}

func TestSourceLookupUsesAPrivateCookieJarForItsDuration(t *testing.T) {
	t.Parallel()
	upstream := hlsUpstream(t)
	var cleaned atomic.Bool
	var asked [2]string
	plat := &fakePlatform{id: defaultID}
	plat.manifest = func(_ context.Context, id, cookies string) (platform.Manifest, error) {
		if cookies != "/jars/user.txt" || cleaned.Load() {
			return platform.Manifest{}, fmt.Errorf("cookies %q (cleaned %v)", cookies, cleaned.Load())
		}
		return platform.Manifest{URL: upstream.URL + "/master.m3u8"}, nil
	}
	handler := api(newProxy(t, []*fakePlatform{plat}, func(o *Options) {
		o.Cookies = cookieFunc(func(userID, platformID string) (string, func(), error) {
			asked = [2]string{userID, platformID}
			return "/jars/user.txt", func() { cleaned.Store(true) }, nil
		})
	}))
	response := get(t, handler, "/api/stream/record1?source=mine")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "?source=mine") {
		t.Fatalf("source playback: %d %s", response.Code, response.Body)
	}
	if asked != [2]string{"user", "mine"} || !cleaned.Load() {
		t.Fatalf("cookie jar: asked %v cleaned %v", asked, cleaned.Load())
	}
}

func TestMissingCookiesFailTheSourceLookup(t *testing.T) {
	t.Parallel()
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: "https://video.example/master.m3u8"})}
	handler := api(newProxy(t, []*fakePlatform{plat}, func(o *Options) {
		o.Cookies = cookieFunc(func(string, string) (string, func(), error) { return "", nil, apierr.NotConnected() })
	}))
	response := get(t, handler, "/api/stream/record1?source=mine")
	if response.Code != http.StatusConflict || errorCode(t, response) != apierr.CodeNotConnected {
		t.Fatalf("source without a session: %d %s", response.Code, response.Body)
	}
}

func TestRememberedManifestSkipsThePlatform(t *testing.T) {
	t.Parallel()
	upstream := hlsUpstream(t)
	plat := &fakePlatform{id: defaultID}
	plat.manifest = func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{}, errors.New("platform must not be asked")
	}
	proxy := newProxy(t, []*fakePlatform{plat}, nil)
	proxy.Remember(Key{VideoID: "native-record1", RecordID: "record1", Platform: defaultID, UserID: "user", Revision: "1"}, platform.Manifest{URL: upstream.URL + "/master.m3u8"})
	if response := get(t, api(proxy), "/api/stream/record1"); response.Code != 200 || plat.calls.Load() != 0 {
		t.Fatalf("remembered manifest: %d %s (%d lookups)", response.Code, response.Body, plat.calls.Load())
	}
}

func TestActivePlaybackCannotExtendSignedManifestBeyondUpstreamExpiry(t *testing.T) {
	t.Parallel()
	for _, format := range []string{
		"https://manifest.example.test/api/manifest/hls/expire/%d/file/index.m3u8",
		"https://media.example.test/master.m3u8?expire=%d",
	} {
		clock := newClock()
		raw := fmt.Sprintf(format, clock.Now().Add(time.Minute).Unix())
		proxy := newProxy(t, []*fakePlatform{{id: defaultID}}, func(o *Options) { o.Now = clock.Now })
		key := Key{VideoID: raw, RecordID: "r", Platform: defaultID}
		proxy.Remember(key, platform.Manifest{URL: raw})
		clock.advance(20 * time.Second)
		if proxy.live(key) == nil {
			t.Fatalf("a manifest valid for 10 more seconds is gone: %s", raw)
		}
		clock.advance(11 * time.Second)
		if proxy.live(key) != nil {
			t.Fatalf("activity revived an expired signed source: %s", raw)
		}
	}
}

func TestManifestExpiryReadsTheSignature(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_900_000_000, 0)
	want := at.Add(-30 * time.Second)
	for raw, expected := range map[string]time.Time{
		"https://m.example/api/manifest/hls/expire/1900000000/file/index.m3u8": want,
		"https://m.example/master.m3u8?expire=1900000000":                      want,
		"https://m.example/master.m3u8":                                        {},
		"https://m.example/master.m3u8?expire=soon":                            {},
		"https://m.example/master.m3u8?expire=-5":                              {},
		"%zz": {},
	} {
		if got := manifestExpiry(raw); !got.Equal(expected) {
			t.Errorf("manifestExpiry(%q) = %v, want %v", raw, got, expected)
		}
	}
}

func TestExplicitPlaybackRetryRefreshesRejectedSignedManifest(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/expired.m3u8" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1920x1080\nvariant.m3u8\n"))
	}))
	t.Cleanup(upstream.Close)
	var healthy atomic.Bool
	plat := &fakePlatform{id: defaultID}
	plat.manifest = func(context.Context, string, string) (platform.Manifest, error) {
		if healthy.Load() {
			return platform.Manifest{URL: upstream.URL + "/master.m3u8"}, nil
		}
		return platform.Manifest{URL: upstream.URL + "/expired.m3u8"}, nil
	}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	first := get(t, handler, "/api/stream/record1")
	if first.Code != http.StatusBadGateway || errorCode(t, first) != apierr.CodeStreamUpstream {
		t.Fatalf("expired upstream was treated as playable: %d %s", first.Code, first.Body)
	}
	healthy.Store(true)
	retry := get(t, handler, "/api/stream/record1")
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), "/api/stream/record1/resource/") {
		t.Fatalf("retry kept the rejected signed source: %d %s", retry.Code, retry.Body)
	}
	if calls := plat.calls.Load(); calls != 2 {
		t.Fatalf("platform lookups: %d", calls)
	}
}

func TestUpstreamFailuresAreNotExposed(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "signed-url-details", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/master.m3u8"})}
	response := get(t, api(newProxy(t, []*fakePlatform{plat}, nil)), "/api/stream/record1")
	if response.Code != http.StatusBadGateway || errorCode(t, response) != apierr.CodeStreamUpstream || strings.Contains(response.Body.String(), "signed") {
		t.Fatalf("upstream failure: %d %s", response.Code, response.Body)
	}
}

func TestInvalidPlaylistsAreRefused(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"not a playlist": "<html>nope</html>",
		"too large":      "#EXTM3U\n" + strings.Repeat("#"+strings.Repeat("x", 1022)+"\n", (maxPlaylist/1024)+8),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(upstream.Close)
			plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/master.m3u8"})}
			response := get(t, api(newProxy(t, []*fakePlatform{plat}, nil)), "/api/stream/record1")
			if response.Code != http.StatusInternalServerError || errorCode(t, response) != apierr.CodeInternal {
				t.Fatalf("%d %.80s", response.Code, response.Body)
			}
		})
	}
}

// A segment that dies in the middle of the copy cannot be reported any more: the connection is cut so the
// player sees the truncation instead of a body that looks complete.
func TestMidCopyFailureAbortsTheConnection(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/master.m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\nsegment.ts\n"))
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(upstream.Close)
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/master.m3u8"})}
	front := httptest.NewServer(api(newProxy(t, []*fakePlatform{plat}, nil)))
	t.Cleanup(front.Close)
	master, err := http.Get(front.URL + "/api/stream/record1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(master.Body)
	master.Body.Close()
	found := resources(string(body))
	if len(found) != 1 {
		t.Fatalf("master: %s", body)
	}
	segment, err := http.Get(front.URL + found[0])
	if err != nil {
		return // cut before the headers reached the player: it saw the failure
	}
	defer segment.Body.Close()
	if _, err := io.ReadAll(segment.Body); err == nil {
		t.Fatal("a truncated segment looked complete to the player")
	}
}
