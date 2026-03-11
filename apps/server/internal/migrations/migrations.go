// Package migrations defines the schema: schema.json is the whole schema. A fresh database is created from
// it by the initial migration; an existing one is brought up to it by a sync migration named after the
// snapshot's hash, so every change of schema.json is applied once to every database, data kept.
package migrations

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

// Collection IDs are stable so relation definitions do not depend on generated IDs.
//
//go:embed schema.json
var schemaJSON []byte

// DescriptionMax is the character limit schema.json sets on videos.description and channels.description;
// descriptions are truncated to it before a record is saved.
const DescriptionMax = 20000

type snapshot struct {
	UsersFields json.RawMessage   `json:"usersFields"`
	Collections []json.RawMessage `json:"collections"`
}

func readSnapshot() (snapshot, error) {
	var schema snapshot
	err := json.Unmarshal(schemaJSON, &schema)
	return schema, err
}

// applyUsers gives the auth collection the snapshot's fields and Viyu's rules.
func applyUsers(app core.App, schema snapshot) error {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(schema.UsersFields, &users.Fields); err != nil {
		return err
	}
	users.CreateRule = new("(@request.body.role:isset = false || @request.body.role = 'user')")
	users.UpdateRule = new("@request.body.role:isset = false && (id = @request.auth.id)")
	// An account is deleted only through /api/admin/users, which keeps the last-admin guard.
	users.DeleteRule = nil
	return app.Save(users)
}

func initialize(app core.App) error {
	schema, err := readSnapshot()
	if err != nil {
		return err
	}
	if err := applyUsers(app, schema); err != nil {
		return err
	}

	for _, definition := range schema.Collections {
		collection := &core.Collection{}
		if err := json.Unmarshal(definition, collection); err != nil {
			return err
		}
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	for _, query := range []string{
		"CREATE TABLE {{app_settings}} ([[id]] INTEGER PRIMARY KEY CHECK ([[id]] = 1), [[registration_enabled]] INTEGER NOT NULL CHECK ([[registration_enabled]] IN (0, 1)))",
		"INSERT INTO {{app_settings}} ([[id]], [[registration_enabled]]) VALUES (1, 1)",
		"CREATE TABLE {{playlist_ocr_settings}} ([[id]] INTEGER PRIMARY KEY CHECK ([[id]] = 1), [[enabled]] INTEGER NOT NULL CHECK ([[enabled]] IN (0, 1)))",
		"INSERT INTO {{playlist_ocr_settings}} ([[id]], [[enabled]]) VALUES (1, 1)",
	} {
		if _, err := app.DB().NewQuery(query).Execute(); err != nil {
			return err
		}
	}
	return configureRateLimits(app)
}

// configureRateLimits turns on PocketBase's built-in rate limiter. The stream rule comes before the
// /api/ prefix because the first matching prefix wins and HLS playback makes many small requests.
func configureRateLimits(app core.App) error {
	settings := app.Settings()
	settings.RateLimits.Enabled = true
	settings.RateLimits.Rules = []core.RateLimitRule{
		{Label: "*:auth", MaxRequests: 10, Duration: 10},
		{Label: "*:create", MaxRequests: 60, Duration: 10},
		{Label: "/api/stream/", MaxRequests: 1500, Duration: 10},
		{Label: "/api/", MaxRequests: 600, Duration: 10},
	}
	return app.Save(settings)
}

// syncSnapshot brings an existing database to schema.json: collections are upserted by their stable id,
// fields and indexes added, changed or dropped to match, rules replaced. Records are kept. A field whose
// type changed (PocketBase refuses that in place) keeps its values: the stored column is renamed aside,
// the new field added, the values copied, then the old column dropped. Only the values of a field the
// snapshot no longer has at all are lost. On a database the initial migration just created, it changes
// nothing.
func syncSnapshot(app core.App) error {
	schema, err := readSnapshot()
	if err != nil {
		return err
	}
	if err := applyUsers(app, schema); err != nil {
		return err
	}
	definitions := make([]map[string]any, 0, len(schema.Collections))
	retyped := map[string][]string{} // collection id -> names of fields whose type changed
	for _, raw := range schema.Collections {
		var definition map[string]any
		if err := json.Unmarshal(raw, &definition); err != nil {
			return err
		}
		id, _ := definition["id"].(string)
		if names := matchFields(app, definition); len(names) > 0 {
			retyped[id] = names
		}
		definitions = append(definitions, definition)
	}
	if len(retyped) == 0 {
		return importExact(app, definitions)
	}
	// Stage: the old columns renamed aside and kept next to the snapshot's fields, so both exist at once.
	// The collection's indexes are left out meanwhile (they may name a renamed column); the final import
	// recreates them.
	staged := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		id, _ := definition["id"].(string)
		names := retyped[id]
		if len(names) == 0 {
			staged = append(staged, definition)
			continue
		}
		existing, err := app.FindCollectionByNameOrId(id)
		if err != nil {
			return err
		}
		for _, name := range names {
			existing.Fields.GetByName(name).SetName(name + legacySuffix)
		}
		existing.Indexes = nil
		if err := app.Save(existing); err != nil {
			return err
		}
		copied := map[string]any{}
		for key, value := range definition {
			copied[key] = value
		}
		fields, _ := definition["fields"].([]any)
		withOld := append([]any{}, fields...)
		for _, name := range names {
			old := existing.Fields.GetByName(name + legacySuffix)
			raw, err := json.Marshal(old)
			if err != nil {
				return err
			}
			var field map[string]any
			if err := json.Unmarshal(raw, &field); err != nil {
				return err
			}
			field["type"] = old.Type()
			field["required"] = false
			withOld = append(withOld, field)
		}
		copied["fields"] = withOld
		copied["indexes"] = []any{}
		staged = append(staged, copied)
	}
	if err := app.ImportCollections(staged, false); err != nil {
		return err
	}
	for id, names := range retyped {
		collection, err := app.FindCollectionByNameOrId(id)
		if err != nil {
			return err
		}
		for _, name := range names {
			query := "UPDATE {{" + collection.Name + "}} SET [[" + name + "]] = [[" + name + legacySuffix + "]]"
			if _, err := app.DB().NewQuery(query).Execute(); err != nil {
				return err
			}
		}
	}
	return importExact(app, definitions)
}

