package catalog

import (
	"math"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// expired is the expiry of a channel that was never listed.
func expired() time.Time { return time.Unix(0, 0) }

// profileSyncedField records when a real profile of the channel was last stored: the channel metadata
// (creation date, country, totals) is optional, so only this tells a channel that never had a profile
// stored from one whose provider omits those fields.
const profileSyncedField = "profile_synced_at"

// ProfileSynced tells whether a real profile of the channel was ever stored. A channel whose profile
// predates the channel metadata is not synced: it gets one lookup, after which absent metadata settles.
func ProfileSynced(r *core.Record) bool {
	return !r.GetDateTime(profileSyncedField).IsZero()
}

// profileMissing tells whether a channel record still lacks what the profile backfill fills in.
func profileMissing(r *core.Record) bool {
	return r.GetString("thumbnail") == "" || r.GetFloat("subscriber_count") == 0 || !ProfileSynced(r)
}

// totals is the content of the totals field of a channels record: the totals the provider reported. A nil
// total was never reported, which a stored zero is not.
type totals struct {
	VideoCount *float64 `json:"video_count"`
	ViewCount  *float64 `json:"view_count"`
}

func totalsOf(r *core.Record) totals {
	var t totals
	_ = r.UnmarshalJSONField("totals", &t)
	return t
}

// reported tells whether a provider total can be stored: a number, never negative.
func reported(n *float64) bool {
	return n != nil && !math.IsNaN(*n) && !math.IsInf(*n, 0) && *n >= 0
}

// providerDate returns s when it is a date as the contract of platform.Channel.CreatedAt allows
// (YYYY-MM-DD or RFC3339), and "" otherwise: a date nobody can read is no date.
func providerDate(s string) string {
	s = strings.TrimSpace(s)
	if _, err := time.Parse(time.DateOnly, s); err == nil {
		return s
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s
	}
	return ""
}

// applyProfileDetails stores the channel metadata a profile reports. What the profile omits keeps its
// stored value: a partial profile never erases metadata.
func applyProfileDetails(r *core.Record, c *platform.Channel) {
	if created := providerDate(c.CreatedAt); created != "" {
		r.Set("channel_created_at", created)
	}
	if country := strings.TrimSpace(c.Country); country != "" {
		r.Set("country", country)
	}
	stored := totalsOf(r)
	merged := stored
	if reported(c.VideoCount) {
		merged.VideoCount = c.VideoCount
	}
	if reported(c.ViewCount) {
		merged.ViewCount = c.ViewCount
	}
	if !sameTotal(stored.VideoCount, merged.VideoCount) || !sameTotal(stored.ViewCount, merged.ViewCount) {
		r.Set("totals", merged)
	}
}

func sameTotal(a, b *float64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// fillProfileDetails gives c the stored channel metadata its profile omitted, so the answer carries what
// the record knows.
func fillProfileDetails(c *platform.Channel, r *core.Record) {
	c.CreatedAt = r.GetString("channel_created_at")
	c.Country = r.GetString("country")
	t := totalsOf(r)
	c.VideoCount, c.ViewCount = t.VideoCount, t.ViewCount
}

// UpsertChannel stores a channel a video or a search result names: its name and avatar, when it has them.
// A channel created here starts expired (a video establishes who owns it, not a fresh listing) and an
// existing one keeps its expiry. ownerID is empty for a public fetch: see applyOwners. It returns the record.
func (s *Store) UpsertChannel(platformID, nativeID, name, thumbnail, ownerID string) (*core.Record, error) {
	var record *core.Record
	var w wanted
	err := s.write(func(tx *Store) error {
		r, written, err := tx.upsertChannel(platformID, nativeID, name, thumbnail, ownerID)
		if err != nil {
			return err
		}
		record = r
		w.profiles = written && profileMissing(r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wake(w)
	return record, nil
}

func (s *Store) upsertChannel(platformID, nativeID, name, thumbnail, ownerID string) (*core.Record, bool, error) {
	return s.upsert(channels, platformID, nativeID, func(r *core.Record) error {
		if r.IsNew() {
			// A video establishes owner identity, not a fresh channel listing.
			r.Set("expires_at", expired())
		}
		applyOwners(r, ownerID)
		if name != "" {
			r.Set("name", name)
		}
		if platform.ChannelImage(thumbnail) {
			r.Set("thumbnail", platform.ChannelImageURL(thumbnail))
		} else if existing := r.GetString("thumbnail"); existing != "" && !platform.ChannelImage(existing) {
			r.Set("thumbnail", "")
		}
		return nil
	})
}

// StoreChannelListing stores a channel profile and reports the record id back through c. The caller has
// cached a listing of the channel, so its expiry is renewed (see renewMargin): StoreChannelProfile is for
// a caller that has not. ownerID is empty for a public fetch: see applyOwners.
func (s *Store) StoreChannelListing(c *platform.Channel, ownerID string) error {
	return s.storeChannel(c, true, ownerID)
}

// StoreChannelProfile stores a channel profile without renewing the channel's expiry. A profile alone is
// not a listing: the subscription refresh decides from that expiry whether the videos of a subscribed
// channel need fetching, so renewing it here would hide the channel from the refresh. A channel record
// created here starts expired, like one created by UpsertChannel.
func (s *Store) StoreChannelProfile(c *platform.Channel, ownerID string) error {
	return s.storeChannel(c, false, ownerID)
}

func (s *Store) storeChannel(c *platform.Channel, renew bool, ownerID string) error {
	var w wanted
	var stored *core.Record
	err := s.write(func(tx *Store) error {
		r, written, err := tx.upsertChannelProfile(c, renew, true, ownerID)
		if err != nil {
			return err
		}
		stored = r
		w.profiles = written && profileMissing(r)
		return nil
	})
	if err != nil {
		c.RecordID = ""
		return err
	}
	c.RecordID = stored.Id
	fillProfileDetails(c, stored)
	s.wake(w)
	return nil
}

// upsertChannelProfile stores what c says of its channel. profile is true when c comes from a lookup of the
// channel itself, which marks the profile as synced (see ProfileSynced); a stub a search result or a video
// names is not a profile.
func (s *Store) upsertChannelProfile(c *platform.Channel, renew, profile bool, ownerID string) (*core.Record, bool, error) {
	return s.upsert(channels, c.Platform, c.ID, func(r *core.Record) error {
		applyOwners(r, ownerID)
		if thumbnail := platform.ChannelImageURL(c.Thumbnail); thumbnail != "" {
			r.Set("thumbnail", thumbnail)
		}
		if c.Name != "" {
			r.Set("name", c.Name)
		}
		if description := truncateDescription(c.Description); description != "" {
			r.Set("description", description)
		}
		if c.SubscriberCount != 0 {
			r.Set("subscriber_count", c.SubscriberCount)
		}
		applyProfileDetails(r, c)
		if profile && !ProfileSynced(r) {
			r.Set(profileSyncedField, time.Now())
		}
		switch {
		case renew:
			if expiry, ok := renewal(r.GetDateTime("expires_at").Time(), time.Now()); ok {
				r.Set("expires_at", expiry)
			}
		case r.IsNew():
			r.Set("expires_at", expired())
		}
		return nil
	})
}
