package app

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// The inference itself (titles, seasons, ordering) is tested in series and the worker in jobs; these tests are
// the HTTP surface: what a profile may read, what the answers hold and which events set the analysis going.
func TestSeriesEndpointsScopeToOwnedProfileSubscriptionsAndCatalogMembership(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t))
	yt := testPlatforms().Default().ID()
	owner := testUser(t, app, "series-owner@example.test")
	other := testUser(t, app, "series-other@example.test")
	token, otherToken := tokenOf(t, owner), tokenOf(t, other)
	first := acctRecord(t, app, "profiles", map[string]any{"user": owner.Id, "name": "First"})
	second := acctRecord(t, app, "profiles", map[string]any{"user": owner.Id, "name": "Second"})
	empty := profileOf(t, app, owner)
	foreign := profileOf(t, app, other)

	firstNative := serChannel("first")
	firstChannel, firstVideos := serSeedCatalog(t, app, firstNative, "Outer Wilds #1", "Outer Wilds #2")
	secondChannel, _ := serSeedCatalog(t, app, serChannel("second"), "Outer Wilds #1", "Outer Wilds #2")
	foreignChannel, _ := serSeedCatalog(t, app, serChannel("foreign"), "Private Series #1", "Private Series #2")
	// A video that was merely cached is no evidence of catalog membership.
	storedVideo(t, app, platform.Video{ID: "notamember01", ChannelID: firstNative, ChannelName: firstNative, Title: "Outer Wilds #3"})
	serSubscribe(t, app, first, firstChannel)
	serSubscribe(t, app, second, secondChannel)
	serSubscribe(t, app, foreign, foreignChannel)

	for _, profile := range []*core.Record{first, second} {
		serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool { return serComplete(a) && len(a.Series) == 1 })
	}
	for _, test := range []struct {
		path, token string
		status      int
		code        string
	}{
		{"/api/series", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"/api/series?profile=missing", token, http.StatusNotFound, "NOT_FOUND"},
		{"/api/series?profile=" + foreign.Id, token, http.StatusNotFound, "NOT_FOUND"},
		{"/api/series?profile=" + first.Id, otherToken, http.StatusNotFound, "NOT_FOUND"},
		{"/api/series?profile=" + first.Id, "", http.StatusUnauthorized, ""},
		{"/api/series/not-found?profile=" + first.Id, token, http.StatusNotFound, "NOT_FOUND"},
		{"/api/series/not-found", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"/api/series/not-found?profile=" + first.Id, "", http.StatusUnauthorized, ""},
	} {
		acctExpect(t, request(t, handler, test.path, test.token), test.status, test.code)
	}

	var firstSeries string
	for _, profile := range []*core.Record{first, second, empty} {
		answer := serList(t, handler, token, profile.Id, "")
		if answer.Sync.Status != jobs.Complete {
			t.Fatalf("sync=%+v", answer.Sync)
		}
		if profile == empty {
			if len(answer.Series) != 0 || answer.Sync.Loaded != 0 {
				t.Fatalf("a profile that follows nothing has series: %+v", answer)
			}
			if answer.Analysis.Channels == nil || len(answer.Analysis.Channels) != 0 || answer.Analysis.Metadata != (jobs.Counts{}) || answer.Analysis.OCR != (jobs.Counts{}) {
				t.Fatalf("empty analysis=%+v", answer.Analysis)
			}
			continue
		}
		if len(answer.Series) != 1 || answer.Series[0].VideoCount != 2 || answer.Sync.Loaded != 2 {
			t.Fatalf("answer=%+v", answer)
		}
		wantChannel := firstChannel
		if profile == second {
			wantChannel = secondChannel
		}
		// Everything is addressed by record id: the native id of the channel never leaves the server.
		if got := answer.Series[0]; got.ChannelID != wantChannel || got.ChannelRecordID != wantChannel || got.Platform != yt {
			t.Fatalf("series addressing: %+v", got)
		}
		channels := answer.Analysis.Channels
		if len(channels) != 1 || channels[0].ChannelID != wantChannel || channels[0].Platform != yt || channels[0].Status != jobs.Complete || channels[0].Loaded != 2 {
			t.Fatalf("profile analysis=%+v", answer.Analysis)
		}
		if profile == first {
			firstSeries = answer.Series[0].ID
		}
		detail := serGet(t, handler, token, answer.Series[0].ID, profile.Id)
		if len(detail.Series.Items) != 2 || detail.Series.Order != "episode" || detail.Sync.Status != jobs.Complete || detail.Sync.Loaded != 2 {
			t.Fatalf("detail=%+v", detail)
		}
	}
	if _, err := app.FindRecordById("series", firstSeries); err != nil {
		t.Fatalf("the listed id %q is not a series record: %v", firstSeries, err)
	}
	detail := serGet(t, handler, token, firstSeries, first.Id)
	members := map[string]bool{}
	for _, item := range detail.Series.Items {
		members[item.Video.ID] = true
		if item.Video.ChannelID != firstChannel {
			t.Fatalf("a member is not addressed by its channel's record id: %+v", item.Video)
		}
	}
	if !members[firstVideos[0]] || !members[firstVideos[1]] || len(members) != 2 {
		t.Fatalf("members=%v, want the two catalog videos %v", members, firstVideos)
	}

	// Another account never reads through a profile that is not its own.
	for _, test := range []struct {
		profile *core.Record
		token   string
	}{{foreign, token}, {first, otherToken}} {
		acctExpect(t, request(t, handler, "/api/series/"+firstSeries+"?profile="+test.profile.Id, test.token), http.StatusNotFound, "NOT_FOUND")
	}
	// A series is public catalog-derived data: it resolves for any owned profile, subscribed to its channel or
	// not, and carries its own channel's state.
	for _, profile := range []*core.Record{second, empty} {
		detail := serGet(t, handler, token, firstSeries, profile.Id)
		if detail.Series.ID != firstSeries || detail.Series.ChannelID != firstChannel || len(detail.Series.Items) != 2 || detail.Sync.Status != jobs.Complete || detail.Sync.Loaded != 2 {
			t.Fatalf("unsubscribed detail=%+v", detail)
		}
	}
}

