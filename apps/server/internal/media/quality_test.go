package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestQualitySwitchKeepsPlaylistsAndInitializationAcrossSegmentEviction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.URL.Path == "/master.m3u8" {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"English\",DEFAULT=YES,AUTOSELECT=YES,URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=100,RESOLUTION=640x360,AUDIO=\"audio\"\n360.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=200,RESOLUTION=1280x720,AUDIO=\"audio\"\n720.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=300,RESOLUTION=1920x1080,AUDIO=\"audio\"\n1080.m3u8\n")
			return
		}
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			quality := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".m3u8")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MAP:URI=\"%s-init.mp4\"\n", quality)
			for i := range 100 {
				fmt.Fprintf(w, "#EXTINF:2,\n%s-segment-%d.m4s\n", quality, i)
			}
			fmt.Fprint(w, "#EXT-X-ENDLIST\n")
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(strings.TrimPrefix(r.URL.Path, "/")))
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: upstream.URL + "/master.m3u8"}, nil
	}}
	proxy := newProxy(t, []*fakePlatform{plat}, func(o *Options) { o.MaxResources = 50 })
	handler := api(proxy)
	master := get(t, handler, "/api/stream/record1")
	variants := resources(master.Body.String())
	if len(variants) != 3 {
		t.Fatalf("master: %d %s", master.Code, master.Body)
	}
	audioURI := playlistURI.FindStringSubmatch(master.Body.String())
	if len(audioURI) != 2 {
		t.Fatalf("missing audio playlist: %s", master.Body)
	}
	playlists := append(append([]string{}, variants...), audioURI[1])
	names := []string{"360", "720", "1080", "audio"}
	allSegments := make([][]string, len(playlists))
	var firstInit string
	for i, path := range playlists {
		variant := get(t, handler, path)
		if variant.Code != 200 {
			t.Fatalf("quality %d playlist disappeared after segment eviction: %d %s", i, variant.Code, variant.Body)
		}
		initialization := playlistURI.FindStringSubmatch(variant.Body.String())
		if len(initialization) != 2 {
			t.Fatalf("missing initialization: %s", variant.Body)
		}
		if i == 0 {
			firstInit = initialization[1]
		}
		segments := resources(variant.Body.String())
		if len(segments) != 100 {
			t.Fatalf("segment count: %d", len(segments))
		}
		allSegments[i] = segments
		expected := names[i] + "-segment-0.m4s"
		segment := get(t, handler, segments[0])
		if segment.Code != 200 || segment.Body.String() != expected {
			t.Fatalf("quality %d media: %d %q", i, segment.Code, segment.Body.String())
		}
	}
	for i := range 75 {
		for _, segments := range allSegments {
			response := get(t, handler, segments[i])
			if response.Code != http.StatusOK {
				t.Fatalf("segment churn: %d %s", response.Code, response.Body)
			}
		}
	}
	for i, path := range playlists {
		playlist := get(t, handler, path)
		if playlist.Code != http.StatusOK {
			t.Fatalf("%s playlist after churn: %d %s", names[i], playlist.Code, playlist.Body)
		}
		for _, index := range []int{0, 50} {
			expected := fmt.Sprintf("%s-segment-%d.m4s", names[i], index)
			response := get(t, handler, allSegments[i][index])
			if response.Code != http.StatusOK || response.Body.String() != expected {
				t.Fatalf("%s segment %d after churn: %d %q", names[i], index, response.Code, response.Body.String())
			}
			response = get(t, handler, allSegments[i][index], "Range", "bytes=4-10")
			if response.Code != http.StatusPartialContent || response.Body.String() != expected[4:11] ||
				response.Header().Get("Content-Range") != fmt.Sprintf("bytes 4-10/%d", len(expected)) {
				t.Fatalf("%s segment %d seek: %d %q, headers %v", names[i], index, response.Code, response.Body.String(), response.Header())
			}
		}
	}
	initialization := get(t, handler, firstInit)
	if initialization.Code != 200 || initialization.Body.String() != "360-init.mp4" {
		t.Fatalf("initialization was evicted with segments: %d %q", initialization.Code, initialization.Body.String())
	}
	key := Key{VideoID: "native-record1", RecordID: "record1", Platform: defaultID, UserID: "user", Revision: "1"}
	if size := proxy.live(key).resources.len(); size > 50 {
		t.Fatalf("resource cache exceeded bound: %d", size)
	}
}

func TestSegmentTokensAreScopedToAccountAndRevision(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:2,\nsegment.ts\n#EXT-X-ENDLIST\n")
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		fmt.Fprint(w, "segment")
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: fixedManifest(platform.Manifest{URL: upstream.URL + "/master.m3u8"})}
	proxy := newProxy(t, []*fakePlatform{plat}, func(o *Options) { o.MaxResources = 1 })
	owner := Key{VideoID: "v", RecordID: "r", Platform: defaultID, UserID: "alice", Revision: "1"}
	manifest := func(key Key) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		if err := proxy.ServeManifest(recorder, httptest.NewRequest(http.MethodGet, "/", nil), key, ""); err != nil {
			t.Fatal(err)
		}
		paths := resources(recorder.Body.String())
		if len(paths) != 1 {
			t.Fatalf("manifest: %s", recorder.Body)
		}
		return strings.Split(strings.Split(paths[0], "/resource/")[1], "?")[0]
	}
	id := manifest(owner)
	for name, other := range map[string]Key{
		"account":  {VideoID: "v", RecordID: "r", Platform: defaultID, UserID: "bob", Revision: "1"},
		"revision": {VideoID: "v", RecordID: "r", Platform: defaultID, UserID: "alice", Revision: "2"},
	} {
		manifest(other)
		err := proxy.ServeResource(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), other, id, "")
		var failure *apierr.Error
		if !errors.As(err, &failure) || failure.Code != apierr.CodeResourceExpired {
			t.Errorf("%s accepted another key's segment token: %v", name, err)
		}
		if size := proxy.live(other).resources.len(); size > 1 {
			t.Errorf("%s cache exceeded bound: %d", name, size)
		}
	}
	recorder := httptest.NewRecorder()
	if err := proxy.ServeResource(recorder, httptest.NewRequest(http.MethodGet, "/", nil), owner, id, ""); err != nil || recorder.Body.String() != "segment" {
		t.Fatalf("owner segment: %q, %v", recorder.Body.String(), err)
	}
}
