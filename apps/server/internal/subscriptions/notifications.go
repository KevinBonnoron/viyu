package subscriptions

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// NewVideos says how many videos a subscribed creator has released since the profile last looked.
type NewVideos struct {
	Platform  string `json:"platform"`
	ChannelID string `json:"channelId"`
	// ChannelRecordID is the PocketBase id of the channel record; it replaces ChannelID at the boundary.
	ChannelRecordID string `json:"channelRecordId"`
	Name            string `json:"name"`
	Thumbnail       string `json:"thumbnail"`
	Count           int    `json:"count"`
}

// MarshalJSON addresses the channel by its record id.
func (n NewVideos) MarshalJSON() ([]byte, error) {
	type plain NewVideos
	out := plain(n)
	if out.ChannelRecordID != "" {
		out.ChannelID = out.ChannelRecordID
	}
	return json.Marshal(out)
}

// Notifications is the answer to "what is new for this profile".
type Notifications struct {
	Items []NewVideos `json:"items"`
	// AsOf is the moment the answer was computed; sending it back to MarkSeen marks exactly those videos as seen.
	AsOf string `json:"asOf"`
}

// NewVideos answers what the profile's subscriptions released since it last looked. The first call only
// starts the count, so nothing is "new" before that. Channels of a platform the account turned off are left
// out.
func (s *Service) NewVideos(userID, profileID string) (Notifications, error) {
	profile, err := s.OwnedProfile(userID, profileID)
	if err != nil {
		return Notifications{}, err
	}
	asOf := time.Now().UTC().Truncate(time.Millisecond)
	answer := Notifications{Items: []NewVideos{}, AsOf: asOf.Format(platform.StoredLayout)}
	seen := profile.GetDateTime("notifications_seen_at")
	if seen.IsZero() {
		if err := s.moveMarker(profile.Id, asOf); err != nil {
			return Notifications{}, internalError("Failed to read notifications", err)
		}
		return answer, nil
	}
	active, err := s.sources.Activations(userID)
	if err != nil {
		return Notifications{}, err
	}
	items, err := s.newVideosSince(userID, profile.Id, seen.Time(), active)
	if err != nil {
		return Notifications{}, internalError("Failed to read notifications", err)
	}
	answer.Items = items
	return answer, nil
}

