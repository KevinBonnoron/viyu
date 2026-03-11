package sources

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

const (
	alphaID = "youtube"  // connectable, on by default
	betaID  = "twitch"   // public only, on by default
	gammaID = "peertube" // connectable, opt-in
)

// fakePlatform is the part of platform.Platform the service uses; calling any other method panics.
type fakePlatform struct {
	platform.Platform
	id        string
	enabled   bool
	connector platform.Connector
}

var fakeVideoID = regexp.MustCompile(`^vid-[0-9]+$`)

func (p *fakePlatform) ID() string                    { return p.id }
func (p *fakePlatform) DefaultEnabled() bool          { return p.enabled }
func (p *fakePlatform) Connector() platform.Connector { return p.connector }
func (p *fakePlatform) ValidVideoID(id string) bool   { return fakeVideoID.MatchString(id) }
func (p *fakePlatform) Descriptor() platform.Descriptor {
	methods := []string{}
	if p.connector != nil {
		methods = append(methods, "cookies")
	}
	return platform.Descriptor{Platform: p.id, Name: strings.ToUpper(p.id), Description: "The " + p.id + " fixture.", Capabilities: []string{"search"}, ConnectionMethods: methods}
}

type feedCall struct {
	Cookies, Kind string
	Page, Limit   int
}

// fakeConnector keeps the rows of its own domain and wants a SESSION cookie among them.
type fakeConnector struct {
	mu          sync.Mutex
	validateErr error
	feedErr     error
	watchErr    error
	validated   []string
	feeds       []feedCall
	watched     []string
}

func (c *fakeConnector) NormalizeCookies(raw string) (string, error) {
	if raw == "" || len(raw) > platform.CookieLimit {
		return "", apierr.InvalidInput("Import a cookie file.")
	}
	lines := strings.Split(raw, "\n")
	if lines[0] != "# Netscape HTTP Cookie File" {
		return "", apierr.InvalidInput("The cookie file must use Netscape cookies.txt format.")
	}
	var kept strings.Builder
	kept.WriteString(lines[0] + "\n")
	session := false
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 7 || strings.TrimPrefix(fields[0], ".") != "fake.test" {
			continue
		}
		session = session || (fields[5] == "SESSION" && fields[6] != "")
		kept.WriteString(line + "\n")
	}
	if !session {
		return "", apierr.InvalidInput("The cookie file does not hold a session.")
	}
	return kept.String(), nil
}

func (c *fakeConnector) Validate(_ context.Context, cookies string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.validated = append(c.validated, cookies)
	return c.validateErr
}

func (c *fakeConnector) Feed(_ context.Context, cookies, kind string, page, limit int) (platform.Feed, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.feeds = append(c.feeds, feedCall{cookies, kind, page, limit})
	if c.feedErr != nil {
		return platform.Feed{}, c.feedErr
	}
	return platform.Feed{Videos: []platform.Video{}, Page: page, HasMore: true}, nil
}

func (c *fakeConnector) MarkWatched(_ context.Context, _, videoID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.watched = append(c.watched, videoID)
	return c.watchErr
}

func (c *fakeConnector) fail(validate, feed, watch error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.validateErr, c.feedErr, c.watchErr = validate, feed, watch
}

func (c *fakeConnector) calls() (validated, feeds, watched int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.validated), len(c.feeds), len(c.watched)
}

// fakeCookies is a cookie jar of the fake domain holding secret, plus a row of another domain the
// connector must drop.
func fakeCookies(secret string) string {
	return "# Netscape HTTP Cookie File\n" +
		".fake.test\tTRUE\t/\tTRUE\t0\tSESSION\t" + secret + "\n" +
		".fake.test\tTRUE\t/\tTRUE\t0\tPREF\tpref-" + secret + "\n" +
		".other.test\tTRUE\t/\tTRUE\t0\tunrelated\tother-secret\n"
}

type fixture struct {
	app       core.App
	svc       *Service
	runner    *ytdlp.Runner
	registry  *platform.Registry
	cookieDir string
	alpha     *fakeConnector
	gamma     *fakeConnector
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	app := testutil.NewApp(t)
	f := &fixture{app: app, alpha: &fakeConnector{}, gamma: &fakeConnector{}, cookieDir: filepath.Join(app.DataDir(), "source-cookies")}
	f.registry = platform.NewRegistry(
		&fakePlatform{id: alphaID, enabled: true, connector: f.alpha},
		&fakePlatform{id: betaID, enabled: true},
		&fakePlatform{id: gammaID, connector: f.gamma},
	)
	f.runner = ytdlp.New(ytdlp.Options{CookieDir: f.cookieDir})
	svc, err := New(app, f.registry, f.runner, Options{})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func newUser(t *testing.T, app core.App, email string) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("source-fixture-password-123")
	record.Set("role", "user")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatalf("want error %s, got %v", code, err)
	}
	for _, secret := range []string{"private-cookie-value", "viyu-source-secret", "cipher"} {
		if strings.Contains(safe.Message, secret) {
			t.Fatalf("the message of an error reaching clients leaks %q: %q", secret, safe.Message)
		}
	}
}

