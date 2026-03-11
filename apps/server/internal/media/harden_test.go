package media

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestContentTypeOnlyPassesStreamingTypes(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"video/mp4":                       "video/mp4",
		"video/MP2T":                      "video/MP2T",
		"audio/mp4; codecs=\"mp4a.40.2\"": "audio/mp4; codecs=\"mp4a.40.2\"",
		"application/octet-stream":        "application/octet-stream",
		"application/mp4":                 "application/mp4",
		"text/vtt; charset=utf-8":         "text/vtt",
		"text/html":                       "application/octet-stream",
		"text/html; charset=utf-8":        "application/octet-stream",
		"image/svg+xml":                   "application/octet-stream",
		"application/xhtml+xml":           "application/octet-stream",
		"application/javascript":          "application/octet-stream",
		"text/plain":                      "application/octet-stream",
		"":                                "application/octet-stream",
		"not a media type":                "application/octet-stream",
	} {
		got, media := contentType(raw)
		if got != want {
			t.Errorf("contentType(%q) = %q, want %q", raw, got, want)
		}
		if media != (got != "application/octet-stream" || raw == "application/octet-stream") {
			t.Errorf("contentType(%q) media=%v", raw, media)
		}
	}
}

func TestManifestHeadersAreOnlyForTheirOwnHostAndTrustedMediaHosts(t *testing.T) {
	t.Parallel()
	manifest := platform.Manifest{URL: "https://manifest.example.test/master.m3u8", Headers: map[string]string{"User-Agent": "agent", "Cookie": "session=secret", "Authorization": "Bearer secret", "Proxy-Authorization": "secret"}}
	trusted := func(host string) bool { return host == "media.example.test" || host == "cdn.media.example.test" }
	for _, test := range []struct {
		name, remote string
		want         map[string]string
	}{
		{"manifest host", "https://manifest.example.test/seg.ts", manifest.Headers},
		{"manifest host in another case", "https://MANIFEST.example.test/seg.ts", manifest.Headers},
		{"foreign host", "https://evil.example.test/seg.ts", nil},
		{"platform media host without credentials", "https://media.example.test/seg.ts", map[string]string{"User-Agent": "agent"}},
		{"lookalike of a platform host", "https://media.example.test.evil.example.test/seg.ts", nil},
	} {
		remote, err := url.Parse(test.remote)
		if err != nil {
			t.Fatal(err)
		}
		got := requestHeaders(trusted, manifest, remote)
		if len(got) != len(test.want) {
			t.Errorf("%s: headers %v, want %v", test.name, got, test.want)
			continue
		}
		for name, value := range test.want {
			if got[name] != value {
				t.Errorf("%s: header %s = %q, want %q", test.name, name, got[name], value)
			}
		}
	}
}

func TestHardenSetsTheDocumentNeutralizingHeaders(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	harden(response)
	header := response.Header()
	if header.Get("Cache-Control") != "no-store" || header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
		t.Fatalf("headers: %v", header)
	}
}

// The default client refuses every non-public address, so a manifest (or a playlist) cannot aim the proxy at
// the internal network.
func TestMediaProxyCannotBeAimedAtTheInternalNetwork(t *testing.T) {
	t.Parallel()
	var reached atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\nsegment.ts\n"))
	}))
	t.Cleanup(internal.Close)
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: internal.URL + "/master.m3u8"})}
	handler := api(newProxy(t, []*fakePlatform{plat}, func(o *Options) { o.HTTP = nil }))
	response := get(t, handler, "/api/stream/record1")
	if response.Code == http.StatusOK || reached.Load() != 0 {
		t.Fatalf("the media proxy fetched an internal address: status=%d reached=%d", response.Code, reached.Load())
	}
	// A public manifest naming an internal resource is refused the same way.
	proxy := newProxy(t, []*fakePlatform{plat}, func(o *Options) { o.HTTP = nil })
	key := Key{VideoID: "v", RecordID: "r", Platform: defaultID}
	found := proxy.remember(key, platform.Manifest{URL: "https://video.example/master.m3u8"})
	found.resources.remember("internal", internal.URL+"/segment.ts")
	response = httptest.NewRecorder()
	if err := proxy.ServeResource(response, httptest.NewRequest(http.MethodGet, "/", nil), key, "internal", ""); err == nil || reached.Load() != 0 {
		t.Fatalf("an internal resource was fetched: err=%v reached=%d", err, reached.Load())
	}
}