// A platform without catalog synchronization has no series to analyse: subscribing to such a channel must not
// turn the profile's series status into an error.
func TestSeriesOfAPlatformWithoutCatalogSyncAreNeitherPendingNorAnError(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t))
	owner := testUser(t, app, "series-twitch@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	var channelID string
	for _, p := range testPlatforms().All() {
		if p.CatalogSync() == nil {
			channelID = storedChannel(t, app, p.ID(), "streamer", "Streamer")
			break
		}
	}
	if channelID == "" {
		t.Fatal("no platform without catalog synchronization to test with")
	}
	serSubscribe(t, app, profile, channelID)
	answer := serList(t, handler, token, profile.Id, "")
	if answer.Sync.Status != jobs.Complete || len(answer.Analysis.Channels) != 0 || len(answer.Series) != 0 {
		t.Fatalf("a channel on a platform without catalog sync: %+v", answer)
	}
}

// A new subscription wakes the analysis, which synchronizes the channel's catalog with nobody asking.
func TestSeriesCatalogsAreSynchronizedForNewSubscriptions(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, withFixtureDownloader(t))
	owner := testUser(t, app, "series-sync@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	serSubscribe(t, app, profile, storedChannel(t, app, testPlatforms().Default().ID(), "UCfixture", "Fixture channel"))

	waitFor(t, "the catalog of the new subscription", func() bool {
		row, err := app.FindFirstRecordByFilter("channel_catalogs", "platform_id = 'UCfixture'")
		return err == nil && row.GetString("status") == string(jobs.Complete)
	})
	answer := serWaitList(t, handler, token, profile.Id, serComplete)
	if answer.Sync.Loaded != 1 || len(answer.Analysis.Channels) != 1 || answer.Analysis.Channels[0].Loaded != 1 {
		t.Fatalf("sync=%+v analysis=%+v", answer.Sync, answer.Analysis)
	}
}

// The subscriptions the instance already held are analysed when the server starts.
func TestSeriesCatalogsOfExistingSubscriptionsAreSynchronizedAtStartup(t *testing.T) {
	t.Parallel()
	app, _ := serStarted(t, func(app core.App) {
		owner := testUser(t, app, "series-startup@example.test")
		serSubscribe(t, app, profileOf(t, app, owner), storedChannel(t, app, testPlatforms().Default().ID(), "UCfixture", "Fixture channel"))
	}, serQuickAnalysis, withFixtureDownloader(t))
	waitFor(t, "the catalog of an existing subscription", func() bool {
		row, err := app.FindFirstRecordByFilter("channel_catalogs", "platform_id = 'UCfixture'")
		return err == nil && row.GetString("status") == string(jobs.Complete)
	})
}

