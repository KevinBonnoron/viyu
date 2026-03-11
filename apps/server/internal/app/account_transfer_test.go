package app

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// exportChannelID and exportVideoID are identifiers the YouTube platform accepts on import.
const (
	exportChannelID  = "UCexportexportexportexpo"
	exportChannelTwo = "UCtwotwotwotwotwotwotwoo"
	exportVideoID    = "export-vide"
)

func acctRecord(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	schema, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(schema)
	record.Load(values)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// acctTransferFixture gives the profile a subscription, a reaction and a watch entry, and the user settings.
func acctTransferFixture(t *testing.T, app core.App, user, profile *core.Record) {
	t.Helper()
	videoID := storedVideo(t, app, platform.Video{ID: exportVideoID, ChannelID: exportChannelID, ChannelName: "Export channel", Title: "Exported video"})
	channelID := recordIDOf(t, app, "channels", exportChannelID, testPlatforms().Default().ID())
	secondID := storedChannel(t, app, testPlatforms().Default().ID(), exportChannelTwo, "Export channel two")
	creator := acctRecord(t, app, "creators", map[string]any{"profile": profile.Id, "name": "Exported creator"})
	acctRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": channelID, "creator": creator.Id})
	acctRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": secondID, "creator": creator.Id})
	acctRecord(t, app, "video_reactions", map[string]any{"profile": profile.Id, "video": videoID, "reaction": "like"})
	acctRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": videoID, "watched_at": "2026-11-01T10:00:00.000Z", "duration_watched": 300, "video_duration": 2820, "position": 250, "watched": true})
	acctRecord(t, app, "settings", map[string]any{"user": user.Id, "hide_shorts": true, "use_original_titles": true, "playback_quality": "720", "auto_skip_sponsors": true, "autoplay": false, "hide_watched": true, "hide_subscriber_only": true})
}

func acctImportBody(t *testing.T, data map[string]any) string {
	t.Helper()
	return acctJSON(t, map[string]any{"format": accounts.ExportFormat, "version": accounts.ExportVersion, "exportedAt": "2026-01-01T00:00:00Z", "data": data})
}

// What an account carries from one instance to another travels through the export and import routes. The
// merge rules themselves are tested in accounts; this follows the file through HTTP.
func TestAccountExportImportMergesIntoAnotherAccount(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "export-owner@example.test")
	profile := acctRecord(t, app, "profiles", map[string]any{"user": owner.Id, "name": "Ada"})
	acctTransferFixture(t, app, owner, profile)
	ownerToken := tokenOf(t, owner)

	export := request(t, handler, "/api/account/export", ownerToken)
	if export.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", export.Code, export.Body)
	}
	if got := export.Header().Get("Content-Disposition"); !strings.HasPrefix(got, `attachment; filename="viyu-export-`) || !strings.HasSuffix(got, `.json"`) {
		t.Fatalf("export is not offered as a download: %q", got)
	}
	if export.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("an account export is cacheable")
	}
	var payload accounts.Export
	acctDecode(t, export, &payload)
	if payload.Format != accounts.ExportFormat || payload.Version != accounts.ExportVersion {
		t.Fatalf("export envelope: %+v", payload)
	}
	if len(payload.Data.Profiles) != 2 || !slices.Contains(payload.Data.Profiles, "Ada") {
		t.Fatalf("profiles: %+v", payload.Data.Profiles)
	}
	yt := testPlatforms().Default().ID()
	if len(payload.Data.Subscriptions) != 2 {
		t.Fatalf("subscriptions: %+v", payload.Data.Subscriptions)
	}
	for _, sub := range payload.Data.Subscriptions {
		if sub.Creator == nil || sub.Creator.Name != "Exported creator" || sub.Creator.Key != payload.Data.Subscriptions[0].Creator.Key || sub.Channel.Platform != yt {
			t.Fatalf("subscriptions: %+v", payload.Data.Subscriptions)
		}
	}
	if len(payload.Data.Reactions) != 1 || payload.Data.Reactions[0].Reaction != "like" || payload.Data.Reactions[0].Video != (accounts.ExportVideo{Platform: yt, PlatformID: exportVideoID}) {
		t.Fatalf("reactions: %+v", payload.Data.Reactions)
	}
	if len(payload.Data.History) != 1 || payload.Data.History[0].Position != 250 || payload.Data.History[0].VideoDuration != 2820 || !payload.Data.History[0].Watched {
		t.Fatalf("history: %+v", payload.Data.History)
	}
	if payload.Data.Settings == nil || payload.Data.Settings.PlaybackQuality != "720" || payload.Data.Settings.Autoplay {
		t.Fatalf("settings: %+v", payload.Data.Settings)
	}

	target := testUser(t, app, "export-target@example.test")
	targetToken := tokenOf(t, target)
	imported := send(t, handler, http.MethodPost, "/api/account/import", targetToken, export.Body.String())
	if imported.Code != http.StatusOK || imported.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("import status=%d body=%s", imported.Code, imported.Body)
	}
	var counts map[string]int
	acctDecode(t, imported, &counts)
	// Both accounts have the default "Profile 1": only Ada is new to the target.
	for key, want := range map[string]int{"subscriptions": 2, "reactions": 1, "history": 1, "profiles": 1} {
		if counts[key] != want {
			t.Fatalf("counts %s=%d want %d: %v", key, counts[key], want, counts)
		}
	}

	targetProfile, err := app.FindFirstRecordByFilter("profiles", "user = {:user} && name = 'Ada'", dbx.Params{"user": target.Id})
	if err != nil {
		t.Fatalf("imported profile: %v", err)
	}
	subscriptions, err := app.FindRecordsByFilter("subscriptions", "profile = {:profile}", "", 0, 0, dbx.Params{"profile": targetProfile.Id})
	if err != nil || len(subscriptions) != 2 || subscriptions[0].GetString("creator") == "" || subscriptions[0].GetString("creator") != subscriptions[1].GetString("creator") {
		t.Fatalf("imported subscriptions: %v %v", subscriptions, err)
	}
	entry, err := app.FindFirstRecordByFilter("watch_history", "profile = {:profile}", dbx.Params{"profile": targetProfile.Id})
	if err != nil || entry.GetFloat("position") != 250 || entry.GetFloat("video_duration") != 2820 {
		t.Fatalf("imported progress: %v %v", entry, err)
	}
	settings, err := app.FindFirstRecordByFilter("settings", "user = {:user}", dbx.Params{"user": target.Id})
	if err != nil || !settings.GetBool("hide_shorts") || settings.GetBool("autoplay") || settings.GetString("playback_quality") != "720" {
		t.Fatalf("imported settings: %v %v", settings, err)
	}

	// A second import merges idempotently: nothing is duplicated.
	again := send(t, handler, http.MethodPost, "/api/account/import", targetToken, export.Body.String())
	if again.Code != http.StatusOK {
		t.Fatalf("re-import status=%d body=%s", again.Code, again.Body)
	}
	var againCounts map[string]int
	acctDecode(t, again, &againCounts)
	for key, value := range againCounts {
		if value != 0 {
			t.Fatalf("re-import duplicated %s: %v", key, againCounts)
		}
	}
}

