package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// streamResourceURI finds the URI="..." attributes of a rewritten playlist.
var streamResourceURI = regexp.MustCompile(`URI="([^"]+)"`)

// streamUpstream is the media host behind the fixture manifest: a master playlist, a variant playlist whose
// init segment and media segment are binary, seekable bytes.
func streamUpstream(t *testing.T) *httptest.Server {
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

// streamYouTubeDownloader is a yt-dlp that answers the connection check of a YouTube session with an empty
// feed and every video with one whose only HLS format is manifestURL. The title tells whether the request
// carried a session (a --cookies file).
func streamYouTubeDownloader(manifestURL string) string {
	return strings.ReplaceAll(`case "$*" in
 *'/feed/subscriptions'*) printf '%s\n' '{"_type":"playlist","id":"subscriptions","title":"Subscriptions","extractor_key":"YoutubeTab","entries":[]}'; exit 0 ;;
esac
case "$*" in *--cookies*) title=Private ;; *) title=Public ;; esac
printf '{"id":"dQw4w9WgXcQ","title":"%s video","channel_id":"UCfixture","channel":"Fixture channel","duration":70,"upload_date":"20260102","formats":[{"protocol":"m3u8_native","height":1080,"manifest_url":"%s"}]}\n' "$title" 'MANIFEST'`, "MANIFEST", manifestURL)
}

// withLoopbackMedia lets the media proxy reach the loopback servers the tests use as upstreams; the
// production client refuses every non-public address.
func withLoopbackMedia(o *Options) { o.MediaHTTP = &http.Client{} }

// streamResources lists the same-origin resource addresses of a rewritten playlist, in order.
func streamResources(body string) []string {
	var found []string
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "/api/stream/") {
			found = append(found, line)
		}
	}
	return found
}

// streamHardened asserts the headers every /api/stream answer carries, errors included.
func streamHardened(t *testing.T, label string, res *httptest.ResponseRecorder) {
	t.Helper()
	header := res.Header()
	if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Cache-Control") != "no-store" || header.Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
		t.Errorf("%s: the answer is not hardened: %d %v", label, res.Code, header)
	}
}

func TestHLSBinarySegmentsRemainSeekableUnderManifestPaths(t *testing.T) {
	t.Parallel()
	upstream := streamUpstream(t)
	app, handler := newApp(t, withDownloader(t, streamYouTubeDownloader(upstream.URL+"/api/manifest/hls_playlist/master.m3u8")), withLoopbackMedia)
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})

	master := request(t, handler, "/api/stream/"+video, "")
	if master.Code != http.StatusOK || master.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("HLS master unavailable: %d %s", master.Code, master.Body)
	}
	streamHardened(t, "master", master)
	if strings.Contains(master.Body.String(), "dQw4w9WgXcQ") || strings.Contains(master.Body.String(), upstream.URL) {
		t.Fatalf("the rewritten master names the native id or the upstream: %s", master.Body)
	}
	resources := streamResources(master.Body.String())
	if len(resources) != 1 || !strings.HasPrefix(resources[0], "/api/stream/"+video+"/resource/") || strings.Contains(resources[0], "platform=") {
		t.Fatalf("the master must point at the proxy by record id: %v", resources)
	}

	variant := request(t, handler, resources[0], "")
	if variant.Code != http.StatusOK {
		t.Fatalf("HLS variant unavailable: %d %s", variant.Code, variant.Body)
	}
	streamHardened(t, "variant", variant)
	initialization := streamResourceURI.FindStringSubmatch(variant.Body.String())
	if len(initialization) != 2 || !strings.HasPrefix(initialization[1], "/api/stream/") {
		t.Fatalf("initialization escaped the same-origin proxy: %s", variant.Body)
	}
	if init := request(t, handler, initialization[1], ""); init.Code != http.StatusOK || init.Body.String() != "authenticated-media" {
		t.Fatalf("initialization bytes corrupted: %d %s", init.Code, init.Body)
	}

	segments := streamResources(variant.Body.String())
	if len(segments) != 1 {
		t.Fatalf("variant resources: %v", segments)
	}
	seek := httptest.NewRequest(http.MethodGet, segments[0], nil)
	seek.Header.Set("Range", "bytes=5-9")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, seek)
	if response.Code != http.StatusPartialContent || response.Header().Get("Content-Range") != "bytes 5-9/19" || response.Body.String() != "ntica" {
		t.Fatalf("byte-range media was mistaken for a playlist or could not be sought: %d %s %q", response.Code, response.Header().Get("Content-Range"), response.Body.String())
	}
	streamHardened(t, "segment", response)
}

// Everything under /api/stream is third-party bytes, so even a refusal carries the headers that keep a browser
// from interpreting it as a document.
func TestStreamErrorsAreHardenedJSONEnvelopes(t *testing.T) {
	t.Parallel()
	upstream := streamUpstream(t)
	app, handler := newApp(t, withDownloader(t, streamYouTubeDownloader(upstream.URL+"/master.m3u8")), withLoopbackMedia)
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	for name, test := range map[string]struct {
		path   string
		status int
		code   string
	}{
		"unknown video":     {"/api/stream/nosuchrecord0001", http.StatusNotFound, "STREAM_NOT_FOUND"},
		"unknown resource":  {"/api/stream/" + video + "/resource/0123456789abcdef", http.StatusNotFound, "STREAM_RESOURCE_EXPIRED"},
		"mismatched source": {"/api/stream/" + video + "?source=twitch", http.StatusBadRequest, "INVALID_SOURCE_INPUT"},
	} {
		res := request(t, handler, test.path, "")
		streamHardened(t, name, res)
		if res.Code != test.status || !strings.Contains(res.Body.String(), `"code":"`+test.code+`"`) || strings.Contains(res.Body.String(), `"error"`) {
			t.Errorf("%s: %d %s", name, res.Code, res.Body)
		}
	}
}

// A downloader that cannot name a playable format is a gateway failure that says nothing about the downloader.
func TestStreamLookupFailureIsAGenericGatewayError(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, withDownloader(t, "echo 'ERROR: secret failure /home/user/.cache' >&2; exit 1"))
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	res := request(t, handler, "/api/stream/"+video, "")
	streamHardened(t, "failed lookup", res)
	if res.Code != http.StatusBadGateway || !strings.Contains(res.Body.String(), `"code":"SOURCE_REQUEST_FAILED"`) || strings.Contains(res.Body.String(), "secret") || strings.Contains(res.Body.String(), "yt-dlp") {
		t.Fatalf("a failed lookup must be a generic gateway failure: %d %s", res.Code, res.Body)
	}
}

// With no MediaHTTP the application fetches media with the hardened public-Internet client: a manifest that
// names an internal address is never fetched.
func TestMediaProxyCannotBeAimedAtTheInternalNetwork(t *testing.T) {
	t.Parallel()
	var reached atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte("internal"))
	}))
	t.Cleanup(internal.Close)
	app, handler := newApp(t, withDownloader(t, streamYouTubeDownloader(internal.URL+"/master.m3u8")))
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	res := request(t, handler, "/api/stream/"+video, "")
	if res.Code == http.StatusOK || reached.Load() != 0 {
		t.Fatalf("the media proxy fetched an internal address: status=%d reached=%d", res.Code, reached.Load())
	}
	streamHardened(t, "internal manifest", res)
}
