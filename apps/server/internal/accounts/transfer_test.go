package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// exportChannelID and exportVideoID are identifiers the YouTube platform accepts on import.
const (
	exportChannelID  = "UCexportexportexportexpo"
	exportChannelTwo = "UCtwotwotwotwotwotwotwoo"
	exportVideoID    = "export-vide"
)

// transferFixture gives the profile a subscription, a reaction and a watch entry, and the user settings.
func (e *env) transferFixture(t *testing.T, user, profile *core.Record) {
	t.Helper()
	channel := e.record(t, "channels", map[string]any{"platform": e.platform.ID(), "platform_id": exportChannelID, "name": "Export channel", "expires_at": time.Now().Add(time.Hour)})
	second := e.record(t, "channels", map[string]any{"platform": e.platform.ID(), "platform_id": exportChannelTwo, "name": "Export channel two", "expires_at": time.Now().Add(time.Hour)})
	creator := e.record(t, "creators", map[string]any{"profile": profile.Id, "name": "Exported creator"})
	e.record(t, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id, "creator": creator.Id})
	e.record(t, "subscriptions", map[string]any{"profile": profile.Id, "channel": second.Id, "creator": creator.Id})
	videos, err := e.app.FindCollectionByNameOrId("videos")
	if err != nil {
		t.Fatal(err)
	}
	video := core.NewRecord(videos)
	video.Set("platform", e.platform.ID())
	video.Set("platform_id", exportVideoID)
	catalog.SetTitles(video, "Exported video", "")
	video.Set("channel", channel.Id)
	video.Set("expires_at", time.Now().Add(time.Hour))
	if err := e.app.Save(video); err != nil {
		t.Fatal(err)
	}
	e.record(t, "video_reactions", map[string]any{"profile": profile.Id, "video": video.Id, "reaction": "like"})
	e.record(t, "watch_history", map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-11-01T10:00:00.000Z", "duration_watched": 300, "video_duration": 2820, "position": 250, "watched": true})
	e.record(t, "settings", map[string]any{"user": user.Id, "hide_shorts": true, "use_original_titles": true, "playback_quality": "720", "auto_skip_sponsors": true, "autoplay": false, "hide_watched": true, "hide_subscriber_only": true})
}

func (e *env) channelRef() ExportChannel {
	return ExportChannel{Platform: e.platform.ID(), PlatformID: exportChannelID}
}

func (e *env) videoRef(id string) ExportVideo {
	return ExportVideo{Platform: e.platform.ID(), PlatformID: id}
}

// importData wraps data in the envelope of an export.
func importData(data ExportData) *Export {
	return &Export{Format: ExportFormat, Version: ExportVersion, ExportedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Data: data}
}

func TestExportImportMergesIntoAnotherAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user(t, "export-owner@example.test", "")
	profile := e.profile(t, owner, "Ada")
	e.transferFixture(t, owner, profile)

	export, err := e.svc.Export(owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	if export.Format != ExportFormat || export.Version != ExportVersion {
		t.Fatalf("export envelope: %+v", export)
	}
	data := export.Data
	if len(data.Profiles) != 2 || data.Profiles[0] != "Ada" || data.Profiles[1] != "export-owner@example.test" {
		t.Fatalf("profiles: %+v", data.Profiles)
	}
	if len(data.Subscriptions) != 2 || data.Subscriptions[0].Profile != "Ada" || data.Subscriptions[1].Profile != "Ada" {
		t.Fatalf("subscriptions: %+v", data.Subscriptions)
	}
	for _, sub := range data.Subscriptions {
		if sub.Creator == nil || sub.Creator.Key == "" || sub.Creator.Name != "Exported creator" || sub.Creator.Key != data.Subscriptions[0].Creator.Key {
			t.Fatalf("subscription creators: %+v", data.Subscriptions)
		}
	}
	if len(data.Reactions) != 1 || data.Reactions[0].Reaction != "like" || data.Reactions[0].Video != e.videoRef(exportVideoID) {
		t.Fatalf("reactions: %+v", data.Reactions)
	}
	if len(data.History) != 1 || data.History[0].Position != 250 || data.History[0].VideoDuration != 2820 || !data.History[0].Watched || data.History[0].Video != e.videoRef(exportVideoID) {
		t.Fatalf("history: %+v", data.History)
	}
	if data.Settings == nil || data.Settings.PlaybackQuality != "720" || data.Settings.Autoplay {
		t.Fatalf("settings: %+v", data.Settings)
	}

	// The file survives its trip through JSON, as a download would.
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	file, err := ParseExport(encoded)
	if err != nil {
		t.Fatal(err)
	}

	target, _ := e.user(t, "export-target@example.test", "")
	counts, err := e.svc.Import(target.Id, file)
	if err != nil {
		t.Fatal(err)
	}
	// The default profiles are named after their accounts: the owner's is new to the target.
	for key, want := range map[string]int{"profiles": 2, "subscriptions": 2, "reactions": 1, "history": 1} {
		if counts[key] != want {
			t.Fatalf("counts %s=%v want %d: %v", key, counts[key], want, counts)
		}
	}
	if counts["skipped"] != 0 {
		t.Fatalf("skipped: %v", counts)
	}

	targetProfile, err := e.app.FindFirstRecordByFilter("profiles", "user = {:user} && name = 'Ada'", dbx.Params{"user": target.Id})
	if err != nil {
		t.Fatalf("imported profile: %v", err)
	}
	subscriptions, err := e.app.FindRecordsByFilter("subscriptions", "profile = {:profile}", "", 0, 0, dbx.Params{"profile": targetProfile.Id})
	if err != nil || len(subscriptions) != 2 || subscriptions[0].GetString("creator") == "" || subscriptions[0].GetString("creator") != subscriptions[1].GetString("creator") {
		t.Fatalf("imported subscriptions: %v %v", subscriptions, err)
	}
	if creator, err := e.app.FindRecordById("creators", subscriptions[0].GetString("creator")); err != nil || creator.GetString("name") != "Exported creator" || creator.GetString("profile") != targetProfile.Id {
		t.Fatalf("imported creator: %v %v", creator, err)
	}
	if _, err := e.app.FindFirstRecordByFilter("video_reactions", "profile = {:profile}", dbx.Params{"profile": targetProfile.Id}); err != nil {
		t.Fatalf("imported reaction: %v", err)
	}
	entry, err := e.app.FindFirstRecordByFilter("watch_history", "profile = {:profile}", dbx.Params{"profile": targetProfile.Id})
	if err != nil || entry.GetFloat("position") != 250 || entry.GetFloat("video_duration") != 2820 {
		t.Fatalf("imported progress: %v %v", entry, err)
	}
	settings, err := e.app.FindFirstRecordByFilter("settings", "user = {:user}", dbx.Params{"user": target.Id})
	if err != nil {
		t.Fatalf("imported settings: %v", err)
	}
	if !settings.GetBool("hide_shorts") || !settings.GetBool("hide_subscriber_only") || settings.GetBool("autoplay") || settings.GetString("playback_quality") != "720" {
		t.Fatalf("imported settings values: %+v", settings)
	}

	// A second import merges idempotently: nothing is duplicated.
	again, err := e.svc.Import(target.Id, file)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range again {
		if value != 0 {
			t.Fatalf("re-import duplicated %s: %v", key, again)
		}
	}
}

func TestExportJSONKeepsItsFormat(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user(t, "format@example.test", "")
	profile := e.firstProfile(t, owner.Id)
	profile.Set("icon", "rocket")
	profile.Set("theme", "rose")
	if err := e.app.Save(profile); err != nil {
		t.Fatal(err)
	}
	channel := e.record(t, "channels", map[string]any{"platform": e.platform.ID(), "platform_id": exportChannelID, "expires_at": time.Now().Add(time.Hour)})
	video := e.video(t, exportVideoID)
	e.record(t, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id})
	e.record(t, "video_reactions", map[string]any{"profile": profile.Id, "video": video.Id, "reaction": "dislike"})
	e.record(t, "watch_history", map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-11-01 10:00:00.000Z", "position": 12})
	if err := e.app.RunInTransaction(func(tx core.App) error { return sources.WriteActivation(tx, owner.Id, e.platform.ID(), false) }); err != nil {
		t.Fatal(err)
	}

	export, err := e.svc.Export(owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	export.ExportedAt = time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC)
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	id := e.platform.ID()
	want := `{"format":"viyu-export","version":1,"exportedAt":"2026-11-02T03:04:05Z","data":{` +
		`"profiles":["format@example.test"],` +
		`"subscriptions":[{"profile":"format@example.test","channel":{"platform":"` + id + `","platformId":"` + exportChannelID + `"}}],` +
		`"reactions":[{"profile":"format@example.test","video":{"platform":"` + id + `","platformId":"` + exportVideoID + `"},"reaction":"dislike"}],` +
		`"history":[{"profile":"format@example.test","video":{"platform":"` + id + `","platformId":"` + exportVideoID + `"},"watchedAt":"2026-11-01 10:00:00.000Z","durationWatched":0,"position":12,"watched":false}],` +
		`"profileAppearance":{"format@example.test":{"icon":"rocket","theme":"rose"}},` +
		`"sources":{"` + id + `":false}}}`
	if string(encoded) != want {
		t.Fatalf("export JSON changed:\n got %s\nwant %s", encoded, want)
	}
	if export.Filename() != "viyu-export-2026-11-02.json" {
		t.Fatalf("filename: %s", export.Filename())
	}
}

