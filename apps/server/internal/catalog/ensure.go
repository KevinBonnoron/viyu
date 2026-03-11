package catalog

import "ganigeorgiev.com/pocketbase/pocketbase/core"

// EnsureChannel finds the channel record of a platform identity, or creates its stub: a public record that
// knows nothing but its identity and starts expired, for the profile backfill to complete. It answers nil
// when the reference is not a channel this instance accepts (unknown platform, an identifier the platform
// rejects) or names one userID may not see. Meant for data that comes from a file, so nothing the file says
// about the channel is stored; it works inside a transaction (see In).
func (s *Store) EnsureChannel(userID, platformID, nativeID string) (*core.Record, error) {
	if p, ok := s.platforms.Get(platformID); !ok || !p.ValidChannelID(nativeID) {
		return nil, nil
	}
	return s.ensure(channels, userID, platformID, nativeID)
}

// EnsureVideo finds the video record of a platform identity, or creates its stub: see EnsureChannel. The
// stub starts expired, for the publication backfill to complete.
func (s *Store) EnsureVideo(userID, platformID, nativeID string) (*core.Record, error) {
	if p, ok := s.platforms.Get(platformID); !ok || !p.ValidVideoID(nativeID) {
		return nil, nil
	}
	return s.ensure(videos, userID, platformID, nativeID)
}

func (s *Store) ensure(collection, userID, platformID, nativeID string) (*core.Record, error) {
	var record *core.Record
	var w wanted
	err := s.write(func(tx *Store) error {
		r, written, err := tx.upsert(collection, platformID, nativeID, func(r *core.Record) error {
			if r.IsNew() {
				r.Set("expires_at", expired())
			}
			return nil
		})
		if err != nil {
			return err
		}
		record = r
		w.profiles = written && collection == channels
		w.publications = written && collection == videos
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wake(w)
	if !VisibleTo(record, userID) {
		return nil, nil
	}
	return record, nil
}
