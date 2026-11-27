package feed

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/search"
)

type discoveryPlatform struct {
	fakePlatform
	lookup func(context.Context, platform.Session, string, int) (platform.SearchPage, error)
}

func (p discoveryPlatform) Search(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
	return p.lookup(ctx, session, query, page)
}

func (f *fixture) discover(lookup func(context.Context, platform.Session, string, int) (platform.SearchPage, error)) *Discovery {
	p := discoveryPlatform{fakePlatform: fakePlatform{t: f.t, id: alpha, calls: f.platform}, lookup: lookup}
	d := NewDiscovery(search.New(platform.NewRegistry(p), f.store, nil), nil)
	f.svc.discovery = d
	return d
}

func TestFeedDiscoversOutsideSubscriptionsAndFiltersBeforeRanking(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := f.user("discovery@example.test")
	profile, sibling := f.profile(user, "Mine"), f.profile(user, "Sibling")
	ids := f.put(
		video{id: "liked", title: "Découvrir l’astronomie", channel: "UCseed", tags: []string{"astronomie"}},
		video{id: "recent", channel: "UCfollowed", published: time.Now().UTC().Format(platform.DayLayout)},
		video{id: "older", channel: "UCnew", published: "2020-01-01"},
		video{id: "same-channel", channel: "UCnew", published: "2020-01-02"},
		video{id: "different-channel", channel: "UCdifferent", published: "2020-01-03"},
		video{id: "watched", channel: "UChidden"},
		video{id: "dismissed", channel: "UChidden"},
		video{id: "subscriber-only", channel: "UChidden", subscriberOnly: true},
		video{id: "sibling-watched", channel: "UCsibling"},
	)
	f.like(profile, ids["liked"])
	f.subscribeTo(profile, "UCfollowed")
	settings := f.save("settings", map[string]any{"user": user.Id, "hide_watched": true, "hide_subscriber_only": true})
	f.save("watch_history", map[string]any{"profile": profile.Id, "video": ids["watched"], "watched": true, "watched_at": time.Now()})
	f.save("video_reactions", map[string]any{"profile": profile.Id, "video": ids["dismissed"], "reaction": "not_interested"})
	f.save("watch_history", map[string]any{"profile": sibling.Id, "video": ids["sibling-watched"], "watched": true, "watched_at": time.Now()})
	var calls atomic.Int32
	f.discover(func(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
		calls.Add(1)
		if session.Connected() || query != "astronomie" || page != 1 {
			t.Errorf("discovery must use the active profile's public topic search: session=%+v query=%q page=%d", session, query, page)
		}
		var results []platform.Result
		for _, native := range []string{"older", "same-channel", "different-channel", "watched", "dismissed", "subscriber-only", "sibling-watched", "older"} {
			v, err := f.store.FindVideo(alpha, native)
			if err != nil {
				return platform.SearchPage{}, err
			}
			video := f.store.VideoOf(v)
			results = append(results, platform.Result{Video: &video})
		}
		return platform.SearchPage{Results: results}, nil
	})

	got := f.feed(user, profile)
	if len(got) < 2 || got[0] != ids["older"] || slices.Index(got, ids["same-channel"]) < slices.Index(got, ids["different-channel"]) {
		t.Fatalf("public search relevance or channel diversity was lost to chronology: got=%v ids=%v", got, ids)
	}
	if !sameSet(got, want(ids, "older", "same-channel", "different-channel", "sibling-watched", "recent")) {
		t.Fatalf("hidden entries, duplicates, or sibling history polluted discovery: %v", got)
	}
	if again := f.feed(user, profile); !reflect.DeepEqual(again, got) || calls.Load() != 1 {
		t.Fatalf("reopening the feed repeated the public lookup or changed ranking: %v, calls=%d", again, calls.Load())
	}
	// Cached search pages must not cache the profile's visibility choices.
	settings.Set("hide_watched", false)
	settings.Set("hide_subscriber_only", false)
	if err := f.app.Save(settings); err != nil {
		t.Fatal(err)
	}
	visible := f.feed(user, profile)
	if !slices.Contains(visible, ids["watched"]) || !slices.Contains(visible, ids["subscriber-only"]) || slices.Contains(visible, ids["dismissed"]) {
		t.Fatalf("cached discovery ignored changed preferences: %v", visible)
	}
	if err := f.accounts.SetEnabled(user.Id, alpha, false); err != nil {
		t.Fatal(err)
	}
	if disabled := f.feed(user, profile); len(disabled) != 0 || calls.Load() != 1 {
		t.Fatalf("disabled platform still queried or exposed cached results: %v, calls=%d", disabled, calls.Load())
	}
}