func TestAccountImportRefusesWhatIsNotAnExportAndAnonymousRequests(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	user := testUser(t, app, "export-invalid@example.test")
	token := tokenOf(t, user)
	for _, body := range []string{`{"format":"other","version":1}`, `{"format":"viyu-export","version":2}`, `not-json`, ``} {
		acctExpect(t, send(t, handler, http.MethodPost, "/api/account/import", token, body), http.StatusBadRequest, "INVALID_REQUEST")
	}
	tooLarge := `{"format":"viyu-export","version":1,"padding":"` + strings.Repeat("x", accounts.ImportMaxBytes) + `"}`
	acctExpect(t, send(t, handler, http.MethodPost, "/api/account/import", token, tooLarge), http.StatusBadRequest, "INVALID_REQUEST")
	if res := send(t, handler, http.MethodPost, "/api/account/import", "", `{"format":"viyu-export","version":1}`); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized import status=%d", res.Code)
	}
	if res := request(t, handler, "/api/account/export", ""); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized export status=%d", res.Code)
	}
}

// An import that is refused writes nothing at all: not the profiles, not the channels it would have needed.
func TestAccountImportIsRefusedAsAWholeAndWritesNothing(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	user := testUser(t, app, "import-atomic@example.test")
	token := tokenOf(t, user)
	video := map[string]any{"platform": testPlatforms().Default().ID(), "platformId": exportVideoID}

	body := acctImportBody(t, map[string]any{
		"profiles":      []string{"Imported"},
		"subscriptions": []map[string]any{{"profile": "Imported", "channel": map[string]any{"platform": testPlatforms().Default().ID(), "platformId": exportChannelID}}},
		// The last entry has no profile name: the whole import is refused, the subscription included.
		"reactions": []map[string]any{{"profile": "  ", "video": video, "reaction": "like"}},
	})
	acctExpect(t, send(t, handler, http.MethodPost, "/api/account/import", token, body), http.StatusBadRequest, "INVALID_REQUEST")
	for _, collection := range []string{"subscriptions", "channels", "videos"} {
		if count, _ := app.CountRecords(collection); count != 0 {
			t.Fatalf("a failed import left %d %s", count, collection)
		}
	}
	if count, _ := app.CountRecords("profiles", dbx.HashExp{"user": user.Id}); count != 1 {
		t.Fatalf("a failed import left profiles: %d", count)
	}

	history := make([]map[string]any, 5001)
	for i := range history {
		history[i] = map[string]any{"profile": "Imported", "video": video, "watchedAt": "2026-02-03 04:05:06.000Z"}
	}
	acctExpect(t, send(t, handler, http.MethodPost, "/api/account/import", token, acctImportBody(t, map[string]any{"profiles": []string{"Imported"}, "history": history})), http.StatusBadRequest, "INVALID_REQUEST")
	if count, _ := app.CountRecords("profiles", dbx.HashExp{"user": user.Id}); count != 1 {
		t.Fatalf("a refused import created profiles: %d", count)
	}
}

