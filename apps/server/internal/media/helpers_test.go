package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	defaultID = "alpha"
	otherID   = "beta"
)

// fakePlatform implements the part of platform.Platform the proxy uses; the embedded nil interface makes any
// other call fail loudly.
type fakePlatform struct {
	platform.Platform
	id       string
	trusted  func(host string) bool
	manifest func(ctx context.Context, id, cookies string) (platform.Manifest, error)
	calls    atomic.Int32
}

func (f *fakePlatform) ID() string { return f.id }

func (f *fakePlatform) TrustedMediaHost(host string) bool { return f.trusted != nil && f.trusted(host) }

func (f *fakePlatform) PlaybackManifest(ctx context.Context, id, cookies string) (platform.Manifest, error) {
	f.calls.Add(1)
	return f.manifest(ctx, id, cookies)
}

// fixedManifest answers every lookup with the same manifest.
func fixedManifest(m platform.Manifest) func(context.Context, string, string) (platform.Manifest, error) {
	return func(context.Context, string, string) (platform.Manifest, error) { return m, nil }
}

type cookieFunc func(userID, platformID string) (string, func(), error)

func (f cookieFunc) CookieFile(userID, platformID string) (string, func(), error) {
	return f(userID, platformID)
}

// testSpawn runs lookups on goroutines the test waits for.
func testSpawn(t *testing.T) func(work func(ctx context.Context)) bool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var group sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		group.Wait()
	})
	return func(work func(ctx context.Context)) bool {
		group.Add(1)
		go func() {
			defer group.Done()
			work(ctx)
		}()
		return true
	}
}

// newProxy builds a Proxy over the given platforms (the first is the default) fetching with the plain
// client, which may reach the loopback servers of the tests.
func newProxy(t *testing.T, platforms []*fakePlatform, configure func(*Options)) *Proxy {
	t.Helper()
	list := make([]platform.Platform, len(platforms))
	for i, p := range platforms {
		list[i] = p
	}
	opts := Options{Platforms: platform.NewRegistry(list...), HTTP: &http.Client{}, Spawn: testSpawn(t)}
	if configure != nil {
		configure(&opts)
	}
	return New(opts)
}

// api is the HTTP layer of the tests: it authorizes nothing, builds the Key and serializes errors the way the
// real layer does.
func api(p *Proxy) http.Handler {
	key := func(r *http.Request) Key {
		id := defaultID
		if named := r.URL.Query().Get("platform"); named != "" {
			id = named
		}
		return Key{VideoID: "native-" + r.PathValue("id"), RecordID: r.PathValue("id"), Platform: id, UserID: "user", Revision: "1"}
	}
	respond := func(w http.ResponseWriter, err error) {
		var failure *apierr.Error
		if err == nil {
			return
		}
		if !errors.As(err, &failure) {
			failure = apierr.Internal("unclassified", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(failure.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": failure.Code, "message": failure.Message})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, p.ServeManifest(w, r, key(r), r.URL.Query().Get("source")))
	})
	mux.HandleFunc("GET /api/stream/{id}/resource/{resource}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, p.ServeResource(w, r, key(r), r.PathValue("resource"), r.URL.Query().Get("source")))
	})
	return mux
}

func get(t *testing.T, handler http.Handler, path string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// resources lists the same-origin addresses of a rewritten playlist.
func resources(body string) []string {
	var found []string
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "/api/stream/") {
			found = append(found, line)
		}
	}
	return found
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an error envelope: %d %q", response.Code, response.Body)
	}
	return body.Code
}

// within waits for a condition signalled on done; tests never poll.
func within(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}

// clock is a controllable time source.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Now().UTC().Truncate(time.Second)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
