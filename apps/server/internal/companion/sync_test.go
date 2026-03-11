package companion

import (
	"net/http"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

func at(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func TestSyncSubscriptionsCreatesTheNewOnesAndLeavesTheKnownOnes(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	f.manualSubscription(t, profile, channelID("1"))

	result, err := f.svc.SyncSubscriptions(device, []string{channelID("1"), channelID("2"), channelID("2"), "not-a-channel", ""}, false)
	if err != nil {
		t.Fatal(err)
	}
	if *result != (SubscriptionsResult{Created: 1, Unchanged: 1, Skipped: 2}) {
		t.Fatalf("result = %+v", result)
	}
	// The channel the user followed by hand stays theirs; the new one is marked as the sync's.
	if got := f.subscribed(t, profile); len(got) != 2 || got[channelID("1")] || !got[channelID("2")] {
		t.Fatalf("subscriptions = %v", got)
	}

	again, err := f.svc.SyncSubscriptions(device, []string{channelID("1"), channelID("2")}, true)
	if err != nil || *again != (SubscriptionsResult{Unchanged: 2}) {
		t.Fatalf("a second identical sync = %+v, %v", again, err)
	}

	// What is stored for a channel is a public stub: nothing but its identity.
	stub, err := f.store.FindChannel("youtube", channelID("2"))
	if err != nil || stub.GetString("name") != "" || len(stub.GetStringSlice("owners")) != 0 {
		t.Fatalf("stub = %v, %v", stub, err)
	}
}

// A complete snapshot removes what a sync created and the account no longer follows, and nothing else.
func TestCompleteSnapshotRemovesOnlySyncedSubscriptions(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	f.manualSubscription(t, profile, channelID("manual"))
	if _, err := f.svc.SyncSubscriptions(device, []string{channelID("a"), channelID("b"), channelID("c")}, false); err != nil {
		t.Fatal(err)
	}

	// Not complete: nothing is removed however short the list.
	result, err := f.svc.SyncSubscriptions(device, []string{channelID("a")}, false)
	if err != nil || result.Removed != 0 || len(f.subscribed(t, profile)) != 4 {
		t.Fatalf("an incomplete snapshot removed subscriptions: %+v %v", result, err)
	}

	result, err = f.svc.SyncSubscriptions(device, []string{channelID("a"), channelID("b")}, true)
	if err != nil || *result != (SubscriptionsResult{Removed: 1, Unchanged: 2}) {
		t.Fatalf("result = %+v, %v", result, err)
	}
	got := f.subscribed(t, profile)
	if len(got) != 3 || got[channelID("manual")] || !got[channelID("a")] || !got[channelID("b")] {
		t.Fatalf("subscriptions = %v", got)
	}

	// The manual subscription is not in the snapshot and survives it.
	result, err = f.svc.SyncSubscriptions(device, []string{channelID("a")}, true)
	if err != nil || result.Removed != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	got = f.subscribed(t, profile)
	if synced, present := got[channelID("manual")]; !present || synced || len(got) != 2 || !got[channelID("a")] {
		t.Fatalf("subscriptions = %v", got)
	}
}

// A snapshot with nothing usable in it is far more likely a failed read than an account that
// unsubscribed from everything: it removes nothing.
func TestEmptySnapshotRemovesNothing(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	if _, err := f.svc.SyncSubscriptions(device, []string{channelID("a"), channelID("b")}, false); err != nil {
		t.Fatal(err)
	}
	for _, channels := range [][]string{nil, {}, {"not-a-channel"}} {
		result, err := f.svc.SyncSubscriptions(device, channels, true)
		if err != nil || result.Removed != 0 {
			t.Fatalf("snapshot %v: %+v %v", channels, result, err)
		}
	}
	if got := f.subscribed(t, profile); len(got) != 2 {
		t.Fatalf("subscriptions = %v", got)
	}
}

// Subscriptions are the profile's own: the sync of another profile's device never touches them.
func TestCompleteSnapshotStaysWithinItsProfile(t *testing.T) {
	f := newFixture(t)
	device, user, profile := f.device(t)
	other := f.profile(t, user, "Other")
	_, otherDevice := f.paired(t, user, other, "Firefox")
	if _, err := f.svc.SyncSubscriptions(device, []string{channelID("a")}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SyncSubscriptions(otherDevice, []string{channelID("b")}, true); err != nil {
		t.Fatal(err)
	}
	if got := f.subscribed(t, profile); len(got) != 1 || !got[channelID("a")] {
		t.Fatalf("subscriptions = %v", got)
	}
}

// A creator is a link between channels: removing a synced channel that shared one leaves it with a single
// member, which is not kept.
func TestRemovingASyncedSubscriptionPrunesItsCreator(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	manual := f.manualSubscription(t, profile, channelID("manual"))
	if _, err := f.svc.SyncSubscriptions(device, []string{channelID("a"), channelID("keep")}, false); err != nil {
		t.Fatal(err)
	}
	channel, err := f.store.FindChannel("youtube", channelID("a"))
	if err != nil {
		t.Fatal(err)
	}
	synced, err := f.app.FindFirstRecordByFilter("subscriptions", "channel = {:channel}", map[string]any{"channel": channel.Id})
	if err != nil {
		t.Fatal(err)
	}
	creator := f.record(t, "creators", map[string]any{"profile": profile.Id, "name": "Linked"})
	manual.Set("creator", creator.Id)
	synced.Set("creator", creator.Id)
	if err := f.app.Save(manual); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Save(synced); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SyncSubscriptions(device, []string{channelID("keep")}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.FindRecordById("creators", creator.Id); err == nil {
		t.Fatal("a creator with a single subscription was kept")
	}
	if _, err := f.app.FindRecordById("subscriptions", manual.Id); err != nil {
		t.Fatalf("the manual subscription was removed: %v", err)
	}
}

func TestSyncRequiresTheYouTubeSourceToBeEnabled(t *testing.T) {
	f := newFixture(t)
	device, user, profile := f.device(t)
	if err := sources.WriteActivation(f.app, user.Id, "youtube", false); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.SyncSubscriptions(device, []string{channelID("a")}, true)
	requireCode(t, err, http.StatusConflict, "SOURCE_DISABLED")
	_, err = f.svc.SyncHistory(device, []HistoryEntry{{VideoID: videoID("a"), WatchedAt: at(now.Add(-time.Hour))}})
	requireCode(t, err, http.StatusConflict, "SOURCE_DISABLED")
	if got := f.subscribed(t, profile); len(got) != 0 || f.history(t, profile, videoID("a")) != nil {
		t.Fatal("a disabled source was synced")
	}
	devices, err := f.svc.Devices(user.Id)
	if err != nil || devices[0].LastSyncAt != "" {
		t.Fatalf("a refused sync marked the device as synced: %+v %v", devices, err)
	}
}

func TestSyncMarksTheDeviceAsSynced(t *testing.T) {
	f := newFixture(t)
	device, user, _ := f.device(t)
	if _, err := f.svc.SyncSubscriptions(device, []string{channelID("a")}, false); err != nil {
		t.Fatal(err)
	}
	devices, err := f.svc.Devices(user.Id)
	if err != nil || devices[0].LastSyncAt != at(now) {
		t.Fatalf("lastSyncAt = %+v %v", devices, err)
	}
}

// A device revoked while its sync runs fails the sync, which then writes nothing.
func TestSyncOfARevokedDeviceWritesNothing(t *testing.T) {
	f := newFixture(t)
	device, user, profile := f.device(t)
	if err := f.svc.Revoke(user.Id, device.ID); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.SyncSubscriptions(device, []string{channelID("a")}, false)
	requireCode(t, err, http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
	if got := f.subscribed(t, profile); len(got) != 0 {
		t.Fatalf("subscriptions = %v", got)
	}
}

func TestSyncRefusesMoreThanTheLimits(t *testing.T) {
	f := newFixture(t)
	device, _, _ := f.device(t)
	_, err := f.svc.SyncSubscriptions(device, make([]string, MaxChannels+1), false)
	requireCode(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	_, err = f.svc.SyncHistory(device, make([]HistoryEntry, MaxHistoryEntries+1))
	requireCode(t, err, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestSyncHistoryCreatesEntries(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	watched := now.Add(-2 * time.Hour)

	result, err := f.svc.SyncHistory(device, []HistoryEntry{
		{VideoID: videoID("a"), WatchedAt: at(watched), Position: new(123.4), Duration: new(600.0), Watched: false},
		{VideoID: videoID("b"), WatchedAt: at(watched), Position: new(9999.0), Duration: new(600.0), Watched: true},
		{VideoID: videoID("c"), WatchedAt: at(watched), Position: new(-5.0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *result != (HistoryResult{Created: 3}) {
		t.Fatalf("result = %+v", result)
	}
	a := f.history(t, profile, videoID("a"))
	if a.GetFloat("position") != 123.4 || a.GetFloat("video_duration") != 600 || a.GetBool("watched") || a.GetFloat("duration_watched") != 0 || a.GetDateTime("watched_at").Time().Unix() != watched.Unix() {
		t.Fatalf("entry a = %v", a.PublicExport())
	}
	// A position past the end is clamped to the duration; a negative one to zero, with no duration stored.
	b := f.history(t, profile, videoID("b"))
	if b.GetFloat("position") != 600 || !b.GetBool("watched") {
		t.Fatalf("entry b = %v", b.PublicExport())
	}
	c := f.history(t, profile, videoID("c"))
	if c.GetFloat("position") != 0 || c.GetFloat("video_duration") != 0 {
		t.Fatalf("entry c = %v", c.PublicExport())
	}
}

func TestSyncHistorySkipsEntriesWithoutAUsableDateOrVideo(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	result, err := f.svc.SyncHistory(device, []HistoryEntry{
		{VideoID: videoID("a"), WatchedAt: "yesterday"},
		{VideoID: videoID("b"), WatchedAt: ""},
		{VideoID: videoID("c"), WatchedAt: "0001-01-01T00:00:00Z"},
		{VideoID: videoID("d"), WatchedAt: at(now.Add(25 * time.Hour))},
		{VideoID: "bad id", WatchedAt: at(now.Add(-time.Hour))},
		{VideoID: videoID("e"), WatchedAt: at(now.Add(23 * time.Hour))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *result != (HistoryResult{Created: 1, Skipped: 5}) {
		t.Fatalf("result = %+v", result)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if f.history(t, profile, videoID(id)) != nil {
			t.Fatalf("entry %s was stored", id)
		}
	}
	// A skipped entry stores no stub either.
	if _, err := f.store.FindVideo("youtube", videoID("a")); err == nil {
		t.Fatal("an entry with a bad date created a video")
	}
	if f.history(t, profile, videoID("e")) == nil {
		t.Fatal("a watch within a day of the future (clock drift) must be kept")
	}
}

func TestSyncHistoryKeepsTheFirstEntryOfARepeatedVideo(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	first := now.Add(-48 * time.Hour)
	result, err := f.svc.SyncHistory(device, []HistoryEntry{
		{VideoID: videoID("a"), WatchedAt: at(first), Position: new(10.0)},
		{VideoID: videoID("a"), WatchedAt: at(now.Add(-time.Hour)), Position: new(500.0), Watched: true},
	})
	if err != nil || *result != (HistoryResult{Created: 1}) {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if entry := f.history(t, profile, videoID("a")); entry.GetFloat("position") != 10 || entry.GetBool("watched") {
		t.Fatalf("entry = %v", entry.PublicExport())
	}
}

func TestSyncHistoryMergesIntoWhatTheProfileHas(t *testing.T) {
	f := newFixture(t)
	device, _, profile := f.device(t)
	stored := now.Add(-100 * time.Hour)
	if _, err := f.svc.SyncHistory(device, []HistoryEntry{
		{VideoID: videoID("rewatch"), WatchedAt: at(stored), Position: new(100.0), Duration: new(600.0), Watched: true},
		{VideoID: videoID("upgrade"), WatchedAt: at(stored), Position: new(100.0), Duration: new(600.0)},
		{VideoID: videoID("same"), WatchedAt: at(stored), Position: new(100.0), Duration: new(600.0), Watched: true},
		{VideoID: videoID("recent"), WatchedAt: at(stored), Position: new(100.0), Duration: new(600.0)},
		{VideoID: videoID("noinfo"), WatchedAt: at(stored), Position: new(100.0), Duration: new(600.0)},
	}); err != nil {
		t.Fatal(err)
	}

	later := stored.Add(25 * time.Hour)
	result, err := f.svc.SyncHistory(device, []HistoryEntry{
		// More than a day after the stored watch: another viewing, which replaces date and progress, and
		// a video watched stays watched whatever the new viewing says.
		{VideoID: videoID("rewatch"), WatchedAt: at(later), Position: new(30.0), Duration: new(700.0), Watched: false},
		// Not a re-watch, but the video is now watched: only that is taken, and the progress stays.
		{VideoID: videoID("upgrade"), WatchedAt: at(stored.Add(time.Hour)), Position: new(550.0), Watched: true},
		// Nothing new.
		{VideoID: videoID("same"), WatchedAt: at(stored.Add(24 * time.Hour)), Position: new(1.0), Watched: false},
		// An older watch never moves the stored one.
		{VideoID: videoID("recent"), WatchedAt: at(stored.Add(-time.Hour)), Position: new(5.0)},
		// A re-watch without progress or duration keeps both.
		{VideoID: videoID("noinfo"), WatchedAt: at(later), Watched: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *result != (HistoryResult{Updated: 3, Unchanged: 2}) {
		t.Fatalf("result = %+v", result)
	}

	rewatch := f.history(t, profile, videoID("rewatch"))
	if rewatch.GetDateTime("watched_at").Time().Unix() != later.Unix() || rewatch.GetFloat("position") != 30 || rewatch.GetFloat("video_duration") != 700 || !rewatch.GetBool("watched") {
		t.Fatalf("rewatch = %v", rewatch.PublicExport())
	}
	upgrade := f.history(t, profile, videoID("upgrade"))
	if upgrade.GetDateTime("watched_at").Time().Unix() != stored.Unix() || upgrade.GetFloat("position") != 100 || !upgrade.GetBool("watched") {
		t.Fatalf("upgrade = %v", upgrade.PublicExport())
	}
	same := f.history(t, profile, videoID("same"))
	if same.GetFloat("position") != 100 || !same.GetBool("watched") {
		t.Fatalf("same = %v", same.PublicExport())
	}
	recent := f.history(t, profile, videoID("recent"))
	if recent.GetDateTime("watched_at").Time().Unix() != stored.Unix() || recent.GetFloat("position") != 100 {
		t.Fatalf("recent = %v", recent.PublicExport())
	}
	noinfo := f.history(t, profile, videoID("noinfo"))
	if noinfo.GetDateTime("watched_at").Time().Unix() != later.Unix() || noinfo.GetFloat("position") != 100 || noinfo.GetFloat("video_duration") != 600 || !noinfo.GetBool("watched") {
		t.Fatalf("noinfo = %v", noinfo.PublicExport())
	}
}

// What the history of another profile holds is not this profile's to change.
func TestSyncHistoryStaysWithinItsProfile(t *testing.T) {
	f := newFixture(t)
	device, user, _ := f.device(t)
	other := f.profile(t, user, "Other")
	_, otherDevice := f.paired(t, user, other, "Firefox")
	watched := now.Add(-time.Hour)
	if _, err := f.svc.SyncHistory(otherDevice, []HistoryEntry{{VideoID: videoID("a"), WatchedAt: at(watched), Position: new(50.0)}}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.SyncHistory(device, []HistoryEntry{{VideoID: videoID("a"), WatchedAt: at(watched)}})
	if err != nil || result.Created != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if entry := f.history(t, other, videoID("a")); entry.GetFloat("position") != 50 {
		t.Fatalf("entry = %v", entry.PublicExport())
	}
}
