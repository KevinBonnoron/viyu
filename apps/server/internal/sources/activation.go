package sources

import (
	"database/sql"
	"errors"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// A source has two independent states for an account. Activation says "I use this source": a source that
// is off is never queried on the account's behalf, which keeps searches and refreshes from reaching
// platforms nobody wants. Connection is an optional authenticated session (imported cookies) on top of an
// active source. Turning a source off keeps its connection, unused until it is turned on.
//
// An account with no choice recorded gets the platform's default, so a new platform never changes what
// existing accounts see unless it is enabled by default.

// ActivationsCollection holds the choices accounts made, one row per account and platform.
const ActivationsCollection = "source_activations"

// Activations returns, for every platform, whether the account uses it.
func (s *Service) Activations(userID string) (map[string]bool, error) {
	records, err := s.app.FindRecordsByFilter(ActivationsCollection, "user = {:user}", "", 0, 0, dbx.Params{"user": userID})
	if err != nil {
		return nil, apierr.RequestFailed().WithCause(err)
	}
	result := make(map[string]bool, len(records))
	for _, p := range s.platforms.All() {
		result[p.ID()] = p.DefaultEnabled()
	}
	for _, record := range records {
		if _, known := result[record.GetString("platform")]; known {
			result[record.GetString("platform")] = record.GetBool("enabled")
		}
	}
	return result, nil
}

// RequireEnabled fails with SOURCE_DISABLED when the account turned the platform off.
func (s *Service) RequireEnabled(userID, platformID string) error {
	active, err := s.Activations(userID)
	if err != nil {
		return err
	}
	if enabled, known := active[platformID]; known && !enabled {
		return apierr.Disabled()
	}
	return nil
}

// KeepEnabled drops the channels of platforms the account turned off.
func (s *Service) KeepEnabled(userID string, channels []*core.Record) ([]*core.Record, error) {
	active, err := s.Activations(userID)
	if err != nil {
		return nil, err
	}
	kept := make([]*core.Record, 0, len(channels))
	for _, channel := range channels {
		if enabled, known := active[channel.GetString("platform")]; known && enabled {
			kept = append(kept, channel)
		}
	}
	return kept, nil
}

// SetEnabled records the account's choice for a platform.
func (s *Service) SetEnabled(userID, platformID string, enabled bool) error {
	if _, ok := s.platforms.Get(platformID); !ok {
		return unknownSource()
	}
	if err := s.app.RunInTransaction(func(tx core.App) error { return WriteActivation(tx, userID, platformID, enabled) }); err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	return nil
}

// WriteActivation stores the choice with the given app, which is a transaction when the caller has one.
// It does not check that the platform exists: callers that take the id from a client use SetEnabled.
func WriteActivation(app core.App, userID, platformID string, enabled bool) error {
	record, err := app.FindFirstRecordByFilter(ActivationsCollection, "user = {:user} && platform = {:platform}", dbx.Params{"user": userID, "platform": platformID})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		collection, err := app.FindCollectionByNameOrId(ActivationsCollection)
		if err != nil {
			return err
		}
		record = core.NewRecord(collection)
		record.Set("user", userID)
		record.Set("platform", platformID)
	}
	record.Set("enabled", enabled)
	return app.Save(record)
}

// PlatformInUse reports whether any account still uses the platform, so work done for everyone (as
// opposed to for one account) can stop when nobody wants it. A failed lookup counts as in use.
func (s *Service) PlatformInUse(platformID string) bool {
	p, ok := s.platforms.Get(platformID)
	if !ok {
		return false
	}
	if p.DefaultEnabled() {
		users, err := s.app.CountRecords("users")
		if err != nil {
			return true
		}
		off, err := s.app.CountRecords(ActivationsCollection, dbx.HashExp{"platform": platformID, "enabled": false})
		// A server with no account has nobody whose choice could turn a platform off.
		return err != nil || users == 0 || users > off
	}
	on, err := s.app.CountRecords(ActivationsCollection, dbx.HashExp{"platform": platformID, "enabled": true})
	return err != nil || on > 0
}

// PlatformsInUse lists the ids of the platforms some account uses, in registry order.
func (s *Service) PlatformsInUse() []string {
	var used []string
	for _, p := range s.platforms.All() {
		if s.PlatformInUse(p.ID()) {
			used = append(used, p.ID())
		}
	}
	return used
}
