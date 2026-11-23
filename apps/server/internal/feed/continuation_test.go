package feed

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

func enableSeriesFixture(f *fixture) {
	registry := platform.NewRegistry(
		fakePlatform{t: f.t, id: alpha, calls: f.platform},
		fakePlatform{t: f.t, id: beta, calls: f.platform},
	)
	f.svc.seriesStore = series.NewStore(f.app, f.store, registry, series.NewParser(nil))
}

func setContinuationTitles(f *fixture, ids map[string]string) {
	f.t.Helper()
	for native, id := range ids {
		record, err := f.store.Video(id)
		if err != nil {
			f.t.Fatal(err)
		}
		record.Set("titles", map[string]string{"original": "Adventure #" + native})
		if err := f.app.Save(record); err != nil {
			f.t.Fatal(err)
		}
	}
}

func continuationID(t *testing.T, f *fixture, userID, anchorID string) string {
	t.Helper()
	got, err := f.svc.nextSeriesVideo(userID, anchorID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		return ""
	}
	return got.RecordID
}

func TestSeriesContinuationCanonicalAndCuratedOrder(t *testing.T) {
	for _, curated := range []bool{false, true} {
		t.Run(map[bool]string{false: "inferred", true: "curated"}[curated], func(t *testing.T) {
			f := newFixture(t)
			enableSeriesFixture(f)
			user := f.user("continuation@example.test")
			ids := f.put(video{id: "1", channel: "channel"}, video{id: "2", channel: "channel"}, video{id: "3", channel: "channel"}, video{id: "4", channel: "channel"})
			setContinuationTitles(f, ids)
			channel := f.channel(alpha, "channel", time.Hour)
			f.save("series", map[string]any{"channel": channel.Id, "name": "Adventure", "curated": curated, "videos": []string{ids["1"], ids["3"], ids["2"]}})
			want := ids["2"]
			if curated {
				want = ids["3"]
			}
			if got := continuationID(t, f, user.Id, ids["1"]); got != want {
				t.Fatalf("successor = %q, want %q", got, want)
			}
			last := ids["3"]
			if curated {
				last = ids["2"]
			}
			if got := continuationID(t, f, user.Id, last); got != "" {
				t.Fatalf("last episode successor = %q", got)
			}
			if got := continuationID(t, f, user.Id, ids["4"]); got != "" {
				t.Fatalf("unrelated video successor = %q", got)
			}
		})
	}
}

func TestSeriesContinuationVisibilityAndPlatform(t *testing.T) {
	f := newFixture(t)
	enableSeriesFixture(f)
	owner := f.user("continuation-owner@example.test")
	other := f.user("continuation-other@example.test")
	ids := f.put(video{id: "1", channel: "channel", platform: beta}, video{id: "2", channel: "channel", platform: beta, owner: owner.Id}, video{id: "3", channel: "channel", platform: beta})
	channel := f.channel(beta, "channel", time.Hour)
	f.save("series", map[string]any{"channel": channel.Id, "name": "Adventure", "curated": true, "videos": []string{ids["1"], ids["2"], ids["3"]}})
	if got := continuationID(t, f, other.Id, ids["1"]); got != "" {
		t.Fatalf("private successor exposed or skipped: %q", got)
	}
	if got := continuationID(t, f, other.Id, ids["2"]); got != "" {
		t.Fatalf("private anchor exposed successor: %q", got)
	}
	if got := continuationID(t, f, owner.Id, ids["1"]); got != ids["2"] {
		t.Fatalf("owner successor = %q, want %q", got, ids["2"])
	}
	if err := f.accounts.SetEnabled(owner.Id, beta, false); err != nil {
		t.Fatal(err)
	}
	if got := continuationID(t, f, owner.Id, ids["1"]); got != "" {
		t.Fatalf("disabled platform successor = %q", got)
	}
}

func TestSeriesContinuationMultipleMemberships(t *testing.T) {
	f := newFixture(t)
	enableSeriesFixture(f)
	user := f.user("continuation-multiple@example.test")
	ids := f.put(video{id: "1", channel: "channel"}, video{id: "2", channel: "channel"}, video{id: "3", channel: "channel"})
	channel := f.channel(alpha, "channel", time.Hour)
	f.save("series", map[string]any{"channel": channel.Id, "name": "Z series", "curated": true, "videos": []string{ids["1"], ids["3"]}})
	f.save("series", map[string]any{"channel": channel.Id, "name": "A series", "curated": true, "videos": []string{ids["1"], ids["2"]}})
	if got := continuationID(t, f, user.Id, ids["1"]); got != ids["2"] {
		t.Fatalf("successor = %q, want first named series' %q", got, ids["2"])
	}
}

func TestRecommendationsPinsEligibleContinuationBeyondCandidateLimit(t *testing.T) {
	f := newFixture(t)
	enableSeriesFixture(f)
	user := f.user("continuation-recommend@example.test")
	profile := f.profile(user, "Viewer")
	ids := f.put(
		video{id: "anchor", channel: "channel", published: "2017-01-01"},
		video{id: "next", channel: "channel", published: "2017-12-01"},
	)
	// Nearer, unrelated videos fill the forward window: the successor must be
	// fetched separately and pinned above their stronger temporal ranking.
	for i := range candidatesPerQuery + 1 {
		f.put(video{id: fmt.Sprintf("new-%03d", i), channel: "channel", published: "2017-06-01"})
	}
	channel := f.channel(alpha, "channel", time.Hour)
	f.save("series", map[string]any{"channel": channel.Id, "name": "Adventure", "curated": true, "videos": []string{ids["anchor"], ids["next"]}})
	got := f.recommend(user, profile, ids["anchor"])
	if len(got) != recommendationLimit || got[0] != ids["next"] {
		t.Fatalf("recommendations = %v, want successor first and %d results", got, recommendationLimit)
	}
	if slices.Contains(got[1:], ids["next"]) {
		t.Fatalf("duplicate successor: %v", got)
	}
	f.save("watch_history", map[string]any{"profile": profile.Id, "video": ids["next"], "watched_at": "2026-11-01 00:00:00.000Z"})
	got = f.recommend(user, profile, ids["anchor"])
	if slices.Contains(got, ids["next"]) {
		t.Fatalf("watched successor recommended: %v", got)
	}
}

func TestSeriesContinuationWithoutStoreOrAnchor(t *testing.T) {
	f := newFixture(t)
	user := f.user("continuation-missing@example.test")
	if got := continuationID(t, f, user.Id, "missing"); got != "" {
		t.Fatalf("continuation without store = %q", got)
	}
	enableSeriesFixture(f)
	if got := continuationID(t, f, user.Id, "missing"); got != "" {
		t.Fatalf("continuation without anchor = %q", got)
	}
}
