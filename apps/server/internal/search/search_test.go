package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

// fakePlatform answers searches from a function; every other method is unused here, except the two a store
// asks the registry (or a lookup by channel) for.
type fakePlatform struct {
	platform.Platform
	id     string
	search func(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error)
}

func (p fakePlatform) ID() string                          { return p.id }
func (p fakePlatform) ChannelIDAliases(id string) []string { return []string{id} }
func (p fakePlatform) SingleTitle() bool                   { return false }
func (p fakePlatform) Search(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
	return p.search(ctx, session, query, page)
}

// videos answers n videos (ids prefix-0…) of one channel each.
func videos(prefix string, n int, hasMore bool) func(context.Context, platform.Session, string, int) (platform.SearchPage, error) {
	return func(_ context.Context, _ platform.Session, _ string, page int) (platform.SearchPage, error) {
		out := platform.SearchPage{Results: []platform.Result{}, Page: page, HasMore: hasMore}
		for i := range n {
			id := prefix + string(rune('0'+i))
			out.Results = append(out.Results, platform.Result{Video: &platform.Video{ID: id, Platform: prefix, Title: id, ChannelID: "UC" + id, ChannelName: id}})
		}
		return out, nil
	}
}

func failing(err error) func(context.Context, platform.Session, string, int) (platform.SearchPage, error) {
	return func(context.Context, platform.Session, string, int) (platform.SearchPage, error) {
		return platform.SearchPage{}, err
	}
}

func fixture(t *testing.T, platforms ...fakePlatform) (*Service, core.App, *catalog.Store) {
	t.Helper()
	app := testutil.NewApp(t)
	catalog.RegisterHooks(app)
	registered := make([]platform.Platform, len(platforms))
	for i, p := range platforms {
		registered[i] = p
	}
	registry := platform.NewRegistry(registered...)
	store := catalog.New(app, registry, catalog.Options{})
	return New(registry, store, nil), app, store
}

func platformIDs(results []platform.Result) []string {
	var ids []string
	for _, r := range results {
		ids = append(ids, r.Video.Platform)
	}
	return ids
}

func TestAllInterleavesEveryPlatformAndStoresTheResults(t *testing.T) {
	t.Parallel()
	alpha := fakePlatform{id: "youtube", search: videos("youtube", 1, false)}
	beta := fakePlatform{id: "twitch", search: videos("twitch", 3, true)}
	s, _, store := fixture(t, alpha, beta)

	page, err := s.All(context.Background(), []platform.Platform{alpha, beta}, "fixture", 2)
	if err != nil {
		t.Fatal(err)
	}
	// One result of the first platform, three of the second: alternation puts each platform's best result
	// first instead of letting one platform bury the other.
	if got := platformIDs(page.Results); len(got) != 4 || got[0] != "youtube" || got[1] != "twitch" || got[2] != "twitch" || got[3] != "twitch" {
		t.Fatalf("results are not interleaved: %v", got)
	}
	if page.Page != 2 || !page.HasMore {
		t.Fatalf("page = %d, hasMore = %v", page.Page, page.HasMore)
	}
	record, err := store.FindVideo("youtube", "youtube0")
	if err != nil || page.Results[0].Video.RecordID != record.Id {
		t.Fatalf("the first result was not stored under its record: %v %+v", err, page.Results[0].Video)
	}
	if owners := record.GetStringSlice("owners"); len(owners) != 0 {
		t.Fatalf("a combined search is public: owners %v", owners)
	}
	if _, err := json.Marshal(page); err != nil {
		t.Fatalf("a returned page must serialize: %v", err)
	}
}

func TestAllSearchesWithoutCredentials(t *testing.T) {
	t.Parallel()
	var connected atomic.Bool
	probe := fakePlatform{id: "probe", search: func(_ context.Context, session platform.Session, _ string, page int) (platform.SearchPage, error) {
		if session.Connected() || session.CookieFile != "" || session.Cookies != "" {
			connected.Store(true)
		}
		return platform.SearchPage{Results: []platform.Result{}, Page: page}, nil
	}}
	s, _, _ := fixture(t, probe)
	if _, err := s.All(context.Background(), []platform.Platform{probe}, "q", 1); err != nil {
		t.Fatal(err)
	}
	if connected.Load() {
		t.Fatal("a combined search must be public")
	}
}

