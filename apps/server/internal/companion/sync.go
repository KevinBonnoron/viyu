package companion

import (
	"database/sql"
	"errors"
	"slices"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
)

const (
	// platformID is the only platform a companion syncs: the extension reads a YouTube account.
	platformID = "youtube"

	// MaxChannels and MaxHistoryEntries bound what one sync request carries: a sync is written in one
	// transaction and must stay bounded.
	MaxChannels       = 5000
	MaxHistoryEntries = 500

	// rewatchGap is how much later than the stored watch an incoming one must be to count as watching the
	// video again: the extension reports the last watch of YouTube, which an earlier sync may already have
	// stored, and the watch the user did in Viyu itself must not be overwritten by the same viewing.
	rewatchGap = 24 * time.Hour
	// futureSlack is how far ahead of the server clock a watch may be dated (clocks and time zones drift).
	futureSlack = 24 * time.Hour

	queryBatch = 500
)

// SubscriptionsResult counts what a subscriptions sync did.
type SubscriptionsResult struct {
	Created   int `json:"created"`
	Removed   int `json:"removed"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
}

// HistoryEntry is one watch the extension read. The position, the duration and whether the video was watched to
// the end are optional: YouTube does not always tell.
type HistoryEntry struct {
	VideoID   string   `json:"videoId"`
	WatchedAt string   `json:"watchedAt"`
	Position  *float64 `json:"position,omitempty"`
	Duration  *float64 `json:"duration,omitempty"`
	Watched   bool     `json:"watched,omitempty"`
}

// HistoryResult counts what a history sync did.
type HistoryResult struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
}

// SyncSubscriptions subscribes the device's profile to the YouTube channels of the list. Nothing the extension
// says about a channel but its id is stored: the channel is a public stub that the background backfill
// completes. A channel the profile already follows is left as it is (it stays the user's own when the user
// subscribed by hand). A complete snapshot also drops the subscriptions an earlier sync created for channels
// that are no longer in the list; a subscription the user made is never dropped, and a snapshot with no
// usable channel drops nothing, since an empty list is more likely a failed read than an unsubscribed
// account.
func (s *Service) SyncSubscriptions(device *Device, channels []string, complete bool) (*SubscriptionsResult, error) {
	if len(channels) > MaxChannels {
		return nil, apierr.Invalid("Too many channels in one sync.")
	}
	if err := s.sources.RequireEnabled(device.userID, platformID); err != nil {
		return nil, err
	}
	result := &SubscriptionsResult{}
	err := s.app.RunInTransaction(func(tx core.App) error {
		*result = SubscriptionsResult{}
		return s.syncSubscriptions(tx, device, channels, complete, result)
	})
	if err != nil {
		return nil, internalError("Failed to sync the subscriptions", err)
	}
	if result.Created > 0 || result.Removed > 0 {
		// Grouping is a convenience: a failure is logged, and the sync still succeeded.
		if _, err := s.subscriptions.AutoGroup(device.Profile); err != nil {
			logging.Of(s.app).Warn("Failed to group the synced subscriptions", "profile", device.Profile, "error", err)
		}
	}
	return result, nil
}

func (s *Service) syncSubscriptions(tx core.App, device *Device, channels []string, complete bool, result *SubscriptionsResult) error {
	store := s.store.In(tx)
	existing, err := tx.FindRecordsByFilter("subscriptions", "profile = {:profile}", "", 0, 0, dbx.Params{"profile": device.Profile})
	if err != nil {
		return err
	}
	subscribed := make(map[string]bool, len(existing))
	for _, subscription := range existing {
		subscribed[subscription.GetString("channel")] = true
	}
	collection, err := tx.FindCollectionByNameOrId("subscriptions")
	if err != nil {
		return err
	}

	listed := make(map[string]bool, len(channels))
	seen := make(map[string]bool, len(channels))
	for _, id := range channels {
		if seen[id] {
			continue
		}
		seen[id] = true
		channel, err := store.EnsureChannel(device.userID, platformID, id)
		if err != nil {
			return err
		}
		if channel == nil {
			result.Skipped++
			continue
		}
		listed[channel.Id] = true
		if subscribed[channel.Id] {
			result.Unchanged++
			continue
		}
		record := core.NewRecord(collection)
		record.Set("profile", device.Profile)
		record.Set("channel", channel.Id)
		record.Set("synced", true)
		if err := tx.Save(record); err != nil {
			return err
		}
		subscribed[channel.Id] = true
		result.Created++
	}

	if complete && len(listed) > 0 {
		removed, err := dropUnlisted(tx, existing, listed)
		if err != nil {
			return err
		}
		if removed > 0 {
			// A creator is a link between channels: one that lost its other channel is not kept.
			if err := subscriptions.PruneCreators(tx, device.Profile); err != nil {
				return err
			}
		}
		result.Removed = removed
	}
	return s.touch(tx, device.ID)
}

// dropUnlisted deletes the subscriptions a sync created (synced) to YouTube channels that are not listed.
func dropUnlisted(tx core.App, existing []*core.Record, listed map[string]bool) (int, error) {
	var candidates []*core.Record
	var channelIDs []string
	for _, subscription := range existing {
		if subscription.GetBool("synced") && !listed[subscription.GetString("channel")] {
			candidates = append(candidates, subscription)
			channelIDs = append(channelIDs, subscription.GetString("channel"))
		}
	}
	platforms := make(map[string]string, len(channelIDs))
	for chunk := range slices.Chunk(channelIDs, queryBatch) {
		channels, err := tx.FindRecordsByIds("channels", chunk)
		if err != nil {
			return 0, err
		}
		for _, channel := range channels {
			platforms[channel.Id] = channel.GetString("platform")
		}
	}
	removed := 0
	for _, subscription := range candidates {
		if platforms[subscription.GetString("channel")] != platformID {
			continue
		}
		if err := tx.Delete(subscription); err != nil {
			return 0, err
		}
		removed++
	}
	return removed, nil
}

// SyncHistory merges the watches of the list into the watch history of the device's profile, one entry per
// video (the first of a repeated video wins). A watch of a video the profile never watched is added. For a
// video it has, the history keeps what the user did in Viyu: a watch more than a day later than the stored
// one is another viewing on YouTube and replaces the date and the progress; otherwise only a "watched" the
// profile lacked is taken. An entry with no usable date or an invalid video id is skipped.
func (s *Service) SyncHistory(device *Device, entries []HistoryEntry) (*HistoryResult, error) {
	if len(entries) > MaxHistoryEntries {
		return nil, apierr.Invalid("Too many history entries in one sync.")
	}
	if err := s.sources.RequireEnabled(device.userID, platformID); err != nil {
		return nil, err
	}
	result := &HistoryResult{}
	err := s.app.RunInTransaction(func(tx core.App) error {
		*result = HistoryResult{}
		return s.syncHistory(tx, device, entries, result)
	})
	if err != nil {
		return nil, internalError("Failed to sync the history", err)
	}
	return result, nil
}

func (s *Service) syncHistory(tx core.App, device *Device, entries []HistoryEntry, result *HistoryResult) error {
	store := s.store.In(tx)
	collection, err := tx.FindCollectionByNameOrId("watch_history")
	if err != nil {
		return err
	}
	latest := s.now().Add(futureSlack)
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry.VideoID] {
			continue
		}
		seen[entry.VideoID] = true
		parsed, err := time.Parse(time.RFC3339, entry.WatchedAt)
		if err != nil || parsed.IsZero() || parsed.After(latest) {
			result.Skipped++
			continue
		}
		watchedAt, err := types.ParseDateTime(parsed)
		if err != nil {
			result.Skipped++
			continue
		}
		video, err := store.EnsureVideo(device.userID, platformID, entry.VideoID)
		if err != nil {
			return err
		}
		if video == nil {
			result.Skipped++
			continue
		}

		record, err := tx.FindFirstRecordByFilter("watch_history", "profile = {:profile} && video = {:video}", dbx.Params{"profile": device.Profile, "video": video.Id})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			record = core.NewRecord(collection)
			record.Set("profile", device.Profile)
			record.Set("video", video.Id)
			record.Set("watched_at", watchedAt)
			record.Set("duration_watched", 0)
			if entry.Duration != nil && *entry.Duration > 0 {
				record.Set("video_duration", *entry.Duration)
			}
			position := 0.0
			if entry.Position != nil {
				position = *entry.Position
			}
			record.Set("position", clampPosition(position, record.GetFloat("video_duration")))
			record.Set("watched", entry.Watched)
			if err := tx.Save(record); err != nil {
				return err
			}
			result.Created++
			continue
		}

		switch stored := record.GetDateTime("watched_at").Time(); {
		case parsed.After(stored.Add(rewatchGap)):
			record.Set("watched_at", watchedAt)
			if entry.Duration != nil && *entry.Duration > 0 {
				record.Set("video_duration", *entry.Duration)
			}
			if entry.Position != nil {
				record.Set("position", clampPosition(*entry.Position, record.GetFloat("video_duration")))
			}
			record.Set("watched", record.GetBool("watched") || entry.Watched)
		case entry.Watched && !record.GetBool("watched"):
			record.Set("watched", true)
		default:
			result.Unchanged++
			continue
		}
		if err := tx.Save(record); err != nil {
			return err
		}
		result.Updated++
	}
	return s.touch(tx, device.ID)
}

// clampPosition keeps a position within the video: never negative, and never past the duration when known.
func clampPosition(position, duration float64) float64 {
	position = max(position, 0)
	if duration > 0 {
		position = min(position, duration)
	}
	return position
}