func TestExportLoadsRecordsInBatches(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user(t, "batches@example.test", "")
	profile := e.firstProfile(t, owner.Id)
	for _, id := range []string{"batch-vid-1", "batch-vid-2", "batch-vid-3", "batch-vid-4"} {
		video := e.video(t, id)
		e.record(t, "video_reactions", map[string]any{"profile": profile.Id, "video": video.Id, "reaction": "like"})
		e.record(t, "watch_history", map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-11-01 10:00:00.000Z"})
	}
	var queries atomic.Int32
	for _, builder := range []dbx.Builder{e.app.ConcurrentDB(), e.app.NonconcurrentDB()} {
		db := builder.(*dbx.DB)
		previous := db.QueryLogFunc
		db.QueryLogFunc = func(_ context.Context, _ time.Duration, query string, _ *sql.Rows, _ error) {
			if strings.Contains(query, "FROM `videos`") {
				queries.Add(1)
			}
		}
		defer func() { db.QueryLogFunc = previous }()
	}
	if _, err := e.svc.Export(owner.Id); err != nil {
		t.Fatal(err)
	}
	if got := queries.Load(); got != 2 {
		t.Fatalf("the export read the videos table %d times for 8 rows, want one batch per section", got)
	}
}

func TestTransferCarriesProfileAppearanceToNewProfilesOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user(t, "appearance-owner@example.test", "")
	ada := e.profile(t, owner, "Ada")
	ada.Set("icon", "rocket")
	ada.Set("theme", "rose")
	if err := e.app.Save(ada); err != nil {
		t.Fatal(err)
	}
	export, err := e.svc.Export(owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := export.Data.ProfileAppearance["Ada"]; got != (ExportAppearance{Icon: "rocket", Theme: "rose"}) {
		t.Fatalf("exported appearance: %+v", export.Data.ProfileAppearance)
	}
	if _, ok := export.Data.ProfileAppearance["appearance-owner@example.test"]; ok {
		t.Fatalf("a profile without appearance must not be listed: %+v", export.Data.ProfileAppearance)
	}

	target, _ := e.user(t, "appearance-target@example.test", "")
	if _, err := e.svc.Import(target.Id, export); err != nil {
		t.Fatal(err)
	}
	imported, err := e.app.FindFirstRecordByFilter("profiles", "user = {:user} && name = 'Ada'", dbx.Params{"user": target.Id})
	if err != nil {
		t.Fatal(err)
	}
	if imported.GetString("icon") != "rocket" || imported.GetString("theme") != "rose" {
		t.Fatalf("imported appearance: icon=%q theme=%q", imported.GetString("icon"), imported.GetString("theme"))
	}

	// An existing profile keeps what the user chose here.
	imported.Set("theme", "sky")
	if err := e.app.Save(imported); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Import(target.Id, export); err != nil {
		t.Fatal(err)
	}
	if kept, err := e.app.FindRecordById("profiles", imported.Id); err != nil || kept.GetString("theme") != "sky" {
		t.Fatalf("re-import overwrote the theme: %v %v", kept, err)
	}

	// Values this instance would reject are skipped; the profile is still created.
	stale, err := ParseExport([]byte(`{"format":"viyu-export","version":1,"exportedAt":"2026-01-01T00:00:00Z","data":{"profiles":["Grace"],"profileAppearance":{"Grace":{"icon":"Not An Icon!","theme":"neon"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Import(target.Id, stale); err != nil {
		t.Fatal(err)
	}
	grace, err := e.app.FindFirstRecordByFilter("profiles", "user = {:user} && name = 'Grace'", dbx.Params{"user": target.Id})
	if err != nil {
		t.Fatal(err)
	}
	if grace.GetString("icon") != "" || grace.GetString("theme") != "" {
		t.Fatalf("invalid appearance was stored: icon=%q theme=%q", grace.GetString("icon"), grace.GetString("theme"))
	}
}

func TestImportCarriesTheSourceChoicesOfKnownPlatformsOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "import-sources@example.test", "")
	_, err := e.svc.Import(user.Id, importData(ExportData{Sources: map[string]bool{e.platform.ID(): false, "vimeo": true}}))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.app.FindRecordsByFilter("source_activations", "user = {:user}", "", 0, 0, dbx.Params{"user": user.Id})
	if err != nil || len(rows) != 1 || rows[0].GetString("platform") != e.platform.ID() || rows[0].GetBool("enabled") {
		t.Fatalf("activations: %v %v", rows, err)
	}
	export, err := e.svc.Export(user.Id)
	if err != nil || export.Data.Sources[e.platform.ID()] {
		t.Fatalf("exported choices: %+v %v", export, err)
	}
}

func TestParseExportRefusesAnythingThatIsNotOne(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"format":"other","version":1}`, `{"format":"viyu-export","version":2}`, `not-json`, ``, `{}`} {
		_, err := ParseExport([]byte(body))
		assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	}
	if _, err := ParseExport([]byte(`{"format":"viyu-export","version":1}`)); err != nil {
		t.Fatalf("an export with no data: %v", err)
	}
	e := newEnv(t)
	user, _ := e.user(t, "wrong-envelope@example.test", "")
	_, err := e.svc.Import(user.Id, &Export{Format: "other", Version: ExportVersion})
	assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	_, err = e.svc.Import(user.Id, nil)
	assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestImportCreatesPublicStubsFromValidatedIdentifiersOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "import-stubs@example.test", "")
	counts, err := e.svc.Import(user.Id, importData(ExportData{
		Profiles: []string{"Imported"},
		Subscriptions: []ExportSub{
			{Profile: "Imported", Channel: e.channelRef()},
			{Profile: "Imported", Channel: ExportChannel{Platform: e.platform.ID(), PlatformID: "UCtoo-short"}},
			{Profile: "Imported", Channel: ExportChannel{Platform: "vimeo", PlatformID: "whatever"}},
		},
		History: []ExportHistory{
			{Profile: "Imported", Video: e.videoRef(exportVideoID), WatchedAt: "2026-02-03 04:05:06.000Z", DurationWatched: 30, Position: -5},
			{Profile: "Imported", Video: e.videoRef("../../etc"), WatchedAt: "2026-02-03 04:05:06.000Z"},
			{Profile: "Imported", Video: e.videoRef("abcdefghijk"), WatchedAt: "not a date"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if counts["subscriptions"] != 1 || counts["history"] != 1 || counts["skipped"] != 4 {
		t.Fatalf("counts: %v", counts)
	}
	channel, err := e.store.FindChannel(e.platform.ID(), exportChannelID)
	if err != nil {
		t.Fatalf("channel stub: %v", err)
	}
	if channel.GetString("name") != "" || channel.GetString("thumbnail") != "" || len(channel.GetStringSlice("owners")) != 0 {
		t.Fatalf("an import wrote into a shared channel record: name=%q thumbnail=%q owners=%v", channel.GetString("name"), channel.GetString("thumbnail"), channel.GetStringSlice("owners"))
	}
	if !channel.GetDateTime("expires_at").Time().Before(time.Now()) {
		t.Fatalf("a stub must start expired: %v", channel.GetDateTime("expires_at"))
	}
	video, err := e.store.FindVideo(e.platform.ID(), exportVideoID)
	if err != nil {
		t.Fatalf("video stub: %v", err)
	}
	if title, _ := catalog.TitlesOf(video); title != "" || len(video.GetStringSlice("owners")) != 0 {
		t.Fatalf("an import wrote into a shared video record: title=%q owners=%v", title, video.GetStringSlice("owners"))
	}
	if count, _ := e.app.CountRecords("videos"); count != 1 {
		t.Fatalf("rejected identifiers created records: %d videos", count)
	}
	entry, err := e.app.FindFirstRecordByFilter("watch_history", "video = {:video}", dbx.Params{"video": video.Id})
	if err != nil || entry.GetFloat("position") != 0 || entry.GetFloat("duration_watched") != 30 {
		t.Fatalf("imported history entry: %v %v", entry, err)
	}
	if e.wakes.profiles == 0 || e.wakes.publications == 0 {
		t.Fatalf("the backfill workers were not woken for the stubs: %+v", e.wakes)
	}
}

func TestImportDoesNotAttachAPrivateRecordOfAnotherAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user(t, "private-owner@example.test", "")
	user, _ := e.user(t, "private-importer@example.test", "")
	private, err := e.store.EnsureVideo("", e.platform.ID(), exportVideoID)
	if err != nil || private == nil {
		t.Fatalf("fixture: %v %v", private, err)
	}
	private.Set("owners", []string{owner.Id})
	if err := e.app.Save(private); err != nil {
		t.Fatal(err)
	}
	counts, err := e.svc.Import(user.Id, importData(ExportData{
		Reactions: []ExportReaction{{Profile: "Imported", Video: e.videoRef(exportVideoID), Reaction: "like"}},
	}))
	if err != nil || counts["reactions"] != 0 || counts["skipped"] != 1 {
		t.Fatalf("a private video of another account was reachable through an import: %v %v", counts, err)
	}
}

func TestImportRefusesOversizedSectionsAndWritesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "import-caps@example.test", "")
	history := make([]ExportHistory, importMaxEntries+1)
	for i := range history {
		history[i] = ExportHistory{Profile: "Imported", Video: e.videoRef(exportVideoID), WatchedAt: "2026-02-03 04:05:06.000Z"}
	}
	_, err := e.svc.Import(user.Id, importData(ExportData{Profiles: []string{"Imported"}, History: history}))
	assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	profiles := make([]string, importMaxProfiles+1)
	for i := range profiles {
		profiles[i] = "P" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	_, err = e.svc.Import(user.Id, importData(ExportData{Profiles: profiles}))
	assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	if count, _ := e.app.CountRecords("profiles", dbx.HashExp{"user": user.Id}); count != 1 {
		t.Fatalf("a refused import created profiles: %d", count)
	}
}

func TestImportRefusesToGrowPastTheProfileLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "import-growth@example.test", "")
	for i := range importMaxProfiles - 1 {
		e.profile(t, user, "Existing "+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	_, err := e.svc.Import(user.Id, importData(ExportData{Profiles: []string{"One more", "And another"}}))
	assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	if count, _ := e.app.CountRecords("profiles", dbx.HashExp{"user": user.Id}); count != importMaxProfiles {
		t.Fatalf("a refused import changed the profiles: %d", count)
	}
}

func TestImportIsAtomic(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "import-atomic@example.test", "")
	_, err := e.svc.Import(user.Id, importData(ExportData{
		Profiles:      []string{"Imported"},
		Settings:      &ExportSettings{HideShorts: true, PlaybackQuality: "720"},
		Sources:       map[string]bool{e.platform.ID(): false},
		Subscriptions: []ExportSub{{Profile: "Imported", Channel: e.channelRef()}},
		// The last entry has no profile name: the whole import is refused, the subscription included.
		Reactions: []ExportReaction{{Profile: "  ", Video: e.videoRef(exportVideoID), Reaction: "like"}},
	}))
	assertAPIError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	for _, collection := range []string{"subscriptions", "channels", "videos", "settings", "source_activations"} {
		if count, _ := e.app.CountRecords(collection); count != 0 {
			t.Fatalf("a failed import left %d %s", count, collection)
		}
	}
	if count, _ := e.app.CountRecords("profiles", dbx.HashExp{"user": user.Id}); count != 1 {
		t.Fatalf("a failed import left profiles: %d", count)
	}
	if e.wakes.profiles != 0 || e.wakes.publications != 0 {
		t.Fatalf("a failed import woke the backfill workers: %+v", e.wakes)
	}
}