func TestAllSurvivesOnePlatformFailingButNotEveryOne(t *testing.T) {
	t.Parallel()
	good := fakePlatform{id: "youtube", search: videos("youtube", 1, false)}
	bad := fakePlatform{id: "bad", search: failing(errors.New("boom"))}
	s, _, _ := fixture(t, good, bad)

	page, err := s.All(context.Background(), []platform.Platform{good, bad}, "q", 1)
	if err != nil || len(page.Results) != 1 || page.Results[0].Video.Platform != "youtube" {
		t.Fatalf("a failing platform hid the other: %v %+v", err, page)
	}

	_, err = s.All(context.Background(), []platform.Platform{bad, bad}, "q", 1)
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Status != http.StatusBadGateway || safe.Cause == nil {
		t.Fatalf("every platform failing must be the generic upstream failure with its cause: %v", err)
	}

	typed := fakePlatform{id: "typed", search: failing(apierr.SessionInvalid())}
	if _, err = s.All(context.Background(), []platform.Platform{typed}, "q", 1); !errors.As(err, &safe) || safe.Code != apierr.CodeSessionInvalid {
		t.Fatalf("a classified failure must keep its code: %v", err)
	}
}

func TestAllOfNothingIsAnEmptyPage(t *testing.T) {
	t.Parallel()
	s, _, _ := fixture(t, fakePlatform{id: "alpha"})
	page, err := s.All(context.Background(), nil, "q", 3)
	if err != nil || page.Results == nil || len(page.Results) != 0 || page.Page != 3 || page.HasMore {
		t.Fatalf("%v %+v", err, page)
	}
	if data, _ := json.Marshal(page); string(data) != `{"results":[],"page":3,"hasMore":false}` {
		t.Fatalf("an empty page must answer an empty list: %s", data)
	}
}

func TestOneOfOnePlatformIsUnchangedByTheOthers(t *testing.T) {
	t.Parallel()
	alpha := fakePlatform{id: "youtube", search: videos("youtube", 2, false)}
	beta := fakePlatform{id: "twitch", search: videos("twitch", 2, false)}
	s, _, _ := fixture(t, alpha, beta)
	for _, p := range []fakePlatform{alpha, beta} {
		page, err := s.One(context.Background(), p, platform.Session{}, "q", 1)
		if err != nil || len(page.Results) != 2 {
			t.Fatalf("%s: %v %+v", p.id, err, page)
		}
		for _, id := range platformIDs(page.Results) {
			if id != p.id {
				t.Fatalf("%s search returned a %s result", p.id, id)
			}
		}
	}
}

func TestOneFailureIsAnAPIError(t *testing.T) {
	t.Parallel()
	bad := fakePlatform{id: "bad", search: failing(errors.New("yt-dlp said secret things"))}
	s, _, _ := fixture(t, bad)
	_, err := s.One(context.Background(), bad, platform.Session{}, "q", 1)
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Code != apierr.CodeRequestFailed || safe.Cause == nil {
		t.Fatalf("%v", err)
	}
}

func TestConnectedSearchResultsBelongToTheAccount(t *testing.T) {
	t.Parallel()
	var seen atomic.Value
	connected := fakePlatform{id: "youtube", search: func(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
		seen.Store(session)
		return videos("youtube", 1, false)(ctx, session, query, page)
	}}
	s, app, store := fixture(t, connected)
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	user := core.NewRecord(users)
	user.SetEmail("searcher@example.test")
	user.SetPassword("search-fixture-password-123")
	user.Set("role", "user")
	if err := app.Save(user); err != nil {
		t.Fatal(err)
	}

	session := platform.Session{UserID: user.Id, CookieFile: "/private/jar.txt"}
	page, err := s.One(context.Background(), connected, session, "q", 1)
	if err != nil || len(page.Results) != 1 {
		t.Fatalf("%v %+v", err, page)
	}
	if got, _ := seen.Load().(platform.Session); got != session {
		t.Fatalf("the platform did not receive the session: %+v", got)
	}
	for _, find := range []func() (*core.Record, error){
		func() (*core.Record, error) { return store.FindVideo("youtube", "youtube0") },
		func() (*core.Record, error) { return store.FindChannel("youtube", "UCyoutube0") },
	} {
		record, err := find()
		if err != nil {
			t.Fatal(err)
		}
		if owners := record.GetStringSlice("owners"); len(owners) != 1 || owners[0] != user.Id {
			t.Fatalf("a connected search is private to the account: owners %v", owners)
		}
	}
}

