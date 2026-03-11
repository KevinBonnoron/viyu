package migrations

import (
	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"slices"
	"strings"
	"testing"
)

const farFuture = "2030-01-01 00:00:00.000Z"

func freshApp(t *testing.T) core.App {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	if err := initialize(app); err != nil {
		t.Fatal(err)
	}
	return app
}

func collection(t *testing.T, app core.App, name string) *core.Collection {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func saveRecord(t *testing.T, app core.App, name string, values map[string]any) *core.Record {
	t.Helper()
	r := core.NewRecord(collection(t, app, name))
	r.Load(values)
	if name == "users" {
		r.SetPassword("password-123456")
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
	return r
}

func saveFails(t *testing.T, app core.App, name string, values map[string]any, why string) {
	t.Helper()
	r := core.NewRecord(collection(t, app, name))
	r.Load(values)
	if err := app.Save(r); err == nil {
		t.Errorf("%s: %s", name, why)
	}
}

func ruleOf(rule *string) string {
	if rule == nil {
		return "<nil>"
	}
	return *rule
}

// fieldRank places a field in the order docs/adr/0001-pocketbase-field-order.md sets: id, the ordinary
// fields, the relations, created, updated.
func fieldRank(field core.Field) int {
	switch {
	case field.GetName() == "id":
		return 0
	case field.Type() == core.FieldTypeRelation:
		return 2
	case field.GetName() == "created":
		return 3
	case field.GetName() == "updated":
		return 4
	default:
		return 1
	}
}

func TestSchemaFollowsFieldOrder(t *testing.T) {
	app := freshApp(t)
	collections, err := app.FindAllCollections(core.CollectionTypeBase)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, c := range collections {
		if c.System {
			continue
		}
		checked++
		fields := c.Fields
		if len(fields) < 3 || fields[0].GetName() != "id" {
			t.Errorf("%s must start with id: %v", c.Name, fields.FieldNames())
			continue
		}
		for i := 1; i < len(fields); i++ {
			if fieldRank(fields[i]) < fieldRank(fields[i-1]) {
				t.Errorf("%s: %s follows %s, against the field order: %v", c.Name, fields[i].GetName(), fields[i-1].GetName(), fields.FieldNames())
				break
			}
		}
	}
	if checked != 15 {
		t.Fatalf("%d collections checked", checked)
	}
}

func TestFinalSchemaShape(t *testing.T) {
	app := freshApp(t)
	if got := strings.Join(collection(t, app, "watch_history").Fields.FieldNames(), ","); got != "id,watched_at,duration_watched,video_duration,position,watched,profile,video,series,created,updated" {
		t.Errorf("watch_history fields: %s", got)
	}
	if got := strings.Join(collection(t, app, "subscriptions").Fields.FieldNames(), ","); got != "id,ungrouped,synced,profile,channel,creator,created,updated" {
		t.Errorf("subscription fields: %s", got)
	}
	if got := strings.Join(collection(t, app, "creators").Fields.FieldNames(), ","); got != "id,name,profile,created,updated" {
		t.Errorf("creators fields: %s", got)
	}
	if creator, ok := collection(t, app, "subscriptions").Fields.GetByName("creator").(*core.RelationField); !ok || creator.CascadeDelete || creator.Required || creator.MaxSelect != 1 || creator.CollectionId != collection(t, app, "creators").Id {
		t.Errorf("subscriptions.creator must be an optional single relation to creators that does not cascade: %#v", creator)
	}
	if profile, ok := collection(t, app, "creators").Fields.GetByName("profile").(*core.RelationField); !ok || !profile.CascadeDelete || !profile.Required {
		t.Errorf("creators.profile must be a required relation that cascades: %#v", profile)
	}
	videoFields := collection(t, app, "videos").Fields
	for _, name := range []string{"titles", "thumbnails", "creators", "published", "metadata", "category", "live", "subscriber_only", "owners"} {
		if videoFields.GetByName(name) == nil {
			t.Errorf("videos lacks %s", name)
		}
	}
	for _, name := range []string{"title", "original_title", "thumbnail", "published_at", "published_at_time", "original_language", "original_title_verified", "chapters_loaded", "metadata_expires_at", "like_count", "dislike_count", "thumbnail_ocr_url", "task_generation", "result_type"} {
		if videoFields.GetByName(name) != nil {
			t.Errorf("videos retains obsolete field %q", name)
		}
	}
	if collection(t, app, "channels").Fields.GetByName("owners") == nil {
		t.Error("channels lacks owners")
	}
	for _, name := range []string{"profiles", "settings", "source_connections", "source_activations", "companion_devices"} {
		f, ok := collection(t, app, name).Fields.GetByName("user").(*core.RelationField)
		if !ok || !f.CascadeDelete {
			t.Errorf("%s.user must cascade on account deletion", name)
		}
	}
	profile := collection(t, app, "profiles").Fields
	for _, name := range []string{"icon", "theme", "notifications_seen_at"} {
		if profile.GetByName(name) == nil {
			t.Errorf("profiles lacks %s", name)
		}
	}
	if collection(t, app, "settings").Fields.GetByName("hide_subscriber_only") == nil {
		t.Error("settings lacks hide_subscriber_only")
	}
	reaction := collection(t, app, "video_reactions").Fields.GetByName("reaction").(*core.SelectField)
	if !slices.Contains(reaction.Values, "not_interested") {
		t.Errorf("reaction values: %v", reaction.Values)
	}
	for _, name := range []string{"channels", "videos"} {
		owners, ok := collection(t, app, name).Fields.GetByName("owners").(*core.RelationField)
		if !ok || owners.CascadeDelete || owners.MaxSelect <= 1 || owners.CollectionId != collection(t, app, "users").Id {
			t.Errorf("%s.owners must be a multi relation to users that does not cascade: %#v", name, owners)
		}
		description := collection(t, app, name).Fields.GetByName("description").(*core.TextField)
		if description.Max != DescriptionMax {
			t.Errorf("%s.description max = %d, want %d", name, description.Max, DescriptionMax)
		}
	}
	for _, name := range []string{"app_settings", "playlist_ocr_settings"} {
		var count int
		if err := app.DB().NewQuery("SELECT COUNT(*) FROM {{" + name + "}} WHERE id = 1").Row(&count); err != nil || count != 1 {
			t.Errorf("%s singleton: %d %v", name, count, err)
		}
	}
	for _, name := range []string{"likes", "playlist_results"} {
		if _, err := app.FindCollectionByNameOrId(name); err == nil {
			t.Errorf("%s is not part of the schema", name)
		}
	}
}

type ruleCase struct{ name, list, view, create, update, deleteRule string }

func TestRuleStrings(t *testing.T) {
	app := freshApp(t)
	owned := "@request.auth.id = profile.user"
	owners := "@request.auth.id != '' && (owners:length = 0 || owners.id ?= @request.auth.id)"
	for _, tc := range []ruleCase{
		{"watch_history", owned, owned, owned, owned + " && @request.body.profile:changed = false && @request.body.video:changed = false", owned},
		{"subscriptions", owned, owned, "<nil>", "<nil>", owned},
		{"creators", owned, owned, "<nil>", "<nil>", "<nil>"},
		{"video_reactions", owned, owned, "", "", owned},
		{"videos", owners, owners, "<nil>", "<nil>", "<nil>"},
		{"channels", owners, owners, "<nil>", "<nil>", "<nil>"},
		{"series", "@request.auth.id != ''", "@request.auth.id != ''", "<nil>", "<nil>", "<nil>"},
		{"notification_seen", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>"},
		{"source_activations", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>"},
		{"source_connections", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>"},
		{"companion_devices", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>"},
	} {
		c := collection(t, app, tc.name)
		if got := ruleOf(c.ListRule); got != tc.list {
			t.Errorf("%s list rule = %q", tc.name, got)
		}
		if got := ruleOf(c.ViewRule); got != tc.view {
			t.Errorf("%s view rule = %q", tc.name, got)
		}
		if tc.create != "" && ruleOf(c.CreateRule) != tc.create {
			t.Errorf("%s create rule = %q", tc.name, ruleOf(c.CreateRule))
		}
		if tc.update != "" && ruleOf(c.UpdateRule) != tc.update {
			t.Errorf("%s update rule = %q", tc.name, ruleOf(c.UpdateRule))
		}
		if got := ruleOf(c.DeleteRule); got != tc.deleteRule {
			t.Errorf("%s delete rule = %q", tc.name, got)
		}
	}
	if users := collection(t, app, "users"); users.DeleteRule != nil {
		t.Errorf("users delete rule = %q: accounts are deleted only through the admin route", *users.DeleteRule)
	}
}

func TestOwnersRuleHidesPrivateRecordsFromOtherAccounts(t *testing.T) {
	app := freshApp(t)
	owner := saveRecord(t, app, "users", map[string]any{"email": "owner@example.test", "role": "user"})
	other := saveRecord(t, app, "users", map[string]any{"email": "other@example.test", "role": "user"})
	for _, name := range []string{"channels", "videos"} {
		c := collection(t, app, name)
		public := saveRecord(t, app, name, map[string]any{"platform": "youtube", "platform_id": "public", "expires_at": farFuture})
		private := saveRecord(t, app, name, map[string]any{"platform": "youtube", "platform_id": "private", "expires_at": farFuture, "owners": []string{owner.Id}})
		for _, tc := range []struct {
			label   string
			record  *core.Record
			auth    *core.Record
			allowed bool
		}{
			{"public/owner", public, owner, true}, {"public/other", public, other, true}, {"public/anonymous", public, nil, false},
			{"private/owner", private, owner, true}, {"private/other", private, other, false}, {"private/anonymous", private, nil, false},
		} {
			ok, err := app.CanAccessRecord(tc.record, &core.RequestInfo{Auth: tc.auth}, c.ViewRule)
			if err != nil || ok != tc.allowed {
				t.Errorf("%s %s view = %v, %v; want %v", name, tc.label, ok, err, tc.allowed)
			}
		}
	}
}

func TestSubscriptionAccessFollowsTheProfileOwner(t *testing.T) {
	app := freshApp(t)
	owner := saveRecord(t, app, "users", map[string]any{"email": "owner@example.test", "role": "user"})
	other := saveRecord(t, app, "users", map[string]any{"email": "other@example.test", "role": "user"})
	profile := saveRecord(t, app, "profiles", map[string]any{"name": "Owner", "user": owner.Id})
	channel := saveRecord(t, app, "channels", map[string]any{"platform": "youtube", "platform_id": "channel", "expires_at": farFuture})
	sub := saveRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id})
	c := collection(t, app, "subscriptions")
	for _, tc := range []struct {
		auth    *core.Record
		allowed bool
	}{{owner, true}, {other, false}, {nil, false}} {
		ok, err := app.CanAccessRecord(sub, &core.RequestInfo{Auth: tc.auth}, c.ViewRule)
		if err != nil || ok != tc.allowed {
			t.Errorf("subscription access = %v, %v; want %v", ok, err, tc.allowed)
		}
	}
}

func TestUniqueIndexes(t *testing.T) {
	app := freshApp(t)
	user := saveRecord(t, app, "users", map[string]any{"email": "owner@example.test", "role": "user"})
	p1 := saveRecord(t, app, "profiles", map[string]any{"name": "One", "user": user.Id})
	p2 := saveRecord(t, app, "profiles", map[string]any{"name": "Two", "user": user.Id})
	channel := saveRecord(t, app, "channels", map[string]any{"platform": "youtube", "platform_id": "channel", "expires_at": farFuture})
	video := saveRecord(t, app, "videos", map[string]any{"platform": "youtube", "platform_id": "video", "expires_at": farFuture, "channel": channel.Id})

	saveFails(t, app, "channels", map[string]any{"platform": "youtube", "platform_id": "channel", "expires_at": farFuture}, "duplicate channel accepted")
	saveFails(t, app, "videos", map[string]any{"platform": "youtube", "platform_id": "video", "expires_at": farFuture}, "duplicate video accepted")

	saveRecord(t, app, "subscriptions", map[string]any{"profile": p1.Id, "channel": channel.Id})
	saveFails(t, app, "subscriptions", map[string]any{"profile": p1.Id, "channel": channel.Id}, "duplicate subscription accepted")
	saveRecord(t, app, "subscriptions", map[string]any{"profile": p2.Id, "channel": channel.Id})

	history := map[string]any{"profile": p1.Id, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z"}
	saveRecord(t, app, "watch_history", history)
	saveFails(t, app, "watch_history", history, "a profile has one history row per video")
	history["profile"] = p2.Id
	saveRecord(t, app, "watch_history", history)
	saveFails(t, app, "watch_history", map[string]any{"profile": p1.Id, "watched_at": "2026-01-01 00:00:00.000Z"}, "history without a video accepted")

	saveRecord(t, app, "video_reactions", map[string]any{"profile": p1.Id, "video": video.Id, "reaction": "like"})
	saveFails(t, app, "video_reactions", map[string]any{"profile": p1.Id, "video": video.Id, "reaction": "dislike"}, "a profile has one reaction per video")

	saveRecord(t, app, "notification_seen", map[string]any{"profile": p1.Id, "channel": channel.Id, "seen_at": "2026-01-01 00:00:00.000Z"})
	saveFails(t, app, "notification_seen", map[string]any{"profile": p1.Id, "channel": channel.Id, "seen_at": "2026-01-02 00:00:00.000Z"}, "one seen marker per profile and channel")

	saveRecord(t, app, "source_activations", map[string]any{"user": user.Id, "platform": "twitch", "enabled": false})
	saveFails(t, app, "source_activations", map[string]any{"user": user.Id, "platform": "twitch", "enabled": true}, "one choice per account and platform")

	device := map[string]any{"name": "Chrome", "token_hash": "hash", "user": user.Id, "profile": p1.Id}
	saveRecord(t, app, "companion_devices", device)
	saveFails(t, app, "companion_devices", device, "a device token hash must be unique")

	catalog := saveRecord(t, app, "channel_catalogs", map[string]any{"platform": "youtube", "platform_id": "channel", "status": "complete"})
	entry := map[string]any{"catalog": catalog.Id, "video": video.Id, "generation": "generation"}
	saveRecord(t, app, "channel_catalog_entries", entry)
	saveFails(t, app, "channel_catalog_entries", entry, "duplicate catalog video accepted")
}

func TestHistoryIndexesForListingByProfile(t *testing.T) {
	app := freshApp(t)
	for name, want := range map[string][]string{
		"watch_history":   {"idx_watch_history_profile_watched_at ON", "idx_watch_history_profile_watched ON"},
		"subscriptions":   {"idx_subscriptions_channel", "idx_subscriptions_profile_creator ON subscriptions (profile, creator)"},
		"creators":        {"idx_creators_profile"},
		"video_reactions": {"idx_video_reactions_video"},
		"profiles":        {"idx_profiles_user"},
		"videos":          {"idx_videos_channel_published"},
	} {
		c := collection(t, app, name)
		for _, index := range want {
			if !slices.ContainsFunc(c.Indexes, func(definition string) bool { return strings.Contains(definition, index) }) {
				t.Errorf("%s lacks index %s: %v", name, index, c.Indexes)
			}
		}
	}
}

func TestCascades(t *testing.T) {
	app := freshApp(t)
	user := saveRecord(t, app, "users", map[string]any{"email": "owner@example.test", "role": "user"})
	profile := saveRecord(t, app, "profiles", map[string]any{"name": "One", "user": user.Id})
	channel := saveRecord(t, app, "channels", map[string]any{"platform": "youtube", "platform_id": "channel", "expires_at": farFuture})
	video := saveRecord(t, app, "videos", map[string]any{"platform": "youtube", "platform_id": "video", "expires_at": farFuture, "channel": channel.Id, "owners": []string{user.Id}})
	saveRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z"})
	saveRecord(t, app, "video_reactions", map[string]any{"profile": profile.Id, "video": video.Id, "reaction": "like"})
	saveRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": channel.Id})

	// A subscribed channel cannot be garbage collected: the subscription does not cascade.
	if err := app.Delete(channel); err == nil {
		t.Fatal("a channel with a subscription was deleted")
	}
	if rows, _ := app.FindAllRecords("subscriptions"); len(rows) != 1 {
		t.Fatalf("subscriptions = %d", len(rows))
	}

	// Deleting a video removes what points at it.
	if err := app.Delete(video); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"watch_history", "video_reactions"} {
		if rows, err := app.FindAllRecords(name); err != nil || len(rows) != 0 {
			t.Errorf("%s outlived its video: %d %v", name, len(rows), err)
		}
	}

	// A creator that goes does not take its subscriptions: they only lose the link.
	creator := saveRecord(t, app, "creators", map[string]any{"profile": profile.Id, "name": "Creator"})
	linked, err := app.FindFirstRecordByFilter("subscriptions", "profile = {:profile}", map[string]any{"profile": profile.Id})
	if err != nil {
		t.Fatal(err)
	}
	linked.Set("creator", creator.Id)
	if err := app.Save(linked); err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(creator); err != nil {
		t.Fatal(err)
	}
	if linked, err = app.FindRecordById("subscriptions", linked.Id); err != nil || linked.GetString("creator") != "" {
		t.Fatalf("deleting a creator must only clear the link: %v %v", linked, err)
	}
	saveRecord(t, app, "creators", map[string]any{"profile": profile.Id, "name": "Cascading"})

	// Deleting the account removes its profiles, subscriptions, creators and choices, but never the shared records.
	saveRecord(t, app, "source_activations", map[string]any{"user": user.Id, "platform": "twitch", "enabled": true})
	private := saveRecord(t, app, "videos", map[string]any{"platform": "youtube", "platform_id": "private", "expires_at": farFuture, "owners": []string{user.Id}})
	if err := app.Delete(user); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"profiles", "subscriptions", "creators", "source_activations"} {
		if rows, err := app.FindAllRecords(name); err != nil || len(rows) != 0 {
			t.Errorf("%s outlived its account: %d %v", name, len(rows), err)
		}
	}
	if _, err := app.FindRecordById("videos", private.Id); err != nil {
		t.Errorf("deleting an account deleted a cached video: %v", err)
	}
}

func TestRateLimitsAreEnabled(t *testing.T) {
	app := freshApp(t)
	limits := app.Settings().RateLimits
	if !limits.Enabled {
		t.Fatal("rate limits are disabled")
	}
	var labels []string
	for _, rule := range limits.Rules {
		labels = append(labels, rule.Label)
	}
	if strings.Join(labels, ",") != "*:auth,*:create,/api/stream/,/api/" {
		t.Errorf("rate limit rules: %v", labels)
	}
}
