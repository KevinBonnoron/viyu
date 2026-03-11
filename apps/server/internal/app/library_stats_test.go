package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// libStats is the answer of GET /api/stats.
type libStats struct {
	Period string `json:"period"`
	Totals struct {
		Videos   int     `json:"videos"`
		Seconds  float64 `json:"seconds"`
		Channels int     `json:"channels"`
	} `json:"totals"`
	TopTags []struct {
		Key   string `json:"key"`
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"topTags"`
	TopCategories []struct {
		Key string `json:"key"`
	} `json:"topCategories"`
	TopChannels []struct {
		Platform  string  `json:"platform"`
		ChannelID string  `json:"channelId"`
		RecordID  string  `json:"recordId"`
		Name      string  `json:"name"`
		Seconds   float64 `json:"seconds"`
		Count     int     `json:"count"`
	} `json:"topChannels"`
	ByMonth []struct {
		Month string `json:"month"`
		Count int    `json:"count"`
	} `json:"byMonth"`
	ByPlatform []struct {
		Platform string  `json:"platform"`
		Seconds  float64 `json:"seconds"`
		Count    int     `json:"count"`
	} `json:"byPlatform"`
}

func TestStatsRouteChecksTheProfileAndAnswersEmptyListsForAnEmptyHistory(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "stats-owner@example.test"), testUser(t, app, "stats-other@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)

	for _, test := range []struct {
		name, path, token string
		status            int
		code              string
	}{
		{"missing profile", "/api/stats", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"unknown profile", "/api/stats?profile=missing", token, http.StatusNotFound, "NOT_FOUND"},
		{"another account's profile", "/api/stats?profile=" + profile.Id, tokenOf(t, other), http.StatusNotFound, "NOT_FOUND"},
		{"anonymous", "/api/stats?profile=" + profile.Id, "", http.StatusUnauthorized, ""},
		{"an unknown period", "/api/stats?profile=" + profile.Id + "&period=forever", token, http.StatusBadRequest, "INVALID_REQUEST"},
	} {
		libFail(t, request(t, handler, test.path, test.token), test.status, test.code)
		if t.Failed() {
			t.Fatal(test.name)
		}
	}

	res := request(t, handler, "/api/stats?profile="+profile.Id, token)
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the statistics of an account are cacheable: %v", res.Header())
	}
	raw := libOK[map[string]any](t, res)
	if raw["period"] != "all" {
		t.Fatalf("the default period is all: %v", raw["period"])
	}
	for _, field := range []string{"topTags", "topCategories", "topChannels", "byMonth", "byPlatform"} {
		if list, ok := raw[field].([]any); !ok || len(list) != 0 {
			t.Fatalf("%s must be an empty list, not %v", field, raw[field])
		}
	}
}

func TestStatsRankWhatTheProfileWatchedAndLeaveOutOtherProfilesAndDisabledPlatforms(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "stats-rank@example.test")
	profile, otherProfile := profileOf(t, app, owner), libProfile(t, app, owner, "Other")
	token := tokenOf(t, owner)
	now := time.Now()
	watch := func(profileID, videoID string, seconds float64, at time.Time) {
		libRecord(t, app, "watch_history", map[string]any{"profile": profileID, "video": videoID, "watched_at": at.UTC().Format(platform.StoredLayout), "duration_watched": seconds})
	}
	// Tags are free text: spelling and spacing differ and a tag may repeat inside one video.
	a := storedVideo(t, app, platform.Video{ID: "videoa00001", ChannelID: libChannelID("studio"), ChannelName: "Studio", Tags: []string{"Speedrun", " speed  run ", "speedrun", "Minecraft"}, Category: "Gaming", FullMetadata: true})
	b := storedVideo(t, app, platform.Video{ID: "videob00001", ChannelID: libChannelID("studio"), ChannelName: "Studio", Tags: []string{"SPEEDRUN"}, Category: "gaming", FullMetadata: true})
	tw := storedVideo(t, app, platform.Video{ID: "123456789", Platform: "twitch", ChannelID: "streamer", ChannelName: "Streamer", Category: "Just Chatting", FullMetadata: true})
	old := storedVideo(t, app, platform.Video{ID: "videoold001", ChannelID: libChannelID("studio"), ChannelName: "Studio", Tags: []string{"jazz"}, Category: "Music", FullMetadata: true})
	watch(profile.Id, a, 600, now)
	watch(profile.Id, b, 1800, now)
	watch(profile.Id, tw, 300, now)
	watch(profile.Id, old, 500, now.AddDate(0, 0, -200))
	// What another profile of the account watched is not this profile's.
	watch(otherProfile.Id, a, 9000, now)

	get := func(query string) libStats {
		t.Helper()
		return libOK[libStats](t, request(t, handler, "/api/stats?profile="+profile.Id+query, token))
	}
	studio := recordIDOf(t, app, "channels", libChannelID("studio"), "youtube")
	all := get("")
	if all.Period != "all" || all.Totals.Videos != 4 || all.Totals.Seconds != 3200 || all.Totals.Channels != 2 {
		t.Fatalf("totals: %+v", all)
	}
	if len(all.TopTags) != 4 || all.TopTags[0].Key != "speedrun" || all.TopTags[0].Name != "SPEEDRUN" || all.TopTags[0].Count != 2 {
		t.Fatalf("a tag counts once per video whatever its spelling: %+v", all.TopTags)
	}
	if len(all.TopCategories) != 3 || all.TopCategories[0].Key != "gaming" {
		t.Fatalf("categories merge by key: %+v", all.TopCategories)
	}
	if len(all.ByPlatform) != 2 || all.ByPlatform[0].Platform != "youtube" || all.ByPlatform[0].Count != 3 || all.ByPlatform[0].Seconds != 2900 {
		t.Fatalf("platforms: %+v", all.ByPlatform)
	}
	if len(all.ByMonth) != 2 || all.ByMonth[0].Month >= all.ByMonth[1].Month {
		t.Fatalf("months run from the oldest to the newest: %+v", all.ByMonth)
	}
	if len(all.TopChannels) != 2 || all.TopChannels[0].Name != "Studio" || all.TopChannels[0].RecordID != studio || all.TopChannels[0].Seconds != 2900 || all.TopChannels[1].Platform != "twitch" {
		t.Fatalf("channels: %+v", all.TopChannels)
	}
	// A channel is addressed by its record id on the wire; its native id stays on the server.
	raw := request(t, handler, "/api/stats?profile="+profile.Id, token).Body.String()
	if !strings.Contains(raw, `"channelId":"`+studio+`"`) || strings.Contains(raw, libChannelID("studio")) || strings.Contains(raw, `"streamer"`) {
		t.Fatalf("a native channel id is in the statistics: %s", raw)
	}
	var wire struct {
		TopChannels []map[string]any `json:"topChannels"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil || wire.TopChannels[0]["channelId"] != wire.TopChannels[0]["recordId"] {
		t.Fatalf("channelId is not the record id: %v %v", wire.TopChannels, err)
	}

	if recent := get("&period=30d"); recent.Period != "30d" || recent.Totals.Videos != 3 || recent.Totals.Seconds != 2700 {
		t.Fatalf("30 days: %+v", recent)
	}
	if year := get("&period=365d"); year.Totals.Videos != 4 {
		t.Fatalf("a year: %+v", year.Totals)
	}

	libFail(t, libActivate(t, handler, token, "twitch", false), http.StatusOK, "")
	if off := get(""); off.Totals.Videos != 3 || off.Totals.Seconds != 2900 || len(off.ByPlatform) != 1 || off.TopChannels[0].Platform != "youtube" || len(off.TopChannels) != 1 {
		t.Fatalf("a platform the account turned off must not count: %+v", off)
	}
}
