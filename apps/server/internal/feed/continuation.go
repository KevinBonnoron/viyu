package feed

import (
	"database/sql"
	"errors"

	"ganigeorgiev.com/pocketbase/dbx"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// nextSeriesVideo finds the immediate successor in a stored series, without inferring new membership.
// When several series contain the anchor, the first by name and record ID with a successor wins.
// An inaccessible successor is not skipped in favor of a later episode.
func (s *Service) nextSeriesVideo(userID, anchorVideoRecordID string) (*platform.Video, error) {
	if s.seriesStore == nil || anchorVideoRecordID == "" {
		return nil, nil
	}
	anchor, err := s.store.Video(anchorVideoRecordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !catalog.VisibleTo(anchor, userID) {
		return nil, nil
	}
	channelID := anchor.GetString("channel")
	channel, err := s.store.Channel(channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !catalog.VisibleTo(channel, userID) {
		return nil, nil
	}
	active, err := s.accounts.Activations(userID)
	if err != nil {
		return nil, err
	}
	if !active[channel.GetString("platform")] {
		return nil, nil
	}
	records, err := s.app.FindRecordsByFilter("series", "channel = {:channel} && videos.id ?= {:video}", "name,id", 0, 0, dbx.Params{"channel": channelID, "video": anchorVideoRecordID})
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		entry, _, err := s.seriesStore.Get(record.Id)
		if err != nil {
			return nil, err
		}
		for i := range entry.Items {
			if entry.Items[i].Video.RecordID != anchorVideoRecordID || i+1 == len(entry.Items) {
				continue
			}
			next := entry.Items[i+1].Video
			video, err := s.store.Video(next.RecordID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if video.GetString("channel") != channelID || !catalog.VisibleTo(video, userID) || !active[next.Platform] {
				return nil, nil
			}
			return &next, nil
		}
	}
	return nil, nil
}
