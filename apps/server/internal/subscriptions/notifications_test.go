package subscriptions

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// release stores a video of the channel, published at the given stored date, that Viyu first stored at created.
func (f *fixture) release(t testing.TB, channel *core.Record, id, published string, created time.Time, live bool, owners ...string) {
	t.Helper()
	record := f.record(t, "videos", map[string]any{"platform": channel.GetString("platform"), "platform_id": id, "channel": channel.Id, "published": published, "expires_at": "2030-01-01 00:00:00.000Z", "live": live, "owners": owners})
	if _, err := f.app.DB().NewQuery("UPDATE videos SET created = {:created} WHERE id = {:id}").Bind(dbx.Params{"created": created.UTC().Format(platform.StoredLayout), "id": record.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
}

// lookedAt sets the marker of the profile.
func (f *fixture) lookedAt(t testing.TB, profile *core.Record, at time.Time) {
	t.Helper()
	profile.Set("notifications_seen_at", at.UTC().Format(platform.StoredLayout))
	if err := f.app.Save(profile); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) marker(t testing.TB, profile *core.Record) string {
	t.Helper()
	record, err := f.app.FindRecordById(profilesCollection, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("notifications_seen_at")
}

func (f *fixture) news(t testing.TB, user, profile *core.Record) Notifications {
	t.Helper()
	answer, err := f.service.NewVideos(user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func stored(t time.Time) string { return t.UTC().Format(platform.StoredLayout) }

// midnight is how a publication known to the day is stored.
func midnight(t time.Time) string { return platform.StartOfDayStored(t.UTC().Truncate(24 * time.Hour)) }

func TestNewVideosFirstLookOnlyStartsTheCount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "news-first@example.test")
	profile := f.profile(t, owner, "News")
	if !profile.GetDateTime("notifications_seen_at").IsZero() {
		t.Fatal("a new profile starts with a marker")
	}
	subscription := f.subscribe(t, profile, alpha, "first", "First")
	f.release(t, f.channelOf(t, subscription), "v1", midnight(time.Now()), time.Now().Add(time.Hour), false)

	before := time.Now().UTC().Truncate(time.Millisecond)
	first := f.news(t, owner, profile)
	if len(first.Items) != 0 || first.AsOf == "" {
		t.Fatalf("first look: %+v", first)
	}
	marker := f.marker(t, profile)
	if marker == "" || marker != first.AsOf || marker < stored(before) {
		t.Fatalf("the first look must set the marker to its asOf: %q %q", marker, first.AsOf)
	}
	// It does not move again.
	if again := f.news(t, owner, profile); f.marker(t, profile) != marker || len(again.Items) != 1 {
		t.Fatalf("second look: marker %q (was %q) %+v", f.marker(t, profile), marker, again)
	}
}

func TestNewVideosCountReleasesSinceTheLastLookPerCreator(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "news@example.test")
	profile := f.profile(t, owner, "News")
	marker := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	f.lookedAt(t, profile, marker)

	first := f.subscribe(t, profile, alpha, "news-a", "News Creator")
	second := f.subscribe(t, profile, beta, "news-b", "News Creator")
	solo := f.subscribe(t, profile, alpha, "solo", "Solo")
	f.link(t, profile, "One creator", first, second)
	day := midnight(time.Now())
	stamp := time.Now().UTC()
	f.release(t, f.channelOf(t, first), "y1", day, stamp, false)
	f.release(t, f.channelOf(t, first), "y2", day, stamp, false)
	f.release(t, f.channelOf(t, second), "t1", day, stamp, false)
	f.release(t, f.channelOf(t, solo), "s1", day, stamp, false)
	// Not new: a live stream, a back catalog stored just now, and a video stored before the marker.
	f.release(t, f.channelOf(t, solo), "s-live", day, stamp, true)
	f.release(t, f.channelOf(t, solo), "s-old-release", midnight(marker.AddDate(0, 0, -30)), stamp, false)
	f.release(t, f.channelOf(t, solo), "s-before", day, marker.Add(-time.Minute), false)

	result := f.news(t, owner, profile)
	if len(result.Items) != 2 {
		t.Fatalf("a creator linked on two platforms is one entry: %+v", result.Items)
	}
	merged, other := result.Items[0], result.Items[1]
	if merged.Name != "News Creator" || merged.Count != 3 || merged.Platform != alpha || merged.ChannelRecordID != f.channelOf(t, first).Id || merged.ChannelID != "news-a" {
		t.Fatalf("linked creator: %+v", merged)
	}
	if other.Name != "Solo" || other.Count != 1 || other.ChannelRecordID != f.channelOf(t, solo).Id {
		t.Fatalf("solo creator must count only its genuinely new video: %+v", other)
	}
	// On the wire, the channel is addressed by its record id.
	data, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct{ ChannelID, ChannelRecordID string }
	if err := json.Unmarshal(data, &wire); err != nil || wire.ChannelID != merged.ChannelRecordID {
		t.Fatalf("serialized %s %v", data, err)
	}

	// Marking seen clears the list, and a later video shows up again.
	if err := f.service.MarkSeen(owner.Id, profile.Id, stored(time.Now().Add(30*time.Second)), ""); err != nil {
		t.Fatal(err)
	}
	if again := f.news(t, owner, profile); len(again.Items) != 0 {
		t.Fatalf("seen videos came back: %+v", again.Items)
	}
	f.release(t, f.channelOf(t, solo), "s2", day, time.Now().Add(45*time.Second), false)
	if later := f.news(t, owner, profile); len(later.Items) != 1 || later.Items[0].Count != 1 {
		t.Fatalf("a new release: %+v", later.Items)
	}
}

func TestNewVideosLeaveOutPlatformsTheAccountTurnedOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "news-activation@example.test")
	profile := f.profile(t, owner, "Activation")
	f.lookedAt(t, profile, time.Now().Add(-time.Hour))
	day := midnight(time.Now())
	for _, channel := range []struct{ platform, id string }{{alpha, "active"}, {beta, "muted"}} {
		subscription := f.subscribe(t, profile, channel.platform, channel.id, channel.id)
		f.release(t, f.channelOf(t, subscription), "v-"+channel.id, day, time.Now(), false)
	}
	if both := f.news(t, owner, profile); len(both.Items) != 2 {
		t.Fatalf("both platforms release something: %+v", both.Items)
	}
	if err := f.sources.SetEnabled(owner.Id, beta, false); err != nil {
		t.Fatal(err)
	}
	if only := f.news(t, owner, profile); len(only.Items) != 1 || only.Items[0].Platform != alpha {
		t.Fatalf("a disabled platform must not notify: %+v", only.Items)
	}
}

