package app

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// contentCommentsDownloader answers the comment extraction of YouTube videos like the real downloader: the
// author of the comments says whose session asked (none, "Private" or "Other"), a few ids fail in the ways a
// downloader fails, and the failures write secrets to stderr that must never reach a client.
const contentCommentsDownloader = `case "$*" in
 *'/feed/subscriptions'*) printf '%s\n' '{"_type":"playlist","id":"subscriptions","title":"Subscriptions","extractor_key":"YoutubeTab","entries":[]}'; exit 0 ;;
esac
cookie=
previous=
for arg in "$@"; do
  if [ "$previous" = '--cookies' ]; then cookie=$arg; fi
  previous=$arg
done
case "$*" in
 *'watch?v=error000001'*) echo 'secret-cookie /tmp/private-cookies downloader error' >&2; exit 1 ;;
 *'watch?v=badjson0001'*) printf '%s\n' '{bad json'; exit 0 ;;
 *'watch?v=empty000001'*) printf '%s\n' '{"id":"empty000001","comments":[]}'; exit 0 ;;
 *'watch?v=denied00001'*) echo 'sign in secret-cookie /tmp/private-cookies' >&2; exit 1 ;;
esac
if [ -n "$cookie" ]; then
  case "$(cat "$cookie")" in *other-session*) author=Other ;; *) author=Private ;; esac
else
  author=Public
fi
printf '{"id":"dQw4w9WgXcQ","comments":[{"id":"comment1","author":"%s","text":"hello","timestamp":1700000000,"like_count":12,"parent":"root"},{"id":"reply1","author":"Reply","text":"reply","parent":"comment1"}]}\n' "$author"`

type contentComments struct {
	Comments   []platform.Comment `json:"comments"`
	NextCursor string             `json:"nextCursor"`
}

func TestCommentsArePublicAndConnectedOnesAreIsolatedPerAccount(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, contentCommentsDownloader)
	app, handler := newApp(t, option)
	_, token := viewerOf(t, handler)
	otherToken := tokenOf(t, testUser(t, app, "comments-other@example.test"))
	contentConnect(t, handler, token, contentCookies)
	contentConnect(t, handler, otherToken, strings.ReplaceAll(contentCookies, "fixture-session", "other-session"))

	path := "/api/video/" + storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"}) + "/comments"
	for _, tc := range []struct{ path, token, author string }{
		{path, "", "Public"},
		{path + "?source=youtube", token, "Private"},
		{path + "?source=youtube", otherToken, "Other"},
		{path, token, "Public"},
		{path, "", "Public"},
	} {
		res := request(t, handler, tc.path, tc.token)
		body := contentDecode[contentComments](t, res)
		if len(body.Comments) != 1 || body.NextCursor != "" {
			t.Fatalf("comments: %s", res.Body)
		}
		comment := body.Comments[0]
		if comment.ID != "comment1" || comment.Author != tc.author || comment.Text != "hello" || comment.PublishedAt != "2023-11-14T22:13:20Z" || comment.LikeCount != 12 || len(comment.Replies) != 1 {
			t.Fatalf("%s as %q: %s", tc.path, tc.author, res.Body)
		}
		if reply := comment.Replies[0]; reply.ID != "reply1" || reply.Author != "Reply" || reply.Text != "reply" || len(reply.Replies) != 0 {
			t.Fatalf("reply: %+v", reply)
		}
		if res.Header().Get("Cache-Control") != "no-store" || strings.Contains(res.Body.String(), "session") {
			t.Fatalf("unsafe answer: %v %s", res.Header(), res.Body)
		}
	}
	if res := anonymousRequest(t, handler, path+"?source=youtube"); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous connected request: %d %s", res.Code, res.Body)
	}
	// One extraction for the public list, one for each account's session.
	if extractions := calls.count("--get-comments"); extractions != 3 {
		t.Fatalf("%d comment extractions, want one public and one per account: %q", extractions, calls.all())
	}
	// The private cookie jars of the connected extractions do not outlive them.
	var jars []string
	for _, call := range calls.all() {
		if _, jar, found := strings.Cut(call, "--cookies "); found {
			jars = append(jars, strings.Fields(jar)[0])
		}
	}
	if len(jars) == 0 {
		t.Fatalf("the connected extractions used no cookie jar: %q", calls.all())
	}
	waitFor(t, "the cookie jars to be removed", func() bool {
		for _, jar := range jars {
			if _, err := os.Stat(jar); !os.IsNotExist(err) {
				return false
			}
		}
		return true
	})
}

