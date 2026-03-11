package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/migrations"
)

const (
	videos   = "videos"
	channels = "channels"
)

func (s *Store) find(collection, nativeID, platformID string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter(collection, "platform_id = {:id} && platform = {:platform}", dbx.Params{"id": nativeID, "platform": platformID})
}

// recordFor returns the record named by the platform identity, or a new one that carries it.
func (s *Store) recordFor(collection, platformID, nativeID string) (*core.Record, error) {
	r, err := s.find(collection, nativeID, platformID)
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	c, err := s.app.FindCollectionByNameOrId(collection)
	if err != nil {
		return nil, err
	}
	r = core.NewRecord(c)
	r.Set("platform_id", nativeID)
	r.Set("platform", platformID)
	return r, nil
}

// upsert finds the record of a platform identity, or starts a new one, lets edit change it, and saves it
// when something changed: every save is a realtime event pushed to all clients, so a record that edit left
// as it was is not written. written tells whether it was.
//
// A new record is inserted under the unique index of its collection. When that insert loses a race the
// record exists by now: it is found again and edited as an update, up to upsertAttempts times. edit may run
// more than once, against the freshest record each time.
func (s *Store) upsert(collection, platformID, nativeID string, edit func(r *core.Record) error) (r *core.Record, written bool, err error) {
	if platformID == "" || nativeID == "" {
		return nil, false, errUnidentified
	}
	var lost error
	for range upsertAttempts {
		r, err = s.recordFor(collection, platformID, nativeID)
		if err != nil {
			return nil, false, err
		}
		isNew := r.IsNew()
		var before map[string]string
		if !isNew {
			before = snapshot(r)
		}
		if err = edit(r); err != nil {
			return nil, false, err
		}
		if !isNew && maps.Equal(before, snapshot(r)) {
			return r, false, nil
		}
		if err = s.app.Save(r); err == nil {
			return r, true, nil
		}
		if !isNew {
			return nil, false, err
		}
		lost = err
		if _, findErr := s.find(collection, nativeID, platformID); findErr != nil {
			return nil, false, err
		}
	}
	return nil, false, lost
}

// snapshot is the comparable content of a record: what it would write, field by field.
func snapshot(r *core.Record) map[string]string {
	fields := r.Collection().Fields
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		switch name := field.GetName(); name {
		case core.FieldNameId, "created", "updated":
		default:
			encoded, _ := json.Marshal(r.Get(name))
			out[name] = string(encoded)
		}
	}
	return out
}

// applyOwners records who the cached record belongs to. ownerID is empty for a public fetch (no connected
// source): the record is public, whoever owned it before. A connected fetch creates the record owned by
// its account, adds the account to a record that is already owned, and leaves a public record public.
func applyOwners(r *core.Record, ownerID string) {
	owners := r.GetStringSlice("owners")
	switch {
	case ownerID == "":
		if len(owners) > 0 {
			r.Set("owners", []string{})
		}
	case r.IsNew():
		r.Set("owners", []string{ownerID})
	case len(owners) > 0 && !slices.Contains(owners, ownerID):
		r.Set("owners", append(owners, ownerID))
	}
}

// VisibleTo tells whether the account may see a stored record: a public record (no owners) is visible to
// everyone, a private one only to its owners. userID is empty for a request with no account.
func VisibleTo(r *core.Record, userID string) bool {
	owners := r.GetStringSlice("owners")
	return len(owners) == 0 || (userID != "" && slices.Contains(owners, userID))
}

// truncateRunes cuts s to at most max characters without splitting a multi-byte character.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// truncateDescription keeps a description within the limit of the schema.
func truncateDescription(s string) string {
	return truncateRunes(s, migrations.DescriptionMax)
}
