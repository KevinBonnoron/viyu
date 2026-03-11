package catalog

import (
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// expired is the expiry of a channel that was never listed.
func expired() time.Time { return time.Unix(0, 0) }

// profileMissing tells whether a channel record still lacks what the profile backfill fills in.
func profileMissing(r *core.Record) bool {
	return r.GetString("thumbnail") == "" || r.GetFloat("subscriber_count") == 0
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
	var recordID string
	err := s.write(func(tx *Store) error {
		r, written, err := tx.upsertChannelProfile(c, renew, ownerID)
		if err != nil {
			return err
		}
		recordID = r.Id
		w.profiles = written && profileMissing(r)
		return nil
	})
	if err != nil {
		c.RecordID = ""
		return err
	}
	c.RecordID = recordID
	s.wake(w)
	return nil
}

func (s *Store) upsertChannelProfile(c *platform.Channel, renew bool, ownerID string) (*core.Record, bool, error) {
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
