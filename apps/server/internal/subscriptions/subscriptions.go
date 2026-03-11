// Package subscriptions is what a profile follows: its subscriptions to channels, the creators that link
// one creator's channels on several platforms, the notifications about their new videos, and the
// watch history of the profile. It holds the logic and the storage; the HTTP layer only binds requests.
//
// Concurrency: there is no lock. Every write that reads before it writes runs in one transaction
// (RunInTransaction serializes writers), and the unique index on (profile, channel) backs the duplicates
// rule. Inside a transaction the code only uses the transaction's app, never the service's.
package subscriptions

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

const (
	profilesCollection      = "profiles"
	channelsCollection      = "channels"
	creatorsCollection      = "creators"
	subscriptionsCollection = "subscriptions"
	seenCollection          = "notification_seen"
	historyCollection       = "watch_history"
)

// maxCreatorNameLength is the longest creator name the schema accepts.
const maxCreatorNameLength = 100

// queryBatch bounds how many ids one IN query looks up.
const queryBatch = 500

// Service manages the subscriptions of the profiles. It is safe for concurrent use.
type Service struct {
	app       core.App
	sources   *sources.Service
	platforms *platform.Registry
}

// New returns the service of app. The store is not used yet: subscriptions address channels by record id,
// which the catalog already resolved.
func New(app core.App, _ *catalog.Store, sources *sources.Service, platforms *platform.Registry) *Service {
	return &Service{app: app, sources: sources, platforms: platforms}
}

// internalError turns err into an *apierr.Error: itself when it is one, an internal failure otherwise (err stays
// as the cause, message is what the client may read).
func internalError(message string, err error) error {
	var safe *apierr.Error
	if errors.As(err, &safe) {
		return safe
	}
	return apierr.Internal(message, err)
}

// conflict is a request that clashes with the state of the subscriptions.
func conflict(message string) *apierr.Error {
	return apierr.New(http.StatusConflict, apierr.CodeConflict, message)
}

// OwnedProfile returns the profile record when it belongs to the account. A profile that does not exist and
// one of another account are the same answer: Profile not found.
func (s *Service) OwnedProfile(userID, profileID string) (*core.Record, error) {
	profile, err := s.app.FindRecordById(profilesCollection, profileID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (userID == "" || profile.GetString("user") != userID)) {
		return nil, apierr.NotFound("Profile")
	}
	if err != nil {
		return nil, apierr.Internal("Failed to read the profile", err)
	}
	return profile, nil
}

// Subscribed returns the channel records a profile is subscribed to, in one query.
func (s *Service) Subscribed(profileID string) ([]*core.Record, error) {
	channels, err := s.app.FindAllRecords(channelsCollection, dbx.NewExp(
		"{{"+channelsCollection+"}}.[[id]] IN (SELECT [[channel]] FROM {{"+subscriptionsCollection+"}} WHERE [[profile]] = {:profile})",
		dbx.Params{"profile": profileID}))
	if err != nil {
		return nil, apierr.Internal("Failed to read the subscriptions", err)
	}
	return channels, nil
}

// Create subscribes the profile to a channel, addressed by its record id: every surface that shows a channel
// stored it first, and the platform and name of a client are never trusted. The channel must be visible to
// the account and its platform turned on for it. creatorID, when set, is an existing creator of the same
// profile that has no channel on this platform yet. Subscribing twice returns the existing subscription. The
// same creator on another platform is merged into one creator without asking, so the returned subscription
// may carry a creator the caller did not name.
func (s *Service) Create(userID, profileID, channelRecordID, creatorID string) (*core.Record, error) {
	profile, err := s.OwnedProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	creatorID = strings.TrimSpace(creatorID)
	channel, err := s.app.FindRecordById(channelsCollection, strings.TrimSpace(channelRecordID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.Internal("Failed to subscribe", err)
	}
	if err != nil || !catalog.VisibleTo(channel, userID) {
		return nil, apierr.Invalid("Invalid channel")
	}
	platformID := channel.GetString("platform")
	if p, ok := s.platforms.Get(platformID); !ok || !p.ValidChannelID(channel.GetString("platform_id")) {
		return nil, apierr.Invalid("Invalid channel")
	}
	if err := s.sources.RequireEnabled(userID, platformID); err != nil {
		return nil, err
	}
	var subscription *core.Record
	err = s.app.RunInTransaction(func(tx core.App) error {
		if creatorID != "" {
			members, err := creatorMembers(tx, profile.Id, creatorID)
			if err != nil {
				return err
			}
			if len(members) == 0 {
				return apierr.Invalid("Invalid creator")
			}
			platforms, err := platformsOf(tx, members)
			if err != nil {
				return err
			}
			for _, member := range members {
				if member.GetString("channel") != channel.Id && platforms[member.Id] == platformID {
					return apierr.Invalid("Creator already has a channel on this platform")
				}
			}
		}
		existing, err := tx.FindFirstRecordByFilter(subscriptionsCollection, "profile = {:profile} && channel = {:channel}", dbx.Params{"profile": profile.Id, "channel": channel.Id})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			collection, err := tx.FindCollectionByNameOrId(subscriptionsCollection)
			if err != nil {
				return err
			}
			subscription = core.NewRecord(collection)
			subscription.Load(map[string]any{"profile": profile.Id, "channel": channel.Id, "creator": creatorID})
			return tx.Save(subscription)
		case err != nil:
			return err
		}
		subscription = existing
		if current := existing.GetString("creator"); creatorID != "" && current != creatorID {
			if current != "" {
				return conflict("Subscription already belongs to another creator")
			}
			existing.Set("creator", creatorID)
			existing.Set("ungrouped", false)
			return tx.Save(existing)
		}
		return nil
	})
	if err != nil {
		return nil, internalError("Failed to subscribe", err)
	}
	// Grouping is a convenience: its failure never fails the subscription.
	if grouped, groupErr := s.AutoGroup(profile.Id); groupErr != nil {
		logging.Of(s.app).Warn("Failed to group subscriptions automatically", "error", groupErr)
	} else if grouped > 0 {
		if fresh, findErr := s.app.FindRecordById(subscriptionsCollection, subscription.Id); findErr == nil {
			subscription = fresh
		}
	}
	return subscription, nil
}

