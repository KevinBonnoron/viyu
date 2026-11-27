package feed

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestFeedKeepsEligibleCacheWhenPublicDiscoveryFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := f.user("discovery-outage@example.test")
	profile := f.profile(user, "Mine")
	ids := f.put(
		video{id: "galaxy-available", channel: "UCgalaxy", tags: []string{"galaxy"}},
		video{id: "galaxy-watched", channel: "UCgalaxy"},
		video{id: "galaxy-dismissed", channel: "UCgalaxy"},
	)
	f.subscribeTo(profile, "UCgalaxy")
	f.save("settings", map[string]any{"user": user.Id, "hide_watched": true})
	f.save("watch_history", map[string]any{"profile": profile.Id, "video": ids["galaxy-watched"], "watched": true, "watched_at": time.Now()})
	f.save("video_reactions", map[string]any{"profile": profile.Id, "video": ids["galaxy-dismissed"], "reaction": "not_interested"})
	f.discover(func(context.Context, platform.Session, string, int) (platform.SearchPage, error) {
		return platform.SearchPage{}, errors.New("public search is unavailable")
	})
	if got := f.feed(user, profile); !sameSet(got, want(ids, "galaxy-available")) {
		t.Fatalf("a public search outage discarded eligible cache or exposed hidden videos: %v", got)
	}
	// With no eligible cache, the outage must remain an error rather than a fabricated successful empty feed.
	f.save("watch_history", map[string]any{"profile": profile.Id, "video": ids["galaxy-available"], "watched": true, "watched_at": time.Now()})
	if _, err := f.svc.Feed(context.Background(), user.Id, profile.Id); err == nil {
		t.Fatal("an outage with no eligible cached videos was reported as successful discovery")
	}
}

func TestFeedCancellationDoesNotFallBackToCache(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := f.user("discovery-cancellation@example.test")
	profile := f.profile(user, "Mine")
	f.put(video{id: "galaxy-available", channel: "UCgalaxy", tags: []string{"galaxy"}})
	f.subscribeTo(profile, "UCgalaxy")
	started := make(chan struct{})
	f.discover(func(ctx context.Context, _ platform.Session, _ string, _ int) (platform.SearchPage, error) {
		close(started)
		<-ctx.Done()
		return platform.SearchPage{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := f.svc.Feed(ctx, user.Id, profile.Id)
		result <- err
	}()
	select {
	case <-started:
	case err := <-result:
		t.Fatalf("the feed returned before the cancellable lookup started: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled request was replaced by cached results: %v", err)
	}
}

func TestFeedExcludesHiddenSubscriptionsBeforeCandidateLimit(t *testing.T) {
	for _, kind := range []string{"watched", "dismissed", "restricted"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			user := f.user("bounded-feed@example.test")
			profile, sibling := f.profile(user, "Mine"), f.profile(user, "Sibling")
			f.save("settings", map[string]any{"user": user.Id, "hide_watched": kind == "watched", "hide_subscriber_only": kind == "restricted"})
			newer := make([]platform.Video, candidatesPerQuery)
			for i := range newer {
				newer[i] = platform.Video{
					ID: fmt.Sprintf("newer-%03d", i), Platform: alpha, Title: "Galaxy archive", Tags: []string{"galaxy"},
					ChannelID: "UCgalaxy", ChannelName: "Galaxy", Published: "2025-01-01",
					SubscriberOnly: kind == "restricted", SubscriberChecked: true,
				}
			}
			if err := f.store.StoreVideos(newer, ""); err != nil {
				t.Fatal(err)
			}
			if kind != "restricted" {
				collection := "watch_history"
				if kind == "dismissed" {
					collection = "video_reactions"
				}
				if err := f.app.RunInTransaction(func(tx core.App) error {
					schema, err := tx.FindCollectionByNameOrId(collection)
					if err != nil {
						return err
					}
					for _, v := range newer {
						r := core.NewRecord(schema)
						r.Load(map[string]any{"profile": profile.Id, "video": v.RecordID})
						if kind == "watched" {
							r.Set("watched", true)
							r.Set("watched_at", time.Now())
						} else {
							r.Set("reaction", "not_interested")
						}
						if err := tx.Save(r); err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			eligible := f.put(video{id: "older-eligible", title: "Ocean exploration", channel: "UCgalaxy", tags: []string{"ocean"}, published: "2020-01-01"})["older-eligible"]
			f.save("watch_history", map[string]any{"profile": profile.Id, "video": eligible, "watched": false, "watched_at": time.Now(), "position": 30, "video_duration": 600})
			f.save("watch_history", map[string]any{"profile": sibling.Id, "video": eligible, "watched": true, "watched_at": time.Now()})
			f.save("video_reactions", map[string]any{"profile": sibling.Id, "video": eligible, "reaction": "not_interested"})
			live := platform.Video{ID: "older-live", Platform: alpha, Title: "Live", ChannelID: "UCgalaxy", ChannelName: "Galaxy", Published: "2019-01-01", Live: true}
			if err := f.store.StoreVideo(&live, ""); err != nil {
				t.Fatal(err)
			}
			f.save("watch_history", map[string]any{"profile": profile.Id, "video": live.RecordID, "watched": true, "watched_at": time.Now()})
			f.save("video_reactions", map[string]any{"profile": profile.Id, "video": live.RecordID, "reaction": "not_interested"})
			if kind == "restricted" {
				restricted := platform.Video{ID: "restricted-live", Platform: alpha, Title: "Restricted live", ChannelID: "UCgalaxy", ChannelName: "Galaxy", Published: "2019-01-01", Live: true, SubscriberOnly: true, SubscriberChecked: true}
				if err := f.store.StoreVideo(&restricted, ""); err != nil {
					t.Fatal(err)
				}
			}
			f.subscribeTo(profile, "UCgalaxy")
			f.discover(func(_ context.Context, _ platform.Session, query string, _ int) (platform.SearchPage, error) {
				if query != "galaxy" {
					t.Errorf("visibility preferences replaced the unfiltered discovery seeds with %q", query)
				}
				return platform.SearchPage{}, nil
			})
			if got := f.feed(user, profile); !sameSet(got, []string{eligible, live.RecordID}) {
				t.Fatalf("newer hidden rows consumed the candidate limit, a sibling hid an entry, or live rules changed: %v", got)
			}
		})
	}
}