func TestDiscoverySeedsAreProfileScopedAndNeedNoPlatformConnection(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := f.user("seeds@example.test")
	liked, untagged, subscribed, cold := f.profile(user, "Liked"), f.profile(user, "Untagged"), f.profile(user, "Subscribed"), f.profile(user, "Cold")
	ids := f.put(
		video{id: "space-seed", title: "Space science", channel: "UCspace", tags: []string{"space", "space", "science"}},
		video{id: "cooking-seed", channel: "UCcooking"},
		video{id: "history-seed", channel: "UChistory", tags: []string{"history"}},
	)
	f.like(liked, ids["space-seed"])
	f.like(untagged, ids["cooking-seed"])
	f.subscribeTo(subscribed, "UChistory")
	var mu sync.Mutex
	var queries []string
	f.discover(func(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
		if session != (platform.Session{}) {
			t.Error("discovery borrowed an account's credentials")
		}
		mu.Lock()
		queries = append(queries, query)
		mu.Unlock()
		return platform.SearchPage{}, nil
	})
	for _, test := range []struct {
		profileID string
		want      []string
	}{
		{liked.Id, []string{"space", "science"}},
		{untagged.Id, []string{"cooking-seed"}},
		{subscribed.Id, []string{"history"}},
		{cold.Id, nil},
	} {
		queries = nil
		if _, err := f.svc.Feed(context.Background(), user.Id, test.profileID); err != nil {
			t.Fatal(err)
		}
		if !sameSet(queries, test.want) {
			t.Fatalf("profile=%s searched %v, want only its own signals %v", test.profileID, queries, test.want)
		}
	}
}