// Reading is a snapshot: a change of the catalog is not inferred by a read, only by the analysis a new
// subscription (or a page view) wakes, and the series keep their ids.
func TestSeriesAreRecomputedForNewEpisodesWithoutChangingTheirID(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t))
	owner := testUser(t, app, "series-updates@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	native := serChannel("updates")
	channel, _ := serSeedCatalog(t, app, native, "Outer Wilds #1", "Outer Wilds #2")
	serSubscribe(t, app, profile, channel)
	before := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return serComplete(a) && len(a.Series) == 1 && a.Series[0].VideoCount == 2
	})

	newcomer := platform.Video{ID: "new-episode", Platform: testPlatforms().Default().ID(), ChannelID: native, ChannelName: native, Title: "Outer Wilds #3", Published: "2026-02-01"}
	if err := newStore(app).StoreVideo(&newcomer, ""); err != nil {
		t.Fatal(err)
	}
	acctRecord(t, app, "channel_catalog_entries", map[string]any{"catalog": serCatalogRow(t, app, native).Id, "video": newcomer.RecordID, "generation": "fixture"})
	if unchanged := serList(t, handler, token, profile.Id, ""); len(unchanged.Series) != 1 || unchanged.Series[0].VideoCount != 2 {
		t.Fatalf("a read recomputed the series: %+v", unchanged)
	}

	serSubscribe(t, app, acctRecord(t, app, "profiles", map[string]any{"user": owner.Id, "name": "Another"}), channel)
	after := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return serComplete(a) && len(a.Series) == 1 && a.Series[0].VideoCount == 3
	})
	if after.Series[0].ID != before.Series[0].ID || after.Sync.Loaded != 3 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

// An expired catalog keeps its membership but is not claimed fresh: its synchronization is due, and here it
// fails.
func TestSeriesOfAnExpiredCatalogKeepTheirMembershipWithoutClaimingFreshness(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t))
	owner := testUser(t, app, "series-expired@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	native := serChannel("expired")
	channel, _ := serSeedCatalog(t, app, native, "Outer Wilds #1", "Outer Wilds #2")
	row := serCatalogRow(t, app, native)
	row.Set("completed_at", time.Now().Add(-48*time.Hour))
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	serSubscribe(t, app, profile, channel)
	answer := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return len(a.Series) == 1 && a.Sync.Status != jobs.Complete
	})
	if answer.Series[0].VideoCount != 2 || answer.Sync.Loaded != 2 {
		t.Fatalf("an expired catalog lost its membership: %+v", answer)
	}
}

// A series stored with an old, partial membership is repaired by the analysis of the whole catalog, not left
// permanently pending on stale data; its record (and id) stays.
func TestSeriesStoredWithAStaleMembershipAreRepairedByTheAnalysis(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t))
	owner := testUser(t, app, "series-repair@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	channel, videos := serSeedCatalog(t, app, serChannel("repair"), "Outer Wilds #1", "Outer Wilds #2")
	stale := acctRecord(t, app, "series", map[string]any{"channel": channel, "name": "Outer Wilds", "videos": []string{videos[0]}})
	serSubscribe(t, app, profile, channel)

	answer := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return serComplete(a) && len(a.Series) == 1 && a.Series[0].VideoCount == 2
	})
	if answer.Series[0].ID != stale.Id {
		t.Fatalf("the repair replaced the series record %s: %+v", stale.Id, answer.Series[0])
	}
	if repaired, err := app.FindRecordById("series", stale.Id); err != nil || len(repaired.GetStringSlice("videos")) != 2 {
		t.Fatalf("the stored membership was not repaired: %v %v", repaired, err)
	}
}

