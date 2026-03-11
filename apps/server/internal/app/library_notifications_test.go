package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// libNotifications is the answer of GET /api/notifications.
type libNotifications struct {
	Items []struct {
		Platform        string `json:"platform"`
		ChannelID       string `json:"channelId"`
		ChannelRecordID string `json:"channelRecordId"`
		Name            string `json:"name"`
		Count           int    `json:"count"`
	} `json:"items"`
	AsOf string `json:"asOf"`
}

// libRelease stores a video of the channel, published at the stored date, that the server first stored at
// created: the notifications count what was stored after the profile last looked.
func libRelease(t *testing.T, app core.App, channel *core.Record, id, published string, created time.Time, live bool) {
	t.Helper()
	record := libRecord(t, app, "videos", map[string]any{"platform": channel.GetString("platform"), "platform_id": id, "channel": channel.Id, "published": published, "expires_at": "2030-01-01 00:00:00.000Z", "live": live})
	if _, err := app.DB().NewQuery("UPDATE videos SET created = {:created} WHERE id = {:id}").Bind(dbx.Params{"created": created.UTC().Format(platform.StoredLayout), "id": record.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
}

func libNews(t *testing.T, handler http.Handler, profileID, token string) libNotifications {
	t.Helper()
	res := request(t, handler, "/api/notifications?profile="+profileID, token)
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the notifications of an account are cacheable: %v", res.Header())
	}
	return libOK[libNotifications](t, res)
}

// libLookedAt moves the marker of the profile: the notifications count what was released since.
func libLookedAt(t *testing.T, app core.App, profileID string, at time.Time) {
	t.Helper()
	profile, err := app.FindRecordById("profiles", profileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Set("notifications_seen_at", at.UTC().Format(platform.StoredLayout))
	if err := app.Save(profile); err != nil {
		t.Fatal(err)
	}
}

func libMarker(t *testing.T, app core.App, profileID string) string {
	t.Helper()
	profile, err := app.FindRecordById("profiles", profileID)
	if err != nil {
		t.Fatal(err)
	}
	return profile.GetString("notifications_seen_at")
}

func TestNotificationsCountWhatEachCreatorReleasedSinceTheLastLook(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "news@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)

	// Nothing is new before the first look: the request only starts the count.
	if first := libNews(t, handler, profile.Id, token); len(first.Items) != 0 || first.AsOf == "" {
		t.Fatalf("first request: %+v", first)
	}
	if libMarker(t, app, profile.Id) == "" {
		t.Fatal("the first request must set the marker")
	}
	marker := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	libLookedAt(t, app, profile.Id, marker)

	youtube := libSubscribe(t, app, profile, "youtube", libChannelID("news"), "News Creator")
	twitch := libSubscribe(t, app, profile, "twitch", "newscreator", "News Creator")
	solo := libSubscribe(t, app, profile, "youtube", libChannelID("solo"), "Solo")
	libLink(t, app, profile, "One creator", youtube, twitch)
	// Released after the subscription (an exact time), stored after the marker.
	released := time.Now().UTC().Add(time.Minute).Format(platform.StoredLayout)
	stored := time.Now().UTC()
	libRelease(t, app, libChannelOf(t, app, youtube), "y1", released, stored, false)
	libRelease(t, app, libChannelOf(t, app, youtube), "y2", released, stored, false)
	libRelease(t, app, libChannelOf(t, app, twitch), "t1", released, stored, false)
	libRelease(t, app, libChannelOf(t, app, solo), "s1", released, stored, false)
	// Not new: a live stream, a back catalog stored just now, and a video stored before the marker.
	libRelease(t, app, libChannelOf(t, app, solo), "s-live", released, stored, true)
	libRelease(t, app, libChannelOf(t, app, solo), "s-old-release", marker.AddDate(0, 0, -30).Format(platform.StoredLayout), stored, false)
	libRelease(t, app, libChannelOf(t, app, solo), "s-before", released, marker.Add(-time.Minute), false)

	result := libNews(t, handler, profile.Id, token)
	if len(result.Items) != 2 {
		t.Fatalf("a creator linked on two platforms is one entry: %+v", result.Items)
	}
	merged, soloItem := result.Items[0], result.Items[1]
	youtubeChannel, soloChannel := libChannelOf(t, app, youtube), libChannelOf(t, app, solo)
	// The entry is shown with the channel that released the most, addressed by its record id.
	if merged.Name != "News Creator" || merged.Count != 3 || merged.Platform != "youtube" || merged.ChannelID != youtubeChannel.Id || merged.ChannelRecordID != youtubeChannel.Id {
		t.Fatalf("linked creator: %+v", merged)
	}
	if soloItem.Name != "Solo" || soloItem.Count != 1 || soloItem.ChannelID != soloChannel.Id {
		t.Fatalf("solo creator must count only its genuinely new video: %+v", soloItem)
	}

	// A platform the account turned off does not notify.
	libFail(t, libActivate(t, handler, token, "twitch", false), http.StatusOK, "")
	if without := libNews(t, handler, profile.Id, token); len(without.Items) != 2 || without.Items[0].Count != 2 {
		t.Fatalf("a disabled platform must not notify: %+v", without.Items)
	}
	libFail(t, libActivate(t, handler, token, "twitch", true), http.StatusOK, "")

	// Marking one creator seen keeps the others and the profile-wide marker.
	before := libMarker(t, app, profile.Id)
	libFail(t, libDo(t, handler, http.MethodPost, "/api/notifications/seen", token, map[string]string{"profile": profile.Id, "asOf": result.AsOf, "channel": soloChannel.Id}), http.StatusOK, "")
	if remaining := libNews(t, handler, profile.Id, token); len(remaining.Items) != 1 || remaining.Items[0].Name != "News Creator" {
		t.Fatalf("the solo creator went quiet, the other stays: %+v", remaining.Items)
	}
	if libMarker(t, app, profile.Id) != before {
		t.Fatalf("a per-creator mark moved the profile marker: %q -> %q", before, libMarker(t, app, profile.Id))
	}
	// Marking one half of a grouped creator marks the whole group.
	twitchChannel := libChannelOf(t, app, twitch)
	libFail(t, libDo(t, handler, http.MethodPost, "/api/notifications/seen", token, map[string]string{"profile": profile.Id, "asOf": result.AsOf, "channel": twitchChannel.Id}), http.StatusOK, "")
	if len(libNews(t, handler, profile.Id, token).Items) != 0 {
		t.Fatal("the grouped creator did not go quiet on every platform")
	}

	// Marking everything seen up to asOf clears the list, and a later release shows up again.
	libLookedAt(t, app, profile.Id, marker)
	res := libDo(t, handler, http.MethodPost, "/api/notifications/seen", token, map[string]string{"profile": profile.Id, "asOf": result.AsOf})
	if got := libOK[map[string]bool](t, res); !got["ok"] {
		t.Fatalf("seen answer: %s", res.Body)
	}
	if libMarker(t, app, profile.Id) == libFormat(marker) {
		t.Fatal("marking everything seen did not move the profile marker")
	}
	libRelease(t, app, soloChannel, "s2", time.Now().UTC().Add(time.Minute).Format(platform.StoredLayout), time.Now().UTC().Add(time.Second), false)
	if later := libNews(t, handler, profile.Id, token); len(later.Items) != 1 || later.Items[0].Count != 1 || later.Items[0].Name != "Solo" {
		t.Fatalf("a new release: %+v", later.Items)
	}
}

func libFormat(at time.Time) string { return at.UTC().Format(platform.StoredLayout) }

func TestNotificationsMarkerOnlyMovesForwardAndStaysWithItsOwner(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, stranger := testUser(t, app, "news-owner@example.test"), testUser(t, app, "news-stranger@example.test")
	profile := profileOf(t, app, owner)
	ownerToken, strangerToken := tokenOf(t, owner), tokenOf(t, stranger)
	first := libNews(t, handler, profile.Id, ownerToken)
	before := libMarker(t, app, profile.Id)
	seen := func(token string, body map[string]string) *httptest.ResponseRecorder {
		return libDo(t, handler, http.MethodPost, "/api/notifications/seen", token, body)
	}

	if res := seen(ownerToken, map[string]string{"profile": profile.Id, "asOf": "2001-01-01 00:00:00.000Z"}); res.Code != http.StatusOK || libMarker(t, app, profile.Id) != before {
		t.Fatalf("the marker moved backwards: %d %s -> %s (first %s)", res.Code, before, libMarker(t, app, profile.Id), first.AsOf)
	}
	for name, test := range map[string]struct {
		token  string
		body   map[string]string
		status int
		code   string
	}{
		"another account":      {strangerToken, map[string]string{"profile": profile.Id, "asOf": first.AsOf}, http.StatusNotFound, "NOT_FOUND"},
		"an invalid date":      {ownerToken, map[string]string{"profile": profile.Id, "asOf": "yesterday"}, http.StatusBadRequest, "INVALID_REQUEST"},
		"a date in the future": {ownerToken, map[string]string{"profile": profile.Id, "asOf": "2999-01-01 00:00:00.000Z"}, http.StatusBadRequest, "INVALID_REQUEST"},
		"an unknown profile":   {ownerToken, map[string]string{"profile": "missing", "asOf": first.AsOf}, http.StatusNotFound, "NOT_FOUND"},
		"an unknown channel":   {ownerToken, map[string]string{"profile": profile.Id, "asOf": first.AsOf, "channel": "neverseen00000"}, http.StatusNotFound, "NOT_FOUND"},
		"anonymous":            {"", map[string]string{"profile": profile.Id, "asOf": first.AsOf}, http.StatusUnauthorized, ""},
	} {
		libFail(t, seen(test.token, test.body), test.status, test.code)
		if t.Failed() {
			t.Fatal(name)
		}
	}
	if libMarker(t, app, profile.Id) != before {
		t.Fatalf("a refused request moved the marker: %s -> %s", before, libMarker(t, app, profile.Id))
	}
	libFail(t, request(t, handler, "/api/notifications?profile="+profile.Id, strangerToken), http.StatusNotFound, "NOT_FOUND")
	libFail(t, request(t, handler, "/api/notifications?profile="+profile.Id, ""), http.StatusUnauthorized, "")
	// A request that names no profile is a bad request, not a missing profile.
	libFail(t, request(t, handler, "/api/notifications", ownerToken), http.StatusBadRequest, "INVALID_REQUEST")
}

func TestClearingTheHistoryDeletesOnlyTheOwnedProfilesAndAnswersNoContent(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "history-owner@example.test"), testUser(t, app, "history-other@example.test")
	profile, sibling := profileOf(t, app, owner), libProfile(t, app, owner, "Sibling")
	foreign := profileOf(t, app, other)
	token := tokenOf(t, owner)
	clear := func(path, token string) *httptest.ResponseRecorder {
		return libDo(t, handler, http.MethodDelete, path, token, nil)
	}

	for _, test := range []struct {
		name, path, token string
		status            int
		code              string
	}{
		{"missing", "/api/history", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"invalid profile", "/api/history?profile=not-a-profile", token, http.StatusNotFound, "NOT_FOUND"},
		{"foreign profile", "/api/history?profile=" + foreign.Id, token, http.StatusNotFound, "NOT_FOUND"},
		{"anonymous", "/api/history?profile=" + profile.Id, "", http.StatusUnauthorized, ""},
	} {
		libFail(t, clear(test.path, test.token), test.status, test.code)
		if t.Failed() {
			t.Fatal(test.name)
		}
	}
	for _, entry := range []struct{ profile, video string }{{profile.Id, "firstvideo01"}, {profile.Id, "secondvideo1"}, {sibling.Id, "siblingvideo"}, {foreign.Id, "foreignvideo"}} {
		libRecord(t, app, "watch_history", map[string]any{"profile": entry.profile, "video": storedVideo(t, app, platform.Video{ID: entry.video}), "watched_at": "2026-01-01 00:00:00.000Z", "duration_watched": 60})
	}
	count := func(profileID string) int {
		records, err := app.FindRecordsByFilter("watch_history", "profile = {:profile}", "", 0, 0, dbx.Params{"profile": profileID})
		if err != nil {
			t.Fatal(err)
		}
		return len(records)
	}
	if res := clear("/api/history?profile="+profile.Id, token); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("clear status=%d body=%s", res.Code, res.Body)
	}
	if got := count(profile.Id); got != 0 {
		t.Fatalf("the cleared profile still has %d entries", got)
	}
	if count(sibling.Id) != 1 || count(foreign.Id) != 1 {
		t.Fatalf("another profile lost its history: sibling %d, foreign %d", count(sibling.Id), count(foreign.Id))
	}
	// Clearing again is a no-op, not an error.
	if res := clear("/api/history?profile="+profile.Id, token); res.Code != http.StatusNoContent {
		t.Fatalf("second clear status=%d", res.Code)
	}
}
