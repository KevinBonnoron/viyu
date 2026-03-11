package twitch

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestCookieImportFiltersDomainsAndRequiresAuthToken(t *testing.T) {
	t.Parallel()
	connector := newPlatform(t, Options{}).Connector()
	valid := cookiesFor("private-twitch-token", time.Now().Add(time.Hour).Unix())
	normalized, err := connector.NormalizeCookies(valid)
	if err != nil || !strings.Contains(normalized, "#HttpOnly_.twitch.tv") || strings.Contains(normalized, "example.net") || strings.Contains(normalized, "other-secret") {
		t.Fatalf("unsafe Twitch cookie normalization: %v", err)
	}
	for _, tc := range []struct {
		name, raw, code string
	}{
		{"empty", "", apierr.CodeInvalidSourceArg},
		{"foreign", strings.ReplaceAll(valid, "twitch.tv", "twitch.tv.evil.test"), apierr.CodeInvalidSourceArg},
		{"missing", strings.ReplaceAll(valid, "auth-token", "preference"), apierr.CodeInvalidSourceArg},
		{"no header", strings.TrimPrefix(valid, "# Netscape HTTP Cookie File\n"), apierr.CodeInvalidSourceArg},
		{"expired", cookiesFor("expired", time.Now().Add(-time.Hour).Unix()), apierr.CodeSessionInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := connector.NormalizeCookies(tc.raw)
			assertCode(t, err, tc.code)
		})
	}
}

func TestValidationHomeFeedAndSearch(t *testing.T) {
	t.Parallel()
	var expectedFirst atomic.Int32
	expectedFirst.Store(5)
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/validate":
			if r.Header.Get("Authorization") != "OAuth private-twitch-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"client_id":"web-client","user_id":"user-1"}`)
		case "/helix/streams/followed":
			if r.Header.Get("Authorization") != "Bearer private-twitch-token" || r.Header.Get("Client-Id") != "web-client" || r.URL.Query().Get("user_id") != "user-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.URL.Query().Get("first"); got != strconv.Itoa(int(expectedFirst.Load())) {
				t.Errorf("feed page size=%s", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"user_id":"owner-1","user_login":"first","user_name":"First","title":"First live","game_name":"Game","thumbnail_url":"https://static.example/{width}x{height}.jpg","started_at":"2026-11-06T12:30:00Z","viewer_count":42,"language":"fr","tags":["French"]},{"user_id":"owner-2","user_login":"second","user_name":"Second","title":"Second live"},{"user_id":"owner-3","user_login":"third","user_name":"Third","title":"Third live"},{"user_id":"owner-4","user_login":"fourth","user_name":"Fourth","title":"Fourth live"},{"user_id":"owner-5","user_login":"fifth","user_name":"Fifth","title":"Fifth live"}],"pagination":{}}`)
		case "/gql":
			_, _ = io.WriteString(w, `{"data":{"searchFor":{"channels":{"cursor":"","edges":[{"item":{"login":"twitch","displayName":"Twitch","profileImageURL":"https://static.example/avatar.jpg"}}]},"videos":{"cursor":"","edges":[]}}}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}, Options{})
	connector := p.Connector()
	cookies, err := connector.NormalizeCookies(cookiesFor("private-twitch-token", 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.Validate(context.Background(), cookies); err != nil {
		t.Fatal(err)
	}
	feed, err := connector.Feed(context.Background(), cookies, "home", 2, 2)
	if err != nil || feed.Page != 2 || !feed.HasMore || len(feed.Videos) != 2 {
		t.Fatalf("Twitch feed page: %+v %v", feed, err)
	}
	video := feed.Videos[0]
	if video.ID != "third" || video.Platform != ID || video.Source != ID || video.ChannelID != "third" || video.Published != "" {
		t.Fatalf("Twitch live mapping: %+v", video)
	}
	expectedFirst.Store(3)
	feed, err = connector.Feed(context.Background(), cookies, "home", 1, 2)
	if err != nil || len(feed.Videos) != 2 || feed.Videos[0].Published != "2026-11-06T12:30:00Z" || feed.Videos[0].Thumbnail != "https://static.example/1280x720.jpg" {
		t.Fatalf("Twitch live metadata: %+v %v", feed, err)
	}
	results, err := p.Search(context.Background(), platform.Session{UserID: "user", Cookies: cookies}, "twitch", 1)
	if err != nil || len(results.Results) != 1 {
		t.Fatalf("Twitch search: %+v %v", results, err)
	}
	if channel := results.Results[0].Channel; channel == nil || channel.ID != "twitch" || channel.Platform != ID || channel.Source != ID {
		t.Fatalf("Twitch channel result: %+v", results.Results[0])
	}
	_, err = connector.Feed(context.Background(), cookies, "history", 1, 30)
	assertCode(t, err, apierr.CodeInvalidSourceArg)
	_, err = connector.Feed(context.Background(), cookies, "home", 0, 30)
	assertCode(t, err, apierr.CodeInvalidSourceArg)
	assertCode(t, connector.MarkWatched(context.Background(), cookies, "123"), apierr.CodeInvalidSourceArg)
}

func TestRejectsInvalidSession(t *testing.T) {
	t.Parallel()
	for name, handler := range map[string]http.HandlerFunc{
		"refused":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"forbidden": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"no identity": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"client_id":"","user_id":""}`)
		},
		"no auth-token": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPlatform(t, Options{})
			cookies := "# Netscape HTTP Cookie File\n.example.net\tTRUE\t/\tTRUE\t0\tother\tvalue\n"
			if handler != nil {
				p = graphQLPlatform(t, handler, Options{})
				var err error
				if cookies, err = p.Connector().NormalizeCookies(cookiesFor("expired-token", 0)); err != nil {
					t.Fatal(err)
				}
			}
			assertCode(t, p.Connector().Validate(context.Background(), cookies), apierr.CodeSessionInvalid)
		})
	}
}

func TestIdentityFailuresAreGenericAndKeepTheirCause(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*Platform{
		"unreachable": deadPlatform(t),
		"server error": graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, Options{}),
		"not json": graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "nope") }, Options{}),
		"trailing data": graphQLPlatform(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"client_id":"a","user_id":"b"} {}`)
		}, Options{}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := p.Connector().Validate(context.Background(), cookiesFor("token", 0))
			assertCode(t, err, apierr.CodeRequestFailed)
			if failure := err.(*apierr.Error); failure.Cause == nil || strings.Contains(failure.Message, "127.0.0.1") {
				t.Fatalf("cause must be attached and kept out of the message: %+v", failure)
			}
		})
	}
}

func TestSearchOfAConnectedSessionWithoutCookiesIsNotConnected(t *testing.T) {
	t.Parallel()
	_, err := newPlatform(t, Options{}).Search(context.Background(), platform.Session{UserID: "user"}, "x", 1)
	assertCode(t, err, apierr.CodeNotConnected)
}