// Group links the profile's subscriptions, which must be on distinct platforms, into one creator and returns
// every member of it. A creator that already exists among them is reused (the members of it that were not
// named join too). Subscriptions of two creators, or two channels of one platform, cannot be linked. Grouping
// by hand lifts the "kept apart" mark of the members, which AutoGroup honours.
func (s *Service) Group(userID, profileID string, subscriptionIDs []string) ([]*core.Record, error) {
	profile, err := s.OwnedProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(subscriptionIDs))
	named := make(map[string]bool, len(subscriptionIDs))
	for _, id := range subscriptionIDs {
		if id = strings.TrimSpace(id); id != "" && !named[id] {
			named[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 {
		return nil, apierr.Invalid("At least two subscriptions are required")
	}
	var members []*core.Record
	err = s.app.RunInTransaction(func(tx core.App) error {
		// The profile's own subscriptions, once: a subscription outside it is simply not found.
		all, err := tx.FindRecordsByFilter(subscriptionsCollection, "profile = {:profile}", "created", 0, 0, dbx.Params{"profile": profile.Id})
		if err != nil {
			return err
		}
		byID := make(map[string]*core.Record, len(all))
		byCreator := map[string][]*core.Record{}
		for _, subscription := range all {
			byID[subscription.Id] = subscription
			if creator := subscription.GetString("creator"); creator != "" {
				byCreator[creator] = append(byCreator[creator], subscription)
			}
		}
		selected := make([]*core.Record, 0, len(ids))
		for _, id := range ids {
			subscription := byID[id]
			if subscription == nil {
				return apierr.NotFound("Subscription")
			}
			selected = append(selected, subscription)
		}
		creator := ""
		for _, subscription := range selected {
			current := subscription.GetString("creator")
			if current == "" {
				continue
			}
			if creator != "" && creator != current {
				return conflict("Subscriptions already belong to different creators")
			}
			creator = current
		}
		members = selected
		if creator != "" {
			for _, member := range byCreator[creator] {
				if !named[member.Id] {
					members = append(members, member)
				}
			}
		}
		platforms, err := platformsOf(tx, members)
		if err != nil {
			return err
		}
		if !distinctPlatforms(platforms) {
			return apierr.Invalid("Grouped subscriptions must be on distinct platforms")
		}
		if creator == "" {
			record, err := newCreator(tx, profile.Id, selected[0])
			if err != nil {
				return err
			}
			creator = record.Id
		}
		for _, member := range members {
			if member.GetString("creator") == creator && !member.GetBool("ungrouped") {
				continue
			}
			member.Set("creator", creator)
			member.Set("ungrouped", false)
			if err := tx.Save(member); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, internalError("Failed to group subscriptions", err)
	}
	return members, nil
}

// Ungroup separates a creator and returns its members. Each member leaves the creator, which is deleted, and
// is marked ungrouped: AutoGroup leaves such a subscription alone, so the creator is not merged again the
// next time channels are refreshed.
func (s *Service) Ungroup(userID, profileID, creatorID string) ([]*core.Record, error) {
	creatorID = strings.TrimSpace(creatorID)
	if creatorID == "" {
		return nil, apierr.Invalid("Invalid creator")
	}
	profile, err := s.OwnedProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	var members []*core.Record
	err = s.app.RunInTransaction(func(tx core.App) error {
		var err error
		if members, err = creatorMembers(tx, profile.Id, creatorID); err != nil {
			return err
		}
		if len(members) == 0 {
			return apierr.NotFound("Creator")
		}
		for _, member := range members {
			member.Set("creator", "")
			member.Set("ungrouped", true)
			if err := tx.Save(member); err != nil {
				return err
			}
		}
		return PruneCreators(tx, profile.Id)
	})
	if err != nil {
		return nil, internalError("Failed to separate subscriptions", err)
	}
	return members, nil
}

// creatorMembers returns the profile's subscriptions that belong to the creator, oldest first.
func creatorMembers(app core.App, profileID, creatorID string) ([]*core.Record, error) {
	return app.FindRecordsByFilter(subscriptionsCollection, "profile = {:profile} && creator = {:creator}", "created", 0, 0, dbx.Params{"profile": profileID, "creator": creatorID})
}

// newCreator stores a creator of the profile named after the channel of the subscription.
func newCreator(app core.App, profileID string, subscription *core.Record) (*core.Record, error) {
	name := ""
	if channel, err := app.FindRecordById(channelsCollection, subscription.GetString("channel")); err == nil {
		name = channel.GetString("name")
	}
	return StoreCreator(app, profileID, name)
}

// StoreCreator stores a creator of the profile; the name is cut to what the schema accepts.
func StoreCreator(app core.App, profileID, name string) (*core.Record, error) {
	collection, err := app.FindCollectionByNameOrId(creatorsCollection)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(collection)
	record.Set("profile", profileID)
	record.Set("name", truncateRunes(strings.TrimSpace(name), maxCreatorNameLength))
	if err := app.Save(record); err != nil {
		return nil, err
	}
	return record, nil
}

// truncateRunes cuts s to at most n runes.
func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// PruneCreators deletes the profile's creators that have fewer than two subscriptions: a creator is a link
// between channels, and a link of one is nothing. Run it in the transaction that dropped the membership.
func PruneCreators(app core.App, profileID string) error {
	var ids []string
	err := app.DB().NewQuery(`
		SELECT c.[[id]] FROM {{creators}} c
		WHERE c.[[profile]] = {:profile}
		  AND (SELECT COUNT(*) FROM {{subscriptions}} s WHERE s.[[creator]] = c.[[id]]) < 2`).
		Bind(dbx.Params{"profile": profileID}).Column(&ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		creator, err := app.FindRecordById(creatorsCollection, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := app.Delete(creator); err != nil {
			return err
		}
	}
	return nil
}

// RegisterHooks keeps the creators consistent when a client deletes a subscription through the records API.
func RegisterHooks(app core.App) {
	app.OnRecordDeleteRequest(subscriptionsCollection).BindFunc(func(e *core.RecordRequestEvent) error {
		profileID := e.Record.GetString("profile")
		if err := e.Next(); err != nil {
			return err
		}
		if err := e.App.RunInTransaction(func(tx core.App) error { return PruneCreators(tx, profileID) }); err != nil {
			logging.Of(e.App).Warn("Failed to prune creators", "profile", profileID, "error", err)
		}
		return nil
	})
}

// recordsByID loads records by id in batches. An id that has no record has no entry.
func recordsByID(app core.App, collection string, ids []string) (map[string]*core.Record, error) {
	found := make(map[string]*core.Record, len(ids))
	for chunk := range slices.Chunk(ids, queryBatch) {
		records, err := app.FindRecordsByIds(collection, chunk)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			found[record.Id] = record
		}
	}
	return found, nil
}

// platformsOf maps each subscription to the platform of its channel, with one query per batch.
func platformsOf(app core.App, subscriptions []*core.Record) (map[string]string, error) {
	ids := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		ids = append(ids, subscription.GetString("channel"))
	}
	channels, err := recordsByID(app, channelsCollection, ids)
	if err != nil {
		return nil, err
	}
	platforms := make(map[string]string, len(subscriptions))
	for _, subscription := range subscriptions {
		channel := channels[subscription.GetString("channel")]
		if channel == nil {
			return nil, fmt.Errorf("subscriptions: subscription %s has no channel %s", subscription.Id, subscription.GetString("channel"))
		}
		platforms[subscription.Id] = channel.GetString("platform")
	}
	return platforms, nil
}

// distinctPlatforms reports whether no two subscriptions share a platform.
func distinctPlatforms(platforms map[string]string) bool {
	seen := make(map[string]bool, len(platforms))
	for _, platformID := range platforms {
		if seen[platformID] {
			return false
		}
		seen[platformID] = true
	}
	return true
}
