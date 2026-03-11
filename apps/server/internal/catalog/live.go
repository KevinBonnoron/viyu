package catalog

import (
	"database/sql"
	"errors"
	"fmt"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// stubReferences lists, per collection, the filter that finds the rows that point at a video; a video
// with one of them cannot be deleted, since deleting a video cascades to all of those.
var stubReferences = []struct{ collection, filter string }{
	{"video_reactions", "video = {:id}"},
	{"watch_history", "video = {:id}"},
	{"series", "videos.id ?= {:id}"},
}

// EndLive forgets the streams of a channel of the platform that has just been found not to be live. A
// video's live flag is only written when its full metadata is fetched, so without this a stream that
// ended would keep its "live" badge in every listing for good.
//
// Every stream video of the channel is turned into an ordinary one: the flag is cleared and the metadata
// expired, so the next opening fetches the final duration and date. The pseudo-video that stands for the
// stream in progress (end.StubVideoID) is deleted instead, since the replay appears under its own id,
// unless something references it: a record someone liked, watched or put in a series is kept, because
// deleting a video cascades to all of those. It then stays flagged, which clients ignore because they
// only show a flagged video while the live lookup confirms it.
func (s *Store) EndLive(platformID string, end platform.LiveEnd) error {
	return s.write(func(tx *Store) error { return tx.endLive(platformID, end) })
}

func (s *Store) endLive(platformID string, end platform.LiveEnd) error {
	var errs []error
	stubRecordID := ""
	if end.StubVideoID != "" {
		stub, err := s.find(videos, end.StubVideoID, platformID)
		switch {
		case err == nil:
			stubRecordID = stub.Id
			if stub.GetBool("live") {
				if err := s.dropStub(stub); err != nil {
					errs = append(errs, err)
				}
			}
		case !errors.Is(err, sql.ErrNoRows):
			errs = append(errs, err)
		}
	}

	var channel *core.Record
	for _, id := range end.ChannelIDs {
		found, err := s.find(channels, id, platformID)
		if err == nil {
			channel = found
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return errors.Join(append(errs, err)...)
		}
	}
	if channel == nil {
		return errors.Join(errs...)
	}
	records, err := s.app.FindRecordsByFilter(videos, "channel = {:channel} && live = true", "", 0, 0, dbx.Params{"channel": channel.Id})
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, record := range records {
		if record.Id == stubRecordID {
			continue
		}
		record.Set("live", false)
		ExpireMetadata(record)
		if err := s.app.Save(record); err != nil {
			errs = append(errs, fmt.Errorf("clear the live flag of video %s: %w", record.Id, err))
		}
	}
	return errors.Join(errs...)
}

// dropStub deletes the pseudo-video of a finished stream unless something references it.
func (s *Store) dropStub(stub *core.Record) error {
	for _, reference := range stubReferences {
		rows, err := s.app.FindRecordsByFilter(reference.collection, reference.filter, "", 1, 0, dbx.Params{"id": stub.Id})
		if err != nil {
			return fmt.Errorf("check the references of the stream video %s: %w", stub.Id, err)
		}
		if len(rows) > 0 {
			return nil
		}
	}
	if err := s.app.Delete(stub); err != nil {
		return fmt.Errorf("delete the stream video %s: %w", stub.Id, err)
	}
	return nil
}