// newVideosSince lists the videos of the profile's subscribed channels that Viyu first stored after seen,
// that were published no earlier than seen's day, and that were published after the profile subscribed to
// the channel (to the minute when the publication time is known, else from that day on). The last two
// conditions keep a freshly subscribed channel's back catalog, which Viyu stores all at once and keeps
// completing with older videos, from counting as new. A creator linked on several platforms is a single
// entry. Live streams are not videos yet. A video another account's connected session cached privately
// (owners without userID) is not counted. Channels of a platform the account turned off are skipped: active
// maps each platform to whether the account uses it, and nil skips nothing.
func (s *Service) newVideosSince(userID, profileID string, seen time.Time, active map[string]bool) ([]NewVideos, error) {
	var rows []struct {
		Channel string `db:"channel"`
		Count   int    `db:"count"`
	}
	// A publication known to the day is stored as midnight; clock is that time of day, so the rule's spelling
	// stays platform's. The day is truncated first: StartOfDayStored keeps the milliseconds of its argument.
	day := platform.StartOfDayStored(seen.UTC().Truncate(24 * time.Hour))
	clock := day[len(platform.DayLayout)+1:]
	err := s.app.DB().NewQuery(`
		SELECT [[v.channel]] AS [[channel]], COUNT(*) AS [[count]]
		FROM {{videos}} v
		JOIN {{subscriptions}} s ON s.[[channel]] = v.[[channel]] AND s.[[profile]] = {:profile}
		LEFT JOIN {{notification_seen}} ns ON ns.[[channel]] = v.[[channel]] AND ns.[[profile]] = {:profile}
		WHERE COALESCE(v.[[live]], 0) = 0
		  AND v.[[created]] > COALESCE(ns.[[seen_at]], {:seen})
		  AND v.[[published]] >= {:day}
		  AND (COALESCE(v.[[owners]], '') IN ('', '[]') OR EXISTS (SELECT 1 FROM json_each(v.[[owners]]) je WHERE je.[[value]] = {:user}))
		  AND (
		    (substr(v.[[published]], 12) <> {:clock} AND v.[[published]] >= s.[[created]])
		    OR (substr(v.[[published]], 12) = {:clock} AND v.[[published]] >= substr(s.[[created]], 1, 10) || ' ' || {:clock})
		  )
		GROUP BY v.[[channel]]`).
		Bind(dbx.Params{"profile": profileID, "user": userID, "seen": seen.UTC().Format(platform.StoredLayout), "day": day, "clock": clock}).
		All(&rows)
	if err != nil || len(rows) == 0 {
		return []NewVideos{}, err
	}
	subscriptions, err := s.app.FindRecordsByFilter(subscriptionsCollection, "profile = {:profile}", "created", 0, 0, dbx.Params{"profile": profileID})
	if err != nil {
		return nil, err
	}
	creators := make(map[string]string, len(subscriptions))
	for _, subscription := range subscriptions {
		if creator := subscription.GetString("creator"); creator != "" {
			creators[subscription.GetString("channel")] = creator
		}
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Channel)
	}
	channels, err := recordsByID(s.app, channelsCollection, ids)
	if err != nil {
		return nil, err
	}
	type entry struct {
		best  NewVideos
		count int
		top   int
	}
	merged := map[string]*entry{}
	order := []string{}
	for _, row := range rows {
		channel := channels[row.Channel]
		if channel == nil {
			continue
		}
		if enabled, known := active[channel.GetString("platform")]; known && !enabled {
			continue
		}
		key := row.Channel
		if creator := creators[row.Channel]; creator != "" {
			key = "creator:" + creator
		}
		item := NewVideos{Platform: channel.GetString("platform"), ChannelID: channel.GetString("platform_id"), ChannelRecordID: channel.Id, Name: channel.GetString("name"), Thumbnail: channel.GetString("thumbnail"), Count: row.Count}
		current := merged[key]
		if current == nil {
			current = &entry{best: item, top: row.Count}
			merged[key] = current
			order = append(order, key)
		} else if row.Count > current.top {
			current.best, current.top = item, row.Count
		}
		current.count += row.Count
	}
	items := make([]NewVideos, 0, len(order))
	for _, key := range order {
		item := merged[key].best
		item.Count = merged[key].count
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

// MarkSeen records that the profile has seen everything up to asOf (a date as Notifications.AsOf gives it).
// The marker only moves forward, so a late or repeated call cannot bring old videos back. Without a channel
// the profile-wide marker moves; with one (a channel record id), only that creator, or all of its channels
// so a creator linked on several platforms goes quiet everywhere at once, is marked, and the
// profile-wide marker stays put.
func (s *Service) MarkSeen(userID, profileID, asOf, channelRecordID string) error {
	profile, err := s.OwnedProfile(userID, profileID)
	if err != nil {
		return err
	}
	at, err := types.ParseDateTime(asOf)
	if err != nil || at.IsZero() || at.Time().After(time.Now().Add(time.Minute)) {
		return apierr.Invalid("Invalid notification marker")
	}
	if channelRecordID == "" {
		if err := s.moveMarker(profile.Id, at.Time()); err != nil {
			return internalError("Failed to save notifications", err)
		}
		return nil
	}
	return s.markChannelSeen(profile.Id, channelRecordID, at.Time())
}

// moveMarker moves the profile-wide marker to at, unless it is already at or after it.
func (s *Service) moveMarker(profileID string, at time.Time) error {
	return s.app.RunInTransaction(func(tx core.App) error {
		profile, err := tx.FindRecordById(profilesCollection, profileID)
		if err != nil {
			return err
		}
		if !at.After(profile.GetDateTime("notifications_seen_at").Time()) {
			return nil
		}
		profile.Set("notifications_seen_at", at.UTC().Format(platform.StoredLayout))
		return tx.Save(profile)
	})
}

// markChannelSeen records at as the moment the profile looked at one creator's releases. All the channels
// of the creator are marked when the subscription belongs to one. The markers are written in one
// transaction.
func (s *Service) markChannelSeen(profileID, channelID string, at time.Time) error {
	err := s.app.RunInTransaction(func(tx core.App) error {
		subscription, err := tx.FindFirstRecordByFilter(subscriptionsCollection, "profile = {:profile} && channel = {:channel}", dbx.Params{"profile": profileID, "channel": channelID})
		if errors.Is(err, sql.ErrNoRows) {
			return apierr.NotFound("Subscription")
		}
		if err != nil {
			return err
		}
		channels := []string{channelID}
		if creator := subscription.GetString("creator"); creator != "" {
			siblings, err := creatorMembers(tx, profileID, creator)
			if err != nil {
				return err
			}
			channels = channels[:0]
			for _, sibling := range siblings {
				channels = append(channels, sibling.GetString("channel"))
			}
		}
		markers, err := tx.FindAllRecords(seenCollection, dbx.HashExp{"profile": profileID}, dbx.In("channel", anys(channels)...))
		if err != nil {
			return err
		}
		known := make(map[string]*core.Record, len(markers))
		for _, marker := range markers {
			known[marker.GetString("channel")] = marker
		}
		collection, err := tx.FindCollectionByNameOrId(seenCollection)
		if err != nil {
			return err
		}
		stamp := at.UTC().Format(platform.StoredLayout)
		for _, id := range channels {
			marker := known[id]
			if marker == nil {
				marker = core.NewRecord(collection)
				marker.Set("profile", profileID)
				marker.Set("channel", id)
			} else if marker.GetDateTime("seen_at").Time().After(at) {
				// The marker only moves forward.
				continue
			}
			marker.Set("seen_at", stamp)
			if err := tx.Save(marker); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return internalError("Failed to save notifications", err)
	}
	return nil
}

func anys(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