// An import never writes what the file says about a channel or a video into a record every account reads:
// the shared records it needs are public stubs made from validated identifiers.
func TestAccountImportCreatesPublicStubsFromValidatedIdentifiersOnly(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	user := testUser(t, app, "import-stubs@example.test")
	token := tokenOf(t, user)
	yt := testPlatforms().Default().ID()
	body := acctImportBody(t, map[string]any{
		"profiles": []string{"Imported"},
		"subscriptions": []map[string]any{
			{"profile": "Imported", "channel": map[string]any{"platform": yt, "platformId": exportChannelID, "name": "Forged name", "thumbnail": "https://evil.example/x.png"}},
			{"profile": "Imported", "channel": map[string]any{"platform": yt, "platformId": "UCtoo-short"}},
			{"profile": "Imported", "channel": map[string]any{"platform": "vimeo", "platformId": "whatever"}},
		},
		"history": []map[string]any{
			{"profile": "Imported", "video": map[string]any{"platform": yt, "platformId": exportVideoID, "title": "Forged title"}, "watchedAt": "2026-02-03 04:05:06.000Z", "durationWatched": 30, "position": -5},
			{"profile": "Imported", "video": map[string]any{"platform": yt, "platformId": "../../etc"}, "watchedAt": "2026-02-03 04:05:06.000Z"},
			{"profile": "Imported", "video": map[string]any{"platform": yt, "platformId": "abcdefghijk"}, "watchedAt": "not a date"},
		},
	})
	res := send(t, handler, http.MethodPost, "/api/account/import", token, body)
	if res.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", res.Code, res.Body)
	}
	var counts map[string]int
	acctDecode(t, res, &counts)
	if counts["subscriptions"] != 1 || counts["history"] != 1 || counts["skipped"] != 4 {
		t.Fatalf("counts: %v", counts)
	}
	channel, err := newStore(app).FindChannel(yt, exportChannelID)
	if err != nil {
		t.Fatalf("channel stub: %v", err)
	}
	if channel.GetString("name") != "" || channel.GetString("thumbnail") != "" || len(channel.GetStringSlice("owners")) != 0 {
		t.Fatalf("an import wrote into a shared channel record: name=%q thumbnail=%q owners=%v", channel.GetString("name"), channel.GetString("thumbnail"), channel.GetStringSlice("owners"))
	}
	video, err := newStore(app).FindVideo(yt, exportVideoID)
	if err != nil {
		t.Fatalf("video stub: %v", err)
	}
	if title, _ := catalog.TitlesOf(video); title != "" || len(video.GetStringSlice("owners")) != 0 {
		t.Fatalf("an import wrote into a shared video record: title=%q owners=%v", title, video.GetStringSlice("owners"))
	}
	if count, _ := app.CountRecords("videos"); count != 1 {
		t.Fatalf("rejected identifiers created records: %d videos", count)
	}
	entry, err := app.FindFirstRecordByFilter("watch_history", "video = {:video}", dbx.Params{"video": video.Id})
	if err != nil || entry.GetFloat("position") != 0 || entry.GetFloat("duration_watched") != 30 {
		t.Fatalf("imported history entry: %v %v", entry, err)
	}
}