func TestNewVideosLeaveOutVideosAnotherAccountCachedPrivately(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, stranger := f.user(t, "news-private@example.test"), f.user(t, "news-private-other@example.test")
	profile := f.profile(t, owner, "Private")
	f.lookedAt(t, profile, time.Now().Add(-time.Hour))
	channel := f.channelOf(t, f.subscribe(t, profile, alpha, "private", "Private channel"))
	day := midnight(time.Now())
	f.release(t, channel, "public-one", day, time.Now(), false)
	f.release(t, channel, "mine", day, time.Now(), false, owner.Id)
	f.release(t, channel, "shared", day, time.Now(), false, stranger.Id, owner.Id)
	f.release(t, channel, "theirs", day, time.Now(), false, stranger.Id)
	result := f.news(t, owner, profile)
	if len(result.Items) != 1 || result.Items[0].Count != 3 {
		t.Fatalf("only public videos and the account's own count: %+v", result.Items)
	}
}

func TestNewVideosIgnoreWhatWasPublishedBeforeTheSubscription(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "news-subscribed@example.test")
	profile := f.profile(t, owner, "Subscribed")
	// The profile last looked three days ago, then subscribed just now.
	f.lookedAt(t, profile, time.Now().AddDate(0, 0, -3))
	subscription := f.subscribe(t, profile, beta, "fresh", "Fresh")
	channel := f.channelOf(t, subscription)
	subscribed := subscription.GetDateTime("created").Time().UTC()
	at := subscribed.Add(time.Second)
	// Fetched by the same job as the rest of the catalog, yet released before the subscription:
	f.release(t, channel, "yesterday", midnight(subscribed.AddDate(0, 0, -1)), at, false)
	f.release(t, channel, "earlier", stored(subscribed.Add(-time.Hour)), at, false)
	// Genuinely released after the subscription: an exact time, and a day-precision date from that day on.
	f.release(t, channel, "after", stored(subscribed.Add(time.Minute)), at, false)
	f.release(t, channel, "undated-today", midnight(subscribed), at, false)

	result := f.news(t, owner, profile)
	if len(result.Items) != 1 || result.Items[0].Count != 2 {
		t.Fatalf("only the two videos released since subscribing are new: %+v", result.Items)
	}
}