func TestDiscoveryRefreshesExpiredPagesAndDoesNotCacheFailures(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var calls atomic.Int32
	d := f.discover(func(ctx context.Context, session platform.Session, query string, page int) (platform.SearchPage, error) {
		if calls.Add(1) == 1 {
			return platform.SearchPage{}, errors.New("public search unavailable")
		}
		return platform.SearchPage{}, nil
	})
	now := time.Now()
	d.pages = cache.New[discoveryKey, []string](cache.Options{TTL: time.Hour, Max: 8, Now: func() time.Time { return now }})
	active := map[string]bool{alpha: true}
	if _, err := d.find(context.Background(), active, []string{"space"}); err == nil {
		t.Fatal("a failed public lookup was presented as an empty successful feed")
	}
	for range 2 {
		if _, err := d.find(context.Background(), active, []string{"space"}); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("a failure was cached, or a successful empty result was not: calls=%d", calls.Load())
	}
	now = now.Add(2 * time.Hour)
	if _, err := d.find(context.Background(), active, []string{"space"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expired public results were never refreshed: calls=%d", calls.Load())
	}
}

func TestWatchedSubscriptionsStillSeedDiscovery(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := f.user("watched-seeds@example.test")
	profile := f.profile(user, "Mine")
	seed := f.put(video{id: "geology-seed", channel: "UCgeology", tags: []string{"geology"}})["geology-seed"]
	f.subscribeTo(profile, "UCgeology")
	f.save("settings", map[string]any{"user": user.Id, "hide_watched": true})
	f.save("watch_history", map[string]any{"profile": profile.Id, "video": seed, "watched": true, "watched_at": time.Now()})
	f.discover(func(context.Context, platform.Session, string, int) (platform.SearchPage, error) {
		return platform.SearchPage{Results: []platform.Result{{Video: &platform.Video{
			ID: "geology-discovery", Platform: alpha, Title: "Volcanoes", ChannelID: "UCvolcanoes", ChannelName: "Volcanoes",
		}}}}, nil
	})
	videos, err := f.svc.Feed(context.Background(), user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 || videos[0].Title != "Volcanoes" || videos[0].RecordID == seed {
		t.Fatalf("hiding watched videos erased the subscription's discovery signal: %+v", videos)
	}
}

func TestDiscoveryDoesNotTurnDescriptiveTagsIntoInterests(t *testing.T) {
	for _, liked := range []bool{true, false} {
		name := "subscriptions"
		if liked {
			name = "likes"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			user := f.user("descriptive-tags@example.test")
			profile := f.profile(user, "Science")
			ids := f.put(
				video{id: "space-seed", title: "Explorer les trous noirs", channel: "UCspace", tags: []string{"français", "vidéo", "trous noirs"}},
				video{id: "earth-seed", title: "Comment naissent les volcans ?", channel: "UCearth", tags: []string{"français", "vidéo", "volcans"}},
			)
			f.save("settings", map[string]any{"user": user.Id, "hide_watched": true})
			for _, id := range ids {
				f.save("watch_history", map[string]any{"profile": profile.Id, "video": id, "watched": true, "watched_at": time.Now()})
				if liked {
					f.like(profile, id)
				}
			}
			if !liked {
				f.subscribeTo(profile, "UCspace")
				f.subscribeTo(profile, "UCearth")
			}
			f.discover(func(_ context.Context, _ platform.Session, query string, _ int) (platform.SearchPage, error) {
				v := platform.Video{Platform: alpha}
				switch {
				case strings.Contains(query, "trous noirs"):
					v.ID, v.Title, v.ChannelID = "black-holes", "La physique des trous noirs", "UCphysics"
				case strings.Contains(query, "volcans"):
					v.ID, v.Title, v.ChannelID = "volcanoes", "La formation des volcans", "UCgeology"
				default:
					v.ID, v.Title, v.ChannelID = "language-course", "Apprendre le français", "UClanguages"
				}
				return platform.SearchPage{Results: []platform.Result{{Video: &v}}}, nil
			})
			videos, err := f.svc.Feed(context.Background(), user.Id, profile.Id)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range videos {
				if v.ID == "language-course" {
					t.Fatalf("a shared descriptive tag became a language-learning interest: %+v", videos)
				}
			}
			if len(videos) != 2 {
				t.Fatalf("discovery lost the actual science subjects: %+v", videos)
			}
		})
	}
}

func TestDiscoveryKeepsExplicitLanguageLearningInterests(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := f.user("language-learning@example.test")
	profile := f.profile(user, "Languages")
	seed := f.put(video{id: "language-seed", title: "Apprendre le français : les verbes", channel: "UClanguages", tags: []string{"français"}})["language-seed"]
	f.like(profile, seed)
	f.discover(func(_ context.Context, _ platform.Session, query string, _ int) (platform.SearchPage, error) {
		if !strings.Contains(query, "français") {
			return platform.SearchPage{}, errors.New("the explicit language-learning subject was lost")
		}
		return platform.SearchPage{Results: []platform.Result{{Video: &platform.Video{
			ID: "language-discovery", Platform: alpha, Title: "Les temps du français", ChannelID: "UCgrammar",
		}}}}, nil
	})
	videos, err := f.svc.Feed(context.Background(), user.Id, profile.Id)
	if err != nil || len(videos) != 1 || videos[0].ID != "language-discovery" {
		t.Fatalf("a genuine language-learning interest was suppressed: %+v, %v", videos, err)
	}
}