// A result that cannot be stored is dropped, never returned with its native id.
func TestResultsThatCannotBeStoredAreDropped(t *testing.T) {
	t.Parallel()
	alpha := fakePlatform{id: "youtube", search: videos("youtube", 3, false)}
	s, app, _ := fixture(t, alpha)
	app.OnRecordCreate("videos").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("platform_id") == "youtube1" {
			return errors.New("the database refused")
		}
		return e.Next()
	})
	for name, search := range map[string]func() (platform.SearchPage, error){
		"one": func() (platform.SearchPage, error) {
			return s.One(context.Background(), alpha, platform.Session{}, "q", 1)
		},
		"all": func() (platform.SearchPage, error) {
			return s.All(context.Background(), []platform.Platform{alpha}, "q", 1)
		},
	} {
		page, err := search()
		if err != nil || len(page.Results) != 2 {
			t.Fatalf("%s: %v %+v", name, err, page)
		}
		for _, result := range page.Results {
			if result.Video.ID == "youtube1" {
				t.Fatalf("%s returned a result that was not stored", name)
			}
		}
		if _, err := json.Marshal(page); err != nil {
			t.Fatalf("%s: the survivors must serialize: %v", name, err)
		}
	}
}

func TestEnabledKeepsTheActivePlatformsInRegistryOrder(t *testing.T) {
	t.Parallel()
	a, b, c := fakePlatform{id: "a"}, fakePlatform{id: "b"}, fakePlatform{id: "c"}
	s, _, _ := fixture(t, a, b, c)
	var ids []string
	for _, p := range s.Enabled(map[string]bool{"c": true, "a": true, "b": false}) {
		ids = append(ids, p.ID())
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "c" {
		t.Fatalf("enabled = %v", ids)
	}
	if got := s.Enabled(nil); got == nil || len(got) != 0 {
		t.Fatalf("nothing enabled must be an empty list: %v", got)
	}
}

// YouTube's search answers videos only: a query naming a channel lists its videos, and the channel itself is put first.
func TestTheChannelAQueryNamesComesFirstEvenWhenOnlyItsVideosWereFound(t *testing.T) {
	t.Parallel()
	results := []platform.Result{
		{Video: &platform.Video{ID: "v1", Platform: "youtube", ChannelID: "UCchonks", ChannelName: "mrchonks"}},
		{Video: &platform.Video{ID: "v2", Platform: "youtube", ChannelID: "UCchonks", ChannelName: "mrchonks"}},
		{Video: &platform.Video{ID: "v3", Platform: "youtube", ChannelID: "UCother", ChannelName: "Someone else"}},
	}
	got := withMatchingChannels(results, "Mr Chonks")
	if len(got) != 4 || got[0].Channel == nil || got[0].Channel.ID != "UCchonks" || got[0].Channel.Name != "mrchonks" {
		t.Fatalf("the named channel must come first, once: %+v", got)
	}
	if again := withMatchingChannels(got, "mrchonks"); len(again) != 4 {
		t.Fatalf("a channel already returned as a card must not be added twice: %d results", len(again))
	}
	if unrelated := withMatchingChannels(results, "radar"); len(unrelated) != 3 {
		t.Fatalf("a query that names no channel adds nothing: %d results", len(unrelated))
	}
	if short := withMatchingChannels(results, "mr"); len(short) != 3 {
		t.Fatalf("a short query that merely appears in a name adds nothing: %d results", len(short))
	}
}
