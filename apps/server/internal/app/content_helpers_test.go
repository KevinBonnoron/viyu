package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// The content tests drive search, videos, channels, comments and live status through the HTTP application.
// Their helpers are shared by the content_*_test.go files and carry the "content" prefix.

// contentTwitchID is the public web client identifier Twitch's own site uses; the platform sends it without an
// account, and the fixtures check it so a request that carried credentials would be caught.
const contentTwitchID = "ue6666qo983tsx6so1t0vnawi233wa"

// contentDownloader is withDownloader that also records every invocation (the arguments of one per line), so
// a test can tell that a route did, or did not, ask the downloader.
func contentDownloader(t *testing.T, body string) (func(*Options), *contentCalls) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls")
	option := withDownloader(t, "printf '%s\\n' \"$*\" >> '"+log+"'\n"+body)
	return option, &contentCalls{path: log}
}

// contentCalls reads the log of a contentDownloader.
type contentCalls struct{ path string }

// all lists the arguments of every call so far.
func (c *contentCalls) all() []string {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// count is the number of calls so far whose arguments contain every one of parts.
func (c *contentCalls) count(parts ...string) int {
	n := 0
	for _, call := range c.all() {
		matches := true
		for _, part := range parts {
			matches = matches && strings.Contains(call, part)
		}
		if matches {
			n++
		}
	}
	return n
}

// contentDecode decodes the body of a successful answer.
func contentDecode[T any](t *testing.T, res *httptest.ResponseRecorder) T {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body)
	}
	var value T
	if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
		t.Fatalf("%v: %s", err, res.Body)
	}
	return value
}

// contentExpectError checks the one error envelope: the status, the code, and nothing else but a message.
func contentExpectError(t *testing.T, res *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("status=%d, the body is not JSON: %s", res.Code, res.Body)
	}
	if res.Code != status || body["code"] != code || body["message"] == "" || len(body) != 2 {
		t.Fatalf("want %d %s, got status=%d body=%s", status, code, res.Code, res.Body)
	}
}

// contentPage is a page of search results as the API answers it.
type contentPage struct {
	Results []map[string]any `json:"results"`
	Page    int              `json:"page"`
	HasMore bool             `json:"hasMore"`
}

// contentListing is the answer of the channel route.
type contentListing struct {
	Channel platform.Channel `json:"channel"`
	Videos  []platform.Video `json:"videos"`
}

// contentLive is the answer of the channel live route.
type contentLive struct {
	Live     bool    `json:"live"`
	RecordID string  `json:"recordId"`
	Title    string  `json:"title"`
	Viewers  float64 `json:"viewers"`
	Category string  `json:"category"`
}

// contentPlatformsOf lists the platforms of the results of a page, in order, without consecutive repeats.
func contentPlatformsOf(page contentPage) []string {
	var seen []string
	for _, result := range page.Results {
		name, _ := result["platform"].(string)
		if len(seen) == 0 || seen[len(seen)-1] != name {
			seen = append(seen, name)
		}
	}
	return seen
}

// contentTwitchServer starts handler as the GraphQL endpoint of Twitch and returns the option that points the
// application at it, with the counter of the requests it received.
func contentTwitchServer(t *testing.T, handler http.HandlerFunc) (func(*Options), *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return func(o *Options) {
		o.Twitch.HTTP = server.Client()
		o.Twitch.GraphQLURL = server.URL
	}, &requests
}

// contentSubscribe subscribes a profile to a channel of the registry (stored as a listing would) and returns
// the subscription and the channel record id.
func contentSubscribe(t *testing.T, app core.App, profile *core.Record, platformID, nativeID, name string) (*core.Record, string) {
	t.Helper()
	channelID := storedChannel(t, app, platformID, nativeID, name)
	collection, err := app.FindCollectionByNameOrId("subscriptions")
	if err != nil {
		t.Fatal(err)
	}
	subscription := core.NewRecord(collection)
	subscription.Load(map[string]any{"profile": profile.Id, "channel": channelID})
	if err := app.Save(subscription); err != nil {
		t.Fatal(err)
	}
	return subscription, channelID
}

// contentProfile creates one more profile of the account.
func contentProfile(t *testing.T, app core.App, user *core.Record, name string) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("profiles")
	if err != nil {
		t.Fatal(err)
	}
	profile := core.NewRecord(collection)
	profile.Load(map[string]any{"user": user.Id, "name": name})
	if err := app.Save(profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

// contentCookies is a YouTube cookie export that passes the connection check.
const contentCookies = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tLOGIN_INFO\tfixture-login\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tfixture-session\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSAPISID\tfixture-sapisid\n"

// contentConnect connects the YouTube source of the account with the cookies and fails the test when the
// application refuses them.
func contentConnect(t *testing.T, handler http.Handler, token, cookies string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"cookies": cookies})
	if err != nil {
		t.Fatal(err)
	}
	if res := send(t, handler, http.MethodPost, "/api/sources/youtube/connect", token, string(body)); res.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", res.Code, res.Body)
	}
}
