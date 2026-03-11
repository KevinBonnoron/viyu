package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/chat"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// One account holds a bounded number of live chat streams open, and cannot use up another account's.
func TestChatLiveRefusesAnAccountOverItsConnectionCap(t *testing.T) {
	t.Parallel()
	irc := newChatIRC(t, true)
	app, handler := newApp(t, withChatIRC(irc, func(o *chat.Options) { o.MaxPerUser = 1 }))
	server := chatServer(t, handler)
	_, token := viewerOf(t, handler)
	first := openChatStream(t, server, "/api/chat/live/first", token)
	if name, _ := first.event(t); name != "ready" {
		t.Fatalf("first stream: %q", name)
	}
	response := chatGet(t, server+"/api/chat/live/second", token)
	var failure struct{ Code, Message string }
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil || response.StatusCode != http.StatusTooManyRequests || failure.Code != "CHAT_TOO_MANY_CONNECTIONS" {
		t.Fatalf("second stream of one account: %d %+v %v", response.StatusCode, failure, err)
	}
	other := testUser(t, app, "chat-other@example.test")
	second := openChatStream(t, server, "/api/chat/live/second", tokenOf(t, other))
	if name, _ := second.event(t); name != "ready" {
		t.Fatalf("another account's stream: %q", name)
	}
	// Closing the first stream gives its slot back.
	_ = first.response.Body.Close()
	waitFor(t, "the closed stream gives its slot back", func() bool {
		return chatGet(t, server+"/api/chat/live/third", token).StatusCode == http.StatusOK
	})
}

// Concurrent requests for the comments of one video share one downloader run, whoever asks, and the answer is
// kept for the requests that follow.
func TestCommentFetchIsSharedByConcurrentRequests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	calls, release := filepath.Join(dir, "calls"), filepath.Join(dir, "release")
	script := `printf 'call\n' >> '` + calls + `'
until [ -e '` + release + `' ]; do sleep 0.02; done
printf '{"id":"dQw4w9WgXcQ","comments":[{"id":"one","author":"Public","text":"hello","parent":"root"}]}\n'`
	app, handler := newApp(t, withDownloader(t, script))
	video := storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"})
	viewerOf(t, handler) // the account is made on this goroutine, not lazily inside the concurrent requests
	count := func() int {
		data, _ := os.ReadFile(calls)
		return strings.Count(string(data), "call\n")
	}

	answers := make([]*httptest.ResponseRecorder, 3)
	var asked sync.WaitGroup
	ask := func(i int) {
		asked.Add(1)
		go func() {
			defer asked.Done()
			answers[i] = request(t, handler, "/api/video/"+video+"/comments", "")
		}()
	}
	ask(0)
	waitFor(t, "the downloader started", func() bool { return count() == 1 })
	ask(1)
	ask(2)
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	asked.Wait()
	for i, res := range answers {
		var body struct {
			Comments []map[string]any `json:"comments"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != http.StatusOK || len(body.Comments) != 1 {
			t.Fatalf("request %d: %d %s %v", i, res.Code, res.Body, err)
		}
	}
	if res := request(t, handler, "/api/video/"+video+"/comments", ""); res.Code != http.StatusOK {
		t.Fatalf("later request: %d %s", res.Code, res.Body)
	}
	if got := count(); got != 1 {
		t.Fatalf("extractions: %d, want 1", got)
	}
}

// The rate limiter the migrations configure is live in the application, and playback gets a limit of its own:
// HLS makes many small requests, so a viewer playing a video must not exhaust the allowance of the rest of the API.
func TestStreamRoutesHaveTheirOwnRateLimit(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	settings := app.Settings()
	settings.RateLimits.Enabled = true
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	// Account creation is not what is measured: sign the viewer in before counting.
	viewerOf(t, handler)

	limited := 0
	for i := 1; i <= 700 && limited == 0; i++ {
		if request(t, handler, "/api/health", "").Code == http.StatusTooManyRequests {
			limited = i
		}
	}
	if limited == 0 || limited > 601 {
		t.Fatalf("the API allowance was not enforced: first refusal at request %d", limited)
	}
	for i := range 700 {
		res := request(t, handler, "/api/stream/nosuchrecord0001", "")
		if res.Code != http.StatusNotFound {
			t.Fatalf("stream request %d was %d: the playback allowance must not share the API's: %s", i, res.Code, res.Body)
		}
	}
}