func (f *fixture) connect(t *testing.T, userID, platformID, secret string) Source {
	t.Helper()
	source, err := f.svc.Connect(context.Background(), userID, platformID, fakeCookies(secret), "")
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestWatchInputIsValidatedBeforeAnythingReachesThePlatform(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "watch@example.test")
	f.connect(t, user.Id, alphaID, "private-cookie-value")
	nan, inf := math.NaN(), math.Inf(1)
	for _, tc := range []struct {
		name     string
		video    string
		played   float64
		duration float64
		valid    bool
	}{
		{"thirty-seconds", "vid-1", 30, 120, true},
		{"short-video-played-fully", "vid-1", 10, 10, true},
		{"too-short-a-play", "vid-1", 29.9, 120, false},
		{"nothing-played", "vid-1", 0, 120, false},
		{"short-video-not-finished", "vid-1", 5, 10, false},
		{"negative-play", "vid-1", -1, 120, false},
		{"zero-duration", "vid-1", 30, 0, false},
		{"negative-duration", "vid-1", 30, -5, false},
		{"nan-play", "vid-1", nan, 120, false},
		{"infinite-play", "vid-1", inf, 120, false},
		{"nan-duration", "vid-1", 30, nan, false},
		{"infinite-duration", "vid-1", 30, inf, false},
		{"invalid-video", "video&injected=1", 30, 120, false},
		{"no-video", "", 30, 120, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, before := f.alpha.calls()
			err := f.svc.Watch(context.Background(), user.Id, alphaID, tc.video, tc.played, tc.duration)
			_, _, after := f.alpha.calls()
			if tc.valid {
				if err != nil || after != before+1 {
					t.Fatalf("a genuine playback was not recorded: %v (%d -> %d)", err, before, after)
				}
				return
			}
			assertCode(t, err, apierr.CodeInvalidSourceArg)
			if after != before {
				t.Fatal("an invalid watch reached the platform")
			}
		})
	}
}

func TestWatchRecordsTheNativeVideoAndClassifiesPlatformFailures(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "watch-failures@example.test")
	ctx := context.Background()

	assertCode(t, f.svc.Watch(ctx, user.Id, alphaID, "vid-7", 30, 120), apierr.CodeNotConnected)
	f.connect(t, user.Id, alphaID, "private-cookie-value")
	if err := f.svc.Watch(ctx, user.Id, alphaID, "vid-7", 30, 120); err != nil {
		t.Fatal(err)
	}
	if _, _, watched := f.alpha.calls(); watched != 1 || f.alpha.watched[0] != "vid-7" {
		t.Fatalf("watched: %v", f.alpha.watched)
	}

	f.alpha.fail(nil, nil, apierr.SessionInvalid())
	assertCode(t, f.svc.Watch(ctx, user.Id, alphaID, "vid-7", 30, 120), apierr.CodeSessionInvalid)
	// An error the platform did not classify is an opaque upstream failure, its cause kept for the log.
	f.alpha.fail(nil, nil, errors.New("ERROR: remote failure private-cookie-value /tmp/viyu-source-secret"))
	err := f.svc.Watch(ctx, user.Id, alphaID, "vid-7", 30, 120)
	assertCode(t, err, apierr.CodeRequestFailed)
	if safe := apierr.From(err); safe.Cause == nil {
		t.Fatal("the cause of the failure was dropped")
	}

	assertCode(t, f.svc.Watch(ctx, user.Id, betaID, "vid-7", 30, 120), apierr.CodeNotConnectable)
	assertCode(t, f.svc.Watch(ctx, user.Id, "unknown", "vid-7", 30, 120), apierr.CodeInvalidSourceArg)
}