func TestPublicCommentsAreFetchedOnceAndReused(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, contentCommentsDownloader)
	app, handler := newApp(t, option)
	path := "/api/video/" + storedVideo(t, app, platform.Video{ID: "dQw4w9WgXcQ"}) + "/comments"
	for range 3 {
		if res := request(t, handler, path, ""); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"author":"Public"`) {
			t.Fatalf("public comments: %d %s", res.Code, res.Body)
		}
	}
	if extractions := calls.count("--get-comments"); extractions != 1 {
		t.Fatalf("%d comment extractions, want 1: %q", extractions, calls.all())
	}
}

func TestCommentsRefuseUnsafeInputsAndHideDownloaderFailures(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, contentCommentsDownloader)
	app, handler := newApp(t, option)
	_, token := viewerOf(t, handler)
	commentsOf := func(id string) string {
		return "/api/video/" + storedVideo(t, app, platform.Video{ID: id}) + "/comments"
	}
	for _, id := range []string{"http:127.0.0.1", "-proxy"} {
		contentExpectError(t, request(t, handler, commentsOf(id), ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	}
	if len(calls.all()) != 0 {
		t.Fatalf("an invalid id launched the downloader: %q", calls.all())
	}
	path := commentsOf("dQw4w9WgXcQ")
	if res := request(t, handler, path+"?source=unknown", token); res.Code != http.StatusBadRequest {
		t.Fatalf("unknown source: %d %s", res.Code, res.Body)
	}
	contentExpectError(t, request(t, handler, path+"?source=youtube", token), http.StatusConflict, "SOURCE_NOT_CONNECTED")
	for _, id := range []string{"error000001", "badjson0001"} {
		res := request(t, handler, commentsOf(id), "")
		contentExpectError(t, res, http.StatusBadGateway, "SOURCE_REQUEST_FAILED")
		if strings.Contains(res.Body.String(), "secret-cookie") || strings.Contains(res.Body.String(), "/tmp/") {
			t.Fatalf("an unsafe downloader failure: %d %s", res.Code, res.Body)
		}
	}
	if res := request(t, handler, commentsOf("empty000001"), ""); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"comments":[]`) || strings.Contains(res.Body.String(), "nextCursor") {
		t.Fatalf("empty comments: %d %s", res.Code, res.Body)
	}

	contentConnect(t, handler, token, contentCookies)
	res := request(t, handler, commentsOf("denied00001")+"?source=youtube", token)
	contentExpectError(t, res, http.StatusUnauthorized, "SOURCE_SESSION_INVALID")
	if strings.Contains(res.Body.String(), "secret-cookie") || strings.Contains(res.Body.String(), "/tmp/") {
		t.Fatalf("an unsafe connected failure: %d %s", res.Code, res.Body)
	}
}

func TestVideosWithoutACommentListHaveNone(t *testing.T) {
	t.Parallel()
	option, calls := contentDownloader(t, "exit 1")
	app, handler := newApp(t, option)
	// A Twitch VOD's conversation is its chat, not a list.
	vod := storedVideo(t, app, platform.Video{ID: "123456", Platform: "twitch", ChannelID: "channel", ChannelName: "Channel"})
	contentExpectError(t, request(t, handler, "/api/video/"+vod+"/comments", ""), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
	if len(calls.all()) != 0 {
		t.Fatalf("the downloader was asked for a comment list that does not exist: %q", calls.all())
	}
}
