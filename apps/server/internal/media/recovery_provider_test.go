package media

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestTwitchOpaquePlaylistAndSegmentRollover(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := "old"
		if generation.Load() == 2 {
			current = "new"
		}
		if strings.Contains(r.URL.Path, "/old-") && current == "new" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		base := "http://video-edge.example.hls.ttvnw.net"
		switch {
		case r.URL.Path == "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100,RESOLUTION=640x360\n%s/v0/%s-low/index-live.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720\n%s/v0/%s-high/index-live.m3u8\n", base, current, base, current)
		case strings.HasPrefix(r.URL.Path, "/v0/"):
			quality := "low"
			if strings.Contains(r.URL.Path, "-high/") {
				quality = "high"
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:42\n#EXT-X-PROGRAM-DATE-TIME:2026-01-02T03:04:05Z\n#EXTINF:6,live\n%s/v1/segment/%s-%s.ts\n", base, current, quality)
		case strings.Contains(r.URL.Path, "-high.ts"):
			fmt.Fprint(w, "correct 720p segment")
		default:
			fmt.Fprint(w, "wrong quality")
		}
	}))
	defer upstream.Close()
	address := strings.TrimPrefix(upstream.URL, "http://")
	transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}}
	defer transport.CloseIdleConnections()
	plat := &fakePlatform{id: "twitch", manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%d", upstream.URL, generation.Load())}, nil
	}}
	proxy := newProxy(t, []*fakePlatform{{id: defaultID}, plat}, func(o *Options) { o.HTTP = &http.Client{Transport: transport} })
	handler := api(proxy)
	master := get(t, handler, "/api/stream/record1?platform=twitch")
	variants := resources(master.Body.String())
	if len(variants) != 2 {
		t.Fatalf("master: %d %s", master.Code, master.Body)
	}
	variant := get(t, handler, variants[1])
	segments := resources(variant.Body.String())
	if len(segments) != 1 {
		t.Fatalf("variant: %d %s", variant.Code, variant.Body)
	}
	generation.Store(2)
	response := get(t, handler, segments[0])
	if response.Code != http.StatusOK || response.Body.String() != "correct 720p segment" || plat.calls.Load() != 2 {
		t.Fatalf("rollover: %d %q, calls %d", response.Code, response.Body.String(), plat.calls.Load())
	}
}

func TestYouTubeSignedPathIdentityRetainsRenditionAndSegment(t *testing.T) {
	old := "https://rr1.googlevideo.com/api/manifest/hls/upn/old/id/video/itag/137/expire/100/signature/old/file/seg.ts?sig=old"
	fresh := "https://rr1.googlevideo.com/api/manifest/hls/upn/old/id/video/itag/137/expire/200/signature/new/file/seg.ts?sig=new"
	otherQuality := strings.Replace(fresh, "/itag/137/", "/itag/140/", 1)
	otherSegment := strings.Replace(fresh, "/seg.ts", "/other.ts", 1)
	if resourceIdentity(old) != resourceIdentity(fresh) {
		t.Fatal("signature rollover changed YouTube resource identity")
	}
	if resourceIdentity(old) == resourceIdentity(otherQuality) || resourceIdentity(old) == resourceIdentity(otherSegment) {
		t.Fatal("YouTube identity lost the rendition or segment")
	}
}
