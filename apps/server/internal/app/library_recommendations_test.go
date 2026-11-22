package app

import (
	"slices"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestRecommendationsKeepTheHistoricalSeriesSuccessorFirstOutsideSeriesMode(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t)
	owner := testUser(t, app, "historical-recommendations@example.test")
	profile := libProfile(t, app, owner, "Viewer")
	ids := make(map[string]string)
	for _, video := range []platform.Video{
		{ID: "historya022", Title: "Z Event 2017 #22 : Trackmania", Published: "2017-09-19T04:00:01Z"},
		{ID: "historya023", Title: "Z Event 2017 #23 : FIFA 17 avec MV - La revanche", Published: "2017-09-19T13:00:03Z"},
		{ID: "historya024", Title: "Z Event 2017 #24 : FIFA 17 avec MV - La belle", Published: "2017-09-19T17:00:03Z"},
		{ID: "historya025", Title: "Z Event 2017 #25 : Chanson", Published: "2017-09-20T04:00:00Z"},
		{ID: "history2024", Title: "An unrelated recent event", Published: "2024-10-01"},
	} {
		video.ChannelID = libChannelID("historical")
		video.ChannelName = "Historical channel"
		ids[video.ID] = libFeedVideo(t, app, video)
	}
	libRecord(t, app, "series", map[string]any{
		"channel": libChannelRecordOf(t, app, ids["historya023"]),
		"name":    "Z Event 2017",
		// Inferred ordering, not this deliberately shuffled stored relation, decides the successor.
		"videos": []string{ids["historya023"], ids["historya025"], ids["historya022"], ids["historya024"]},
	})

	// The route has no series context: the API must preserve the contextual recommendation order itself.
	got := libRecommendations(t, handler, profile.Id, ids["historya023"], tokenOf(t, owner))
	if len(got) != 4 || got[0] != ids["historya024"] {
		t.Fatalf("recommendations = %v, want episode 24 first", got)
	}
	if recent := slices.Index(got, ids["history2024"]); recent != len(got)-1 {
		t.Fatalf("2024 video precedes the 2017 alternatives: %v", got)
	}

	libRecord(t, app, "watch_history", map[string]any{
		"profile": profile.Id, "video": ids["historya024"], "watched_at": "2026-11-01 00:00:00.000Z",
	})
	got = libRecommendations(t, handler, profile.Id, ids["historya023"], tokenOf(t, owner))
	if slices.Contains(got, ids["historya024"]) {
		t.Fatalf("already watched successor was suggested again: %v", got)
	}
}
