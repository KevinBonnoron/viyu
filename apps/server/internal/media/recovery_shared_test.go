package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestRecoveryDoesNotConfuseSegmentsSharingAByteRangeResource(t *testing.T) {
	var renewed atomic.Bool
	var substituted atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			first, second := "shared.ts?sig=old", "shared.ts?sig=old"
			if renewed.Load() {
				first, second = "first.ts?sig=new", "second.ts?sig=new"
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:42\n#EXT-X-PROGRAM-DATE-TIME:2026-11-23T01:00:00Z\n#EXTINF:2,\n#EXT-X-BYTERANGE:4@0\n%s\n#EXT-X-PROGRAM-DATE-TIME:2026-11-23T01:00:02Z\n#EXTINF:2,\n#EXT-X-BYTERANGE:4@4\n%s\n#EXT-X-ENDLIST\n", first, second)
			return
		}
		if renewed.Load() && r.URL.Path == "/shared.ts" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		substituted.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		body := "old-data"
		if r.URL.Path == "/first.ts" {
			body = "FIRSTxxx"
		} else if r.URL.Path == "/second.ts" {
			body = "xxxxLAST"
		}
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(body))
	}))
	defer upstream.Close()
	plat := &fakePlatform{id: defaultID, manifest: func(context.Context, string, string) (platform.Manifest, error) {
		return platform.Manifest{URL: fmt.Sprintf("%s/master.m3u8?sig=%t", upstream.URL, renewed.Load())}, nil
	}}
	handler := api(newProxy(t, []*fakePlatform{plat}, nil))
	master := get(t, handler, "/api/stream/record1")
	paths := resources(master.Body.String())
	if len(paths) != 2 {
		t.Fatalf("shared resource playlist: %s", master.Body)
	}
	renewed.Store(true)
	response := get(t, handler, paths[0], "Range", "bytes=0-3")
	if response.Code != http.StatusPartialContent || response.Body.String() != "FIRS" || substituted.Load() != 1 {
		t.Fatalf("first shared-resource recovery: status %d, body %q, media fetches %d", response.Code, response.Body.String(), substituted.Load())
	}
	response = get(t, handler, paths[1], "Range", "bytes=4-7")
	if response.Code != http.StatusPartialContent || response.Body.String() != "LAST" {
		t.Fatalf("second shared-resource recovery: status %d, body %q", response.Code, response.Body.String())
	}
}
