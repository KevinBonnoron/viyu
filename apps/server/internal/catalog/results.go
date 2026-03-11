package catalog

import (
	"errors"
	"fmt"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// StoreResults stores every video and channel of a search page, in place, so each result carries the
// record id that addresses it from then on, and returns the results that were stored, in order. ownerID
// is the account of a connected search and empty for a public one: see applyOwners.
//
// A result that could not be stored is dropped, never returned with its native id: dropped counts them and
// err says why (one error per failed result). A channel card that names no avatar gets the one the stored
// channel has; an unknown channel is registered, which asks for its profile to be filled in.
func (s *Store) StoreResults(results []platform.Result, ownerID string) (kept []platform.Result, dropped int, err error) {
	var searched []*platform.Video
	var cards []*platform.ChannelCard
	for _, result := range results {
		if result.Video != nil {
			searched = append(searched, result.Video)
		}
		if result.Channel != nil {
			cards = append(cards, result.Channel)
		}
	}
	errs := []error{s.storeVideos(searched, ownerID), s.storeCards(cards, ownerID)}
	kept = make([]platform.Result, 0, len(results))
	for _, result := range results {
		if storedResult(result) {
			kept = append(kept, result)
		}
	}
	return kept, len(results) - len(kept), errors.Join(errs...)
}

// storedResult tells whether a result can leave the server: it holds something and everything it holds
// has the record ids that replace its native ones.
func storedResult(r platform.Result) bool {
	if r.Video == nil && r.Channel == nil {
		return false
	}
	if v := r.Video; v != nil && (v.RecordID == "" || (v.ChannelID != "" && v.ChannelRecordID == "")) {
		return false
	}
	return r.Channel == nil || r.Channel.RecordID != ""
}

func (s *Store) storeCards(cards []*platform.ChannelCard, ownerID string) error {
	var errs []error
	var w wanted
	for _, chunk := range chunks(cards) {
		ids := make([]string, len(chunk))
		failures := make([]error, len(chunk))
		var chunkWanted wanted
		err := s.write(func(tx *Store) error {
			for i, card := range chunk {
				var cardWanted wanted
				ids[i], cardWanted, failures[i] = tx.storeCard(card, ownerID)
				chunkWanted.merge(cardWanted)
			}
			return nil
		})
		for i, card := range chunk {
			switch {
			case err != nil:
				card.RecordID = ""
			case failures[i] != nil:
				card.RecordID = ""
				errs = append(errs, fmt.Errorf("channel %s/%s: %w", card.Platform, card.ID, failures[i]))
			default:
				card.RecordID = ids[i]
			}
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		w.merge(chunkWanted)
	}
	s.wake(w)
	return errors.Join(errs...)
}

// storeCard stores a channel card as a profile: it is not a listing, so the channel's expiry is left alone.
func (s *Store) storeCard(card *platform.ChannelCard, ownerID string) (string, wanted, error) {
	if card.Thumbnail == "" && card.ID != "" && card.Platform != "" {
		if known, err := s.find(channels, card.ID, card.Platform); err == nil {
			if thumbnail := platform.ChannelImageURL(known.GetString("thumbnail")); thumbnail != "" {
				card.Thumbnail = thumbnail
			}
		}
	}
	r, written, err := s.upsertChannelProfile(&platform.Channel{ID: card.ID, Platform: card.Platform, Name: card.Name, Thumbnail: card.Thumbnail}, false, ownerID)
	if err != nil {
		return "", wanted{}, err
	}
	return r.Id, wanted{profiles: written && profileMissing(r)}, nil
}
