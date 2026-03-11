package migrations

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// A field stored with another type than the snapshot's (videos.platform as text, before it became a
// select) is converted and keeps its values; a field the snapshot no longer has is dropped.
func TestSyncMigrationConvertsARetypedFieldAndKeepsItsValues(t *testing.T) {
	app := freshApp(t)
	videos := collection(t, app, "videos")
	videos.Indexes = nil
	videos.Fields.RemoveByName("platform")
	if err := app.Save(videos); err != nil {
		t.Fatal(err)
	}
	videos.Fields.Add(&core.TextField{Name: "platform", Required: true})
	videos.Fields.Add(&core.TextField{Name: "obsolete"})
	if err := app.Save(videos); err != nil {
		t.Fatal(err)
	}
	video := saveRecord(t, app, "videos", map[string]any{"platform": "twitch", "platform_id": "v1", "obsolete": "x", "expires_at": "2026-01-01 00:00:00.000Z"})

	if err := syncSnapshot(app); err != nil {
		t.Fatal(err)
	}

	synced := collection(t, app, "videos")
	if field := synced.Fields.GetByName("platform"); field == nil || field.Type() != core.FieldTypeSelect {
		t.Fatalf("platform was not converted to a select: %v", field)
	}
	if synced.Fields.GetByName("obsolete") != nil || synced.Fields.GetByName("platform"+legacySuffix) != nil {
		t.Fatal("fields the snapshot does not define must be dropped")
	}
	kept, err := app.FindRecordById("videos", video.Id)
	if err != nil || kept.GetString("platform") != "twitch" || kept.GetString("platform_id") != "v1" {
		t.Fatalf("the retyped value was not kept: %v %v", kept, err)
	}
	if len(synced.Indexes) == 0 {
		t.Fatal("the indexes were not recreated")
	}
}

// A database created from an older snapshot (here: series without its `curated` field and without one
// index) is brought up to schema.json by the sync migration, and its records survive it.
func TestSyncMigrationUpgradesAnOlderDatabaseAndKeepsItsRecords(t *testing.T) {
	app := freshApp(t)
	series := collection(t, app, "series")
	if series.Fields.GetByName("curated") == nil {
		t.Fatal("the test expects schema.json to define series.curated")
	}
	series.Fields.RemoveByName("curated")
	indexes := len(series.Indexes)
	if indexes == 0 {
		t.Fatal("the test expects series to have an index")
	}
	series.Indexes = series.Indexes[:indexes-1]
	if err := app.Save(series); err != nil {
		t.Fatal(err)
	}
	channel := saveRecord(t, app, "channels", map[string]any{"platform": "youtube", "platform_id": "UC0000000000000000000001", "name": "Kept", "expires_at": "2026-01-01 00:00:00.000Z"})

	if err := syncSnapshot(app); err != nil {
		t.Fatal(err)
	}

	synced := collection(t, app, "series")
	if synced.Fields.GetByName("curated") == nil {
		t.Fatal("the missing field was not added back")
	}
	if len(synced.Indexes) != indexes {
		t.Fatalf("indexes = %d, want %d", len(synced.Indexes), indexes)
	}
	kept, err := app.FindRecordById("channels", channel.Id)
	if err != nil || kept.GetString("name") != "Kept" {
		t.Fatalf("a record did not survive the sync: %v %v", kept, err)
	}
	if err := syncSnapshot(app); err != nil {
		t.Fatalf("a second sync must be a no-op: %v", err)
	}
}

func TestSyncMigrationNameFollowsTheSnapshot(t *testing.T) {
	name := SyncMigrationName()
	saved := schemaJSON
	t.Cleanup(func() { schemaJSON = saved })
	schemaJSON = append(append([]byte{}, saved...), ' ')
	if SyncMigrationName() == name {
		t.Fatal("a changed schema.json must get a new sync migration")
	}
}