func TestFeedValidatesThePageAndDeliversTheStoredSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "feed@example.test")
	ctx := context.Background()

	for _, page := range [][2]int{{0, 10}, {-1, 10}, {1, 0}, {1, 51}, {51, 10}} {
		_, err := f.svc.Feed(ctx, user.Id, alphaID, "home", page[0], page[1])
		assertCode(t, err, apierr.CodeInvalidSourceArg)
	}
	if _, feeds, _ := f.alpha.calls(); feeds != 0 {
		t.Fatal("an invalid page reached the platform")
	}

	_, err := f.svc.Feed(ctx, user.Id, alphaID, "home", 1, 10)
	assertCode(t, err, apierr.CodeNotConnected)
	_, err = f.svc.Feed(ctx, user.Id, betaID, "home", 1, 10)
	assertCode(t, err, apierr.CodeNotConnectable)
	_, err = f.svc.Feed(ctx, user.Id, "unknown", "home", 1, 10)
	assertCode(t, err, apierr.CodeInvalidSourceArg)

	f.connect(t, user.Id, alphaID, "private-cookie-value")
	feed, err := f.svc.Feed(ctx, user.Id, alphaID, "history", 2, 5)
	if err != nil || feed.Page != 2 || !feed.HasMore {
		t.Fatalf("feed: %+v %v", feed, err)
	}
	call := f.alpha.feeds[0]
	if call.Kind != "history" || call.Page != 2 || call.Limit != 5 || !strings.Contains(call.Cookies, "private-cookie-value") || strings.Contains(call.Cookies, "other-secret") {
		t.Fatalf("the platform was not given the normalized stored session: %+v", call)
	}

	f.alpha.fail(nil, apierr.SessionInvalid(), nil)
	_, err = f.svc.Feed(ctx, user.Id, alphaID, "home", 1, 10)
	assertCode(t, err, apierr.CodeSessionInvalid)
}

