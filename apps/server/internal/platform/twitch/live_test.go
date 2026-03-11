package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// liveHandler answers Twitch's live lookup: a channel that is a key of live is known, and live when true.
func liveHandler(live map[string]bool, requests *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		requests.Add(1)
		var body struct {
			Variables struct {
				Login string `json:"login"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(payload, &body)
		switch isLive, known := live[body.Variables.Login]; {
		case !known:
			_, _ = io.WriteString(w, `{"data":{"user":null}}`)
		case isLive:
			_, _ = io.WriteString(w, `{"data":{"user":{"stream":{"id":"1","title":"Speedrun","viewersCount":321,"game":{"name":"wow","displayName":"World of Warcraft"}}}}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"user":{"stream":null}}}`)
		}
	}
}

func TestLiveStream(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	p := graphQLPlatform(t, liveHandler(map[string]bool{"zerator": true, "xqc": false}, &requests), Options{})
	stream, err := p.LiveStream(context.Background(), "zerator")
	if err != nil || !stream.Live || stream.VideoID != "zerator" || stream.Title != "Speedrun" || stream.Viewers != 321 || stream.Category != "World of Warcraft" {
		t.Fatalf("live channel: %+v %v", stream, err)
	}
	for _, login := range []string{"xqc", "nobody"} {
		if stream, err := p.LiveStream(context.Background(), login); err != nil || stream.Live || stream.VideoID != "" {
			t.Fatalf("%s must not be live: %+v %v", login, stream, err)
		}
	}
}

func TestLiveStreamFailuresAreReported(t *testing.T) {
	t.Parallel()
	_, err := deadPlatform(t).LiveStream(context.Background(), "zerator")
	assertCode(t, err, apierr.CodeRequestFailed)
	for name, body := range map[string]string{
		"graphql error": `{"errors":[{"message":"upstream"}],"data":null}`,
		"no data":       `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }, Options{})
			_, err := p.LiveStream(context.Background(), "zerator")
			assertCode(t, err, apierr.CodeRequestFailed)
		})
	}
}