func TestMarkSeenForOneCreatorKeepsTheOthers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "news-per-channel@example.test")
	profile := f.profile(t, owner, "PerChannel")
	alphaSub := f.subscribe(t, profile, alpha, "alpha-creator", "Alpha")
	betaSub := f.subscribe(t, profile, beta, "beta-creator", "Beta")
	groupedA := f.subscribe(t, profile, alpha, "grouped-a", "Grouped")
	groupedB := f.subscribe(t, profile, beta, "grouped-b", "Grouped")
	if grouped, err := f.service.AutoGroup(profile.Id); err != nil || grouped != 1 {
		t.Fatalf("auto group: %d %v", grouped, err)
	}
	day := midnight(time.Now())
	for id, subscription := range map[string]*core.Record{"alpha-video": alphaSub, "beta-video": betaSub, "grouped-video-a": groupedA, "grouped-video-b": groupedB} {
		f.release(t, f.channelOf(t, subscription), id, day, time.Now(), false)
	}
	// The profile looked an hour ago, so everything released since counts as new.
	marker := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	f.lookedAt(t, profile, marker)
	listed := f.news(t, owner, profile)
	if len(listed.Items) != 3 {
		t.Fatalf("three entries (the grouped pair merges): %+v", listed.Items)
	}
	asOf := stored(time.Now().Add(30 * time.Second))
	alphaChannel := f.channelOf(t, alphaSub)
	if err := f.service.MarkSeen(owner.Id, profile.Id, asOf, alphaChannel.Id); err != nil {
		t.Fatal(err)
	}
	remaining := f.news(t, owner, profile)
	if len(remaining.Items) != 2 {
		t.Fatalf("alpha went quiet, the rest stays: %+v", remaining.Items)
	}
	for _, item := range remaining.Items {
		if item.ChannelRecordID == alphaChannel.Id {
			t.Fatalf("alpha still listed: %+v", remaining.Items)
		}
	}
	// The profile-wide marker did not move.
	if got := f.marker(t, profile); got != stored(marker) {
		t.Fatalf("a per-creator mark moved the global marker: %q want %q", got, stored(marker))
	}
	// A late asOf cannot bring a creator back: the marker only moves forward.
	if err := f.service.MarkSeen(owner.Id, profile.Id, stored(marker), alphaChannel.Id); err != nil {
		t.Fatal(err)
	}
	if again := f.news(t, owner, profile); len(again.Items) != 2 {
		t.Fatalf("a late asOf resurrected alpha: %+v", again.Items)
	}
	if n := f.count(t, seenCollection); n != 1 {
		t.Fatalf("one marker per channel: %d", n)
	}
	// Marking one half of a grouped creator marks the whole group, in one go.
	if err := f.service.MarkSeen(owner.Id, profile.Id, asOf, f.channelOf(t, groupedA).Id); err != nil {
		t.Fatal(err)
	}
	last := f.news(t, owner, profile)
	if len(last.Items) != 1 || last.Items[0].ChannelRecordID != f.channelOf(t, betaSub).Id {
		t.Fatalf("the grouped creator went quiet everywhere: %+v", last.Items)
	}
	if n := f.count(t, seenCollection); n != 3 {
		t.Fatalf("markers after marking a group: %d, want alpha + the two grouped channels", n)
	}
}

func TestMarkSeenMarkerOnlyMovesForwardAndStaysWithItsOwner(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, stranger := f.user(t, "news-owner@example.test"), f.user(t, "news-stranger@example.test")
	profile := f.profile(t, owner, "Mine")
	first := f.news(t, owner, profile)
	marker := f.marker(t, profile)
	if marker == "" {
		t.Fatal("the first look did not set the marker")
	}
	if err := f.service.MarkSeen(owner.Id, profile.Id, "2001-01-01 00:00:00.000Z", ""); err != nil || f.marker(t, profile) != marker {
		t.Fatalf("the marker moved backwards: %v %s -> %s", err, marker, f.marker(t, profile))
	}
	later := stored(time.Now().Add(30 * time.Second))
	if err := f.service.MarkSeen(owner.Id, profile.Id, later, ""); err != nil || f.marker(t, profile) != later {
		t.Fatalf("the marker did not move forward: %v %s", err, f.marker(t, profile))
	}
	for name, test := range map[string]struct {
		user, profile, asOf, channel string
		status                       int
		code                         string
	}{
		"another account":      {stranger.Id, profile.Id, first.AsOf, "", http.StatusNotFound, apierr.CodeNotFound},
		"invalid date":         {owner.Id, profile.Id, "yesterday", "", http.StatusBadRequest, apierr.CodeInvalidRequest},
		"empty date":           {owner.Id, profile.Id, "", "", http.StatusBadRequest, apierr.CodeInvalidRequest},
		"future date":          {owner.Id, profile.Id, "2999-01-01 00:00:00.000Z", "", http.StatusBadRequest, apierr.CodeInvalidRequest},
		"unknown profile":      {owner.Id, "missing", first.AsOf, "", http.StatusNotFound, apierr.CodeNotFound},
		"channel not followed": {owner.Id, profile.Id, first.AsOf, "missing", http.StatusNotFound, apierr.CodeNotFound},
	} {
		err := f.service.MarkSeen(test.user, test.profile, test.asOf, test.channel)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		requireCode(t, err, test.status, test.code)
	}
	if _, err := f.service.NewVideos(stranger.Id, profile.Id); err == nil {
		t.Fatal("another account read the notifications")
	} else {
		requireCode(t, err, http.StatusNotFound, apierr.CodeNotFound)
	}
}
