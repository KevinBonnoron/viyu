package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// accessPlatform answers Twitch's GraphQL like the real thing for the playback access token: restricted
// VODs list their qualities as restricted, public ones list none, unknown ones have no token.
func accessPlatform(t *testing.T, restricted map[string]bool, requests *atomic.Int32) *Platform {
	t.Helper()
	pattern := regexp.MustCompile(`(v\d+): videoPlaybackAccessToken\(id: "(\d+)"`)
	return graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		var body struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(payload, &body)
		requests.Add(1)
		data := map[string]any{}
		for _, match := range pattern.FindAllStringSubmatch(body.Query, -1) {
			alias, id := match[1], match[2]
			bitrates, known := restricted[id]
			switch {
			case !known:
				data[alias] = nil
			case bitrates:
				data[alias] = map[string]string{"value": `{"chansub":{"restricted_bitrates":["chunked","720p60"]},"vod_id":` + id + `}`}
			default:
				data[alias] = map[string]string{"value": `{"chansub":{"restricted_bitrates":[]},"vod_id":` + id + `}`}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}, Options{})
}

func TestSubscriberOnlyLookupTellsRestrictedPublicAndUnknownVODsApart(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	p := accessPlatform(t, map[string]bool{"111": true, "222": false}, &requests)
	flags := p.subscriberOnly(context.Background(), []string{"111", "222", "333"})
	if len(flags) != 2 || !flags["111"] || flags["222"] {
		t.Fatalf("flags: %v", flags)
	}
	if _, known := flags["333"]; known {
		t.Fatal("a VOD Twitch gave no token for must stay unknown, not public")
	}

	video := platform.Video{ID: "333", Platform: ID, SubscriberOnly: true}
	applySubscriberFlag(&video, flags)
	if video.SubscriberChecked || !video.SubscriberOnly {
		t.Fatalf("an unknown restriction must leave the video as it was: %+v", video)
	}
	other := platform.Video{ID: "111", Platform: "elsewhere"}
	applySubscriberFlag(&other, flags)
	if other.SubscriberOnly || other.SubscriberChecked {
		t.Fatalf("only Twitch VODs have this restriction: %+v", other)
	}
	known := platform.Video{ID: "111", Platform: ID}
	applySubscriberFlag(&known, flags)
	if !known.SubscriberOnly || !known.SubscriberChecked {
		t.Fatalf("a looked-up restriction was not recorded: %+v", known)
	}
}

func TestSubscriberOnlyLookupBatchesRequests(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	restricted := map[string]bool{}
	ids := make([]string, 0, accessBatch+1)
	for i := 0; i <= accessBatch; i++ {
		id := fmt.Sprint(1000 + i)
		ids = append(ids, id)
		restricted[id] = i%2 == 0
	}
	flags := accessPlatform(t, restricted, &requests).subscriberOnly(context.Background(), ids)
	if requests.Load() != 2 {
		t.Fatalf("%d VODs took %d requests, want one per %d", len(ids), requests.Load(), accessBatch)
	}
	if len(flags) != len(ids) {
		t.Fatalf("%d answers for %d VODs", len(flags), len(ids))
	}
	for _, id := range ids {
		if flags[id] != restricted[id] {
			t.Fatalf("VOD %s was mixed up across batches: got %v, want %v", id, flags[id], restricted[id])
		}
	}
}

func TestSubscriberOnlyLookupFailureKnowsNothing(t *testing.T) {
	t.Parallel()
	if flags := deadPlatform(t).subscriberOnly(context.Background(), []string{"111"}); len(flags) != 0 {
		t.Fatalf("a failed lookup claimed to know: %v", flags)
	}
}

func TestMarkSubscriberOnlyFlagsListingsAndSkipsWhatIsNotAVOD(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	p := accessPlatform(t, map[string]bool{"111": true, "222": false}, &requests)
	videos := []platform.Video{{ID: "111", Platform: ID}, {ID: "222", Platform: ID}, {ID: "xqc", Platform: ID, Live: true}, {ID: "333", Platform: ID}}
	p.markSubscriberOnly(context.Background(), videos)
	if !videos[0].SubscriberOnly || !videos[0].SubscriberChecked || videos[1].SubscriberOnly || !videos[1].SubscriberChecked || videos[2].SubscriberChecked || videos[3].SubscriberChecked {
		t.Fatalf("flags: %+v", videos)
	}
	if requests.Load() != 1 {
		t.Fatalf("%d requests", requests.Load())
	}
	// A listing of streams only does not ask Twitch at all.
	p.markSubscriberOnly(context.Background(), []platform.Video{{ID: "xqc", Platform: ID}})
	if requests.Load() != 1 {
		t.Fatalf("a login is not a VOD, yet %d requests", requests.Load())
	}
	if !strings.Contains(subscriberQuery([]string{"1", "2"}), `v1: videoPlaybackAccessToken(id: "2"`) {
		t.Fatal("query aliases")
	}
}