func TestConnectValidatesLabelAndCookiesAndNeverStoresACancelledRequest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "connect-input@example.test")
	ctx := context.Background()

	for _, label := range []string{strings.Repeat("é", 121), "two\nlines", "nul\x00", "bad\xff"} {
		_, err := f.svc.Connect(ctx, user.Id, alphaID, fakeCookies("private-cookie-value"), label)
		assertCode(t, err, apierr.CodeInvalidSourceArg)
	}
	for _, cookies := range []string{"", "not a cookie file", strings.Repeat("x", 400<<10), "# Netscape HTTP Cookie File\n.fake.test\tTRUE\t/\tTRUE\t0\tPREF\tx\n"} {
		_, err := f.svc.Connect(ctx, user.Id, alphaID, cookies, "")
		assertCode(t, err, apierr.CodeInvalidSourceArg)
	}
	assertCode(t, errOf(f.svc.Connect(ctx, user.Id, betaID, fakeCookies("x"), "")), apierr.CodeNotConnectable)
	assertCode(t, errOf(f.svc.Connect(ctx, user.Id, "unspecified", fakeCookies("x"), "")), apierr.CodeInvalidSourceArg)
	if validated, _, _ := f.alpha.calls(); validated != 0 {
		t.Fatal("an invalid import reached the platform")
	}

	label := strings.Repeat("é", 120)
	source, err := f.svc.Connect(ctx, user.Id, alphaID, fakeCookies("private-cookie-value"), "  "+label+"  ")
	if err != nil || source.Label != label {
		t.Fatalf("a label of the maximal length is kept, trimmed: %+v %v", source, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	other := newUser(t, f.app, "connect-cancel@example.test")
	assertCode(t, errOf(f.svc.Connect(cancelled, other.Id, alphaID, fakeCookies("x"), "")), apierr.CodeRequestFailed)
	assertCode(t, errOf(f.svc.SessionVersion(other.Id, alphaID)), apierr.CodeNotConnected)
}

func errOf[T any](_ T, err error) error { return err }

func TestConnectionsAreEncryptedPrivateAndIsolated(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := newUser(t, f.app, "source-owner@example.test")
	other := newUser(t, f.app, "source-other@example.test")
	cookies := fakeCookies("private-cookie-value")

	connected, err := f.svc.Connect(context.Background(), owner.Id, alphaID, cookies, "My session")
	if err != nil {
		t.Fatal(err)
	}
	if !connected.Connected || connected.Label != "My session" || connected.ConnectionID == "" || connected.UpdatedAt == "" || connected.Platform != alphaID {
		t.Fatalf("incomplete connection descriptor: %+v", connected)
	}
	record, err := f.svc.connection(owner.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	secret := record.GetString("encrypted_cookies")
	if !strings.HasPrefix(secret, "v1.") || strings.Contains(secret, "private-cookie-value") || secret == cookies {
		t.Fatal("cookies were not encrypted")
	}
	encoded, err := record.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "encrypted_cookies") || strings.Contains(string(encoded), "private-cookie-value") {
		t.Fatalf("record serialization exposed a secret: %s", encoded)
	}

	catalog, err := f.svc.List(owner.Id)
	if err != nil || len(catalog) != 3 || catalog[0].Platform != alphaID || !catalog[0].Connected || catalog[1].Platform != betaID || catalog[1].Connected || catalog[2].Platform != gammaID || catalog[2].Connected {
		t.Fatalf("catalog: %+v %v", catalog, err)
	}

	collection, err := f.app.FindCollectionByNameOrId(ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if collection.ListRule != nil || collection.ViewRule != nil || collection.CreateRule != nil || collection.UpdateRule != nil || collection.DeleteRule != nil {
		t.Fatal("source collection must be locked to backend access")
	}

	// Two jars of the same connection are distinct, private and carry only the platform's own rows.
	path1, cleanup1, err := f.svc.CookieFile(owner.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup1()
	path2, cleanup2, err := f.svc.CookieFile(owner.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if path1 == path2 || strings.Contains(readFile(t, path1), "other-secret") || !strings.Contains(readFile(t, path1), "private-cookie-value") {
		t.Fatal("temporary cookies were shared or not domain-filtered")
	}
	assertPrivate(t, path1, 0o600)

	// A record only decrypts for the account and platform it was sealed for.
	_, err = f.svc.StoredCookies(other.Id, alphaID)
	assertCode(t, err, apierr.CodeNotConnected)
	_, err = f.svc.decrypt(secret, other.Id, alphaID)
	assertCode(t, err, apierr.CodeRequestFailed)
	_, err = f.svc.decrypt(secret, owner.Id, gammaID)
	assertCode(t, err, apierr.CodeRequestFailed)
	for _, damaged := range []string{"", "v2." + secret[3:], "v1.not base64!", "v1.AAAA", secret[:len(secret)-4] + "AAAA"} {
		_, err = f.svc.decrypt(damaged, owner.Id, alphaID)
		assertCode(t, err, apierr.CodeRequestFailed)
	}

	if _, err := f.svc.Connect(context.Background(), other.Id, alphaID, fakeCookies("other-account"), "Other"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Disconnect(owner.Id, alphaID); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.StoredCookies(owner.Id, alphaID)
	assertCode(t, err, apierr.CodeNotConnected)
	if cookies, err := f.svc.StoredCookies(other.Id, alphaID); err != nil || !strings.Contains(cookies, "other-account") {
		t.Fatalf("disconnect affected another user: %v", err)
	}
	// Disconnecting twice is not an error; an unknown source is.
	if err := f.svc.Disconnect(owner.Id, alphaID); err != nil {
		t.Fatal(err)
	}
	assertCode(t, f.svc.Disconnect(owner.Id, "unknown"), apierr.CodeInvalidSourceArg)
}

func TestSourceJSONKeepsTheShapeOfTheCatalog(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "json@example.test")
	f.connect(t, user.Id, alphaID, "private-cookie-value")
	catalog, err := f.svc.List(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	encoded := marshal(t, catalog)
	for _, field := range []string{`"platform":"youtube"`, `"name":"YOUTUBE"`, `"description":`, `"capabilities":["search"]`, `"connectionMethods":["cookies"]`, `"connected":true`, `"enabled":true`, `"connectionId":"`, `"updatedAt":"`} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("catalog lacks %s: %s", field, encoded)
		}
	}
	if strings.Contains(encoded, "Descriptor") || strings.Contains(encoded, "encrypted_cookies") || strings.Contains(encoded, "private-cookie-value") {
		t.Fatalf("catalog shape: %s", encoded)
	}
	// An unconnected source has none of the connection fields.
	if bare := marshal(t, catalog[1]); strings.Contains(bare, "connectionId") || strings.Contains(bare, "label") || strings.Contains(bare, "updatedAt") || !strings.Contains(bare, `"connectionMethods":[]`) || !strings.Contains(bare, `"connected":false`) {
		t.Fatalf("unconnected source: %s", bare)
	}
}

func TestFailedReplacementPreservesSessionAndReconnectCreatesANewRecord(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "source-replace@example.test")
	ctx := context.Background()
	cookies := fakeCookies("private-cookie-value")
	first, err := f.svc.Connect(ctx, user.Id, alphaID, cookies, "Original")
	if err != nil {
		t.Fatal(err)
	}
	version, err := f.svc.SessionVersion(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}

	for name, failure := range map[string]error{
		"expired":  apierr.SessionInvalid(),
		"rejected": apierr.RequestFailed(),
		"opaque":   errors.New("ERROR: remote failure private-cookie-value /tmp/viyu-source-secret"),
	} {
		f.alpha.fail(failure, nil, nil)
		_, err := f.svc.Connect(ctx, user.Id, alphaID, fakeCookies("replacement"), "Replacement")
		code := apierr.CodeRequestFailed
		if name == "expired" {
			code = apierr.CodeSessionInvalid
		}
		assertCode(t, err, code)
		if next, err := f.svc.SessionVersion(user.Id, alphaID); err != nil || next != version {
			t.Fatalf("%s: failed replacement changed the session: %v", name, err)
		}
	}
	f.alpha.fail(nil, nil, nil)

	// A restart (a new service on the same data directory and key) still decrypts the original session.
	reopened, err := New(f.app, f.registry, f.runner, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if original, err := reopened.StoredCookies(user.Id, alphaID); err != nil || !strings.Contains(original, "private-cookie-value") {
		t.Fatalf("restarting could not decrypt original credentials: %v", err)
	}

	// Importing the very same cookies again is still a new connection: playback sessions end with the old one.
	second, err := f.svc.Connect(ctx, user.Id, alphaID, cookies, "Reconnected")
	if err != nil {
		t.Fatal(err)
	}
	if next, err := f.svc.SessionVersion(user.Id, alphaID); err != nil || next == version || second.ConnectionID == first.ConnectionID {
		t.Fatalf("reconnect did not create a new connection: %v", err)
	}
	if _, err := f.app.FindRecordById(ConnectionsCollection, first.ConnectionID); err == nil {
		t.Fatal("the replaced connection record survived")
	}
	if rows, err := f.app.FindRecordsByFilter(ConnectionsCollection, "user = {:user}", "", 0, 0, map[string]any{"user": user.Id}); err != nil || len(rows) != 1 {
		t.Fatalf("one connection per account and platform: %d %v", len(rows), err)
	}
}

func TestCheckValidatesTheStoredSessionWithThePlatform(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "check@example.test")
	ctx := context.Background()
	assertCode(t, errOf(f.svc.Check(ctx, user.Id, alphaID)), apierr.CodeNotConnected)
	assertCode(t, errOf(f.svc.Check(ctx, user.Id, betaID)), apierr.CodeNotConnectable)
	assertCode(t, errOf(f.svc.Check(ctx, user.Id, "unknown")), apierr.CodeInvalidSourceArg)

	f.connect(t, user.Id, alphaID, "private-cookie-value")
	before, _, _ := f.alpha.calls()
	source, err := f.svc.Check(ctx, user.Id, alphaID)
	if err != nil || !source.Connected {
		t.Fatalf("check: %+v %v", source, err)
	}
	if after, _, _ := f.alpha.calls(); after != before+1 || !strings.Contains(f.alpha.validated[after-1], "private-cookie-value") {
		t.Fatal("the stored session was not validated with the platform")
	}
	f.alpha.fail(apierr.SessionInvalid(), nil, nil)
	assertCode(t, errOf(f.svc.Check(ctx, user.Id, alphaID)), apierr.CodeSessionInvalid)
}

func TestOpenBuildsTheSessionOfAConnectedRequest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "open@example.test")
	_, cleanup, err := f.svc.Open(user.Id, alphaID)
	if cleanup != nil {
		t.Fatal("a failed open returned a cleanup")
	}
	assertCode(t, err, apierr.CodeNotConnected)
	_, _, err = f.svc.Open(user.Id, betaID)
	assertCode(t, err, apierr.CodeNotConnectable)

	f.connect(t, user.Id, alphaID, "private-cookie-value")
	session, cleanup, err := f.svc.Open(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Connected() || session.UserID != user.Id || !strings.Contains(session.Cookies, "private-cookie-value") || strings.Contains(session.Cookies, "other-secret") {
		t.Fatalf("session: %+v", session)
	}
	if filepath.Dir(session.CookieFile) != f.cookieDir || readFile(t, session.CookieFile) != session.Cookies {
		t.Fatalf("the jar of the session is not a copy of its cookies in the runner's directory: %s", session.CookieFile)
	}
	assertPrivate(t, session.CookieFile, 0o600)
	cleanup()
	assertGone(t, session.CookieFile)
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertPrivate(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != perm {
		t.Fatalf("%s must be %o: %v %v", path, perm, info, err)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should be gone: %v", path, err)
	}
}