// importExact imports the snapshot, then drops the stored fields it does not define: PocketBase's import
// keeps fields that are missing from a definition, so without this a removed field would stay forever.
func importExact(app core.App, definitions []map[string]any) error {
	if err := app.ImportCollections(definitions, false); err != nil {
		return err
	}
	for _, definition := range definitions {
		id, _ := definition["id"].(string)
		collection, err := app.FindCollectionByNameOrId(id)
		if err != nil {
			return err
		}
		wanted := map[string]bool{}
		fields, _ := definition["fields"].([]any)
		for _, item := range fields {
			if field, ok := item.(map[string]any); ok {
				name, _ := field["name"].(string)
				wanted[name] = true
			}
		}
		changed := false
		for _, field := range append([]core.Field{}, collection.Fields...) {
			if !wanted[field.GetName()] {
				collection.Fields.RemoveById(field.GetId())
				changed = true
			}
		}
		if changed {
			if err := app.Save(collection); err != nil {
				return err
			}
		}
	}
	return nil
}

// legacySuffix names a column set aside while its field changes type.
const legacySuffix = "__previous"

// syncVersion is part of the sync migration's name: bump it when syncSnapshot itself changes, so databases
// already synced to the same schema.json by an older version of it are synced again.
const syncVersion = "2"

// matchFields gives each snapshot field the id of the stored field of the same name and type, since
// PocketBase matches fields by id, and returns the names whose stored field has another type: those get a
// new field (their snapshot id) and their values are moved by syncSnapshot.
func matchFields(app core.App, definition map[string]any) []string {
	id, _ := definition["id"].(string)
	existing, err := app.FindCollectionByNameOrId(id)
	if err != nil {
		return nil
	}
	var retyped []string
	fields, _ := definition["fields"].([]any)
	for _, item := range fields {
		field, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := field["name"].(string)
		stored := existing.Fields.GetByName(name)
		if stored == nil {
			continue
		}
		if kind, _ := field["type"].(string); kind != stored.Type() {
			retyped = append(retyped, name)
			continue
		}
		field["id"] = stored.GetId()
	}
	return retyped
}

// SyncMigrationName names the sync migration after the snapshot, so each schema.json runs once.
func SyncMigrationName() string {
	sum := sha256.Sum256(append([]byte(syncVersion+"\n"), schemaJSON...))
	return "1794000001_sync_" + hex.EncodeToString(sum[:6]) + ".go"
}

func init() {
	m.Register(initialize, nil, "1794000000_initialize.go")
	m.Register(syncSnapshot, nil, SyncMigrationName())
}