// A catalog stored as failed (by a process that stopped, say) is not reported as an error and is not left
// alone either: the background synchronizes it again by itself, without any request asking for it.
func TestSeriesOfAStoredFailedCatalogAreRetriedInTheBackgroundNeverReportedAsAnError(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, withFixtureDownloader(t))
	owner := testUser(t, app, "series-retry@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	channel, _ := serSeedCatalog(t, app, "UCfixture", "Outer Wilds #1", "Outer Wilds #2")
	row := serCatalogRow(t, app, "UCfixture")
	row.Set("status", string(jobs.Failed))
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	serSubscribe(t, app, profile, channel)
	// Whatever the poll catches, the stored failure is never an error: the answers are pending until the
	// synchronization the background started is done. The fixture channel lists one video, so the catalog is
	// complete at last and the series that no longer exist are removed.
	done := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		if a.Sync.Status == jobs.Failed || len(a.Analysis.Channels) == 1 && a.Analysis.Channels[0].Status == jobs.Failed {
			t.Fatalf("a stored failure was reported as an error: %+v", a)
		}
		return serComplete(a)
	})
	if done.Sync.Loaded != 1 || len(done.Series) != 0 {
		t.Fatalf("completed retry=%+v", done)
	}
}

// Channel series resolve from the public catalog alone: the Playlists tab of a channel and its detail links
// work for viewers who never subscribed. Viewing the page stores the series; clients read them from the
// collection.
func TestChannelSeriesResolveWithoutSubscription(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t))
	viewer, token := viewerOf(t, handler)
	profile := profileOf(t, app, viewer)
	native := serChannel("unsubscribed")
	channelID, videoIDs := serSeedCatalog(t, app, native, "Outer Wilds #1", "Outer Wilds #2", "Vidéo isolée")
	if count, err := app.CountRecords("subscriptions"); err != nil || count != 0 {
		t.Fatalf("the test needs an account that follows nothing: %d %v", count, err)
	}

	// The page answers its catalog and wakes the analysis, which stores the series in passing.
	res := request(t, handler, "/api/channel/"+channelID+"?sync=1", "")
	acctExpect(t, res, http.StatusOK, "")
	listing := "/api/collections/series/records?filter=" + url.QueryEscape("channel='"+channelID+"'")
	var records struct {
		Items []struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Videos []string `json:"videos"`
		} `json:"items"`
	}
	waitFor(t, "the series of the viewed channel", func() bool {
		res := request(t, handler, listing, token)
		acctExpect(t, res, http.StatusOK, "")
		records.Items = nil
		acctDecode(t, res, &records)
		return len(records.Items) > 0
	})
	if len(records.Items) != 1 || records.Items[0].Name != "Outer Wilds" || len(records.Items[0].Videos) != 2 {
		t.Fatalf("channel series: %+v", records.Items)
	}
	seriesID := records.Items[0].ID
	if stored, err := app.FindRecordById("series", seriesID); err != nil || stored.GetString("channel") != channelID {
		t.Fatalf("series id %q is not the stored record of the channel %q: %v", seriesID, channelID, err)
	}

	waitFor(t, "the analysis of the viewed channel", func() bool {
		return serGet(t, handler, token, seriesID, profile.Id).Sync.Status == jobs.Complete
	})
	detail := serGet(t, handler, token, seriesID, profile.Id)
	if detail.Series.Title != "Outer Wilds" || len(detail.Series.Items) != 2 || detail.Series.Items[0].Episode == nil {
		t.Fatalf("channel series detail: %+v", detail.Series)
	}
	got := map[string]bool{}
	for _, item := range detail.Series.Items {
		got[item.Video.ID] = true
	}
	if !got[videoIDs[0]] || !got[videoIDs[1]] {
		t.Fatalf("channel series detail members = %v, want %v", got, videoIDs[:2])
	}

	// Ids are stable: another page view that infers the same series keeps its record.
	acctExpect(t, request(t, handler, "/api/channel/"+channelID+"?sync=1", ""), http.StatusOK, "")
	res = request(t, handler, listing, token)
	var again struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	acctDecode(t, res, &again)
	if len(again.Items) != 1 || again.Items[0].ID != seriesID {
		t.Fatalf("the series id changed across a page view: %s", res.Body)
	}

	acctExpect(t, request(t, handler, "/api/series/doesnotexist1234?profile="+profile.Id, token), http.StatusNotFound, "NOT_FOUND")
	if res := request(t, handler, "/api/series/"+seriesID+"?profile="+profile.Id, ""); res.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous detail status=%d", res.Code)
	}
}
