package twitch

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// newPlatform builds a platform whose yt-dlp is a script that fails unless the test brings its own runner.
func newPlatform(t *testing.T, opts Options) *Platform {
	t.Helper()
	if opts.Runner == nil {
		opts.Runner = ytdlp.New(ytdlp.Options{Binary: testutil.Script(t, "yt-dlp", "echo 'unexpected yt-dlp call' >&2\nexit 9")})
	}
	return New(opts)
}

// fakeYTDLP is a runner over a /bin/sh script standing in for yt-dlp.
func fakeYTDLP(t *testing.T, body string) *ytdlp.Runner {
	t.Helper()
	return ytdlp.New(ytdlp.Options{Binary: testutil.Script(t, "yt-dlp", body)})
}

// graphQLPlatform serves every endpoint of the platform from one httptest server running handler.
func graphQLPlatform(t *testing.T, handler http.HandlerFunc, opts Options) *Platform {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts.HTTP = server.Client()
	opts.GraphQLURL = server.URL + "/gql"
	opts.ValidateURL = server.URL + "/validate"
	opts.APIURL = server.URL + "/helix"
	return newPlatform(t, opts)
}

// deadPlatform has endpoints nobody listens on.
func deadPlatform(t *testing.T) *Platform {
	t.Helper()
	return newPlatform(t, Options{GraphQLURL: "http://127.0.0.1:1/graphql", ValidateURL: "http://127.0.0.1:1/validate", APIURL: "http://127.0.0.1:1/helix"})
}

func cookiesFor(token string, expires int64) string {
	return fmt.Sprintf("# Netscape HTTP Cookie File\n#HttpOnly_.twitch.tv\tTRUE\t/\tTRUE\t%d\tauth-token\t%s\n.example.net\tTRUE\t/\tTRUE\t%d\tunrelated\tother-secret\n", expires, token, expires)
}

func readBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("reading request body: %v", err)
	}
	return body
}

// assertCode fails unless err is an *apierr.Error with the code.
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *apierr.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}
