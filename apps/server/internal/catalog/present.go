package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// queryBatch bounds how many ids one IN query looks up.
const queryBatch = 200

// Video returns the videos record with the id.
func (s *Store) Video(recordID string) (*core.Record, error) {
	return s.app.FindRecordById(videos, recordID)
}

// Channel returns the channels record with the id.
func (s *Store) Channel(recordID string) (*core.Record, error) {
	return s.app.FindRecordById(channels, recordID)
}

// FindVideo returns the videos record of a platform-native video id; sql.ErrNoRows when there is none.
func (s *Store) FindVideo(platformID, nativeID string) (*core.Record, error) {
	return s.find(videos, nativeID, platformID)
}

// FindChannel returns the channels record of a platform-native channel id, whichever of the forms the
// platform stores it under (see platform.Platform.ChannelIDAliases); sql.ErrNoRows when there is none.
func (s *Store) FindChannel(platformID, nativeID string) (*core.Record, error) {
	ids := []string{nativeID}
	if p, ok := s.platforms.Get(platformID); ok {
		if aliases := p.ChannelIDAliases(nativeID); len(aliases) > 0 {
			ids = aliases
			if !slices.Contains(ids, nativeID) {
				ids = append(slices.Clone(ids), nativeID)
			}
		}
	}
	for _, id := range ids {
		r, err := s.find(channels, id, platformID)
		if err == nil {
			return r, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return nil, sql.ErrNoRows
}

// VideoIdentity resolves a videos record id to the platform-native identity the providers use.
func (s *Store) VideoIdentity(recordID string) (platformID, nativeID string, err error) {
	record, err := s.Video(recordID)
	if err != nil {
		return "", "", err
	}
	return record.GetString("platform"), record.GetString("platform_id"), nil
}

// ChannelIdentity resolves a channels record id to the platform-native identity the providers use.
func (s *Store) ChannelIdentity(recordID string) (platformID, nativeID string, err error) {
	record, err := s.Channel(recordID)
	if err != nil {
		return "", "", err
	}
	return record.GetString("platform"), record.GetString("platform_id"), nil
}

// VideoRecords returns the records of the videos of a platform, by native id. A video that is not stored
// has no entry.
func (s *Store) VideoRecords(platformID string, nativeIDs []string) (map[string]*core.Record, error) {
	results := make(map[string]*core.Record, len(nativeIDs))
	for chunk := range slices.Chunk(nativeIDs, queryBatch) {
		values := make([]any, len(chunk))
		for i, id := range chunk {
			values[i] = id
		}
		var records []*core.Record
		if err := s.app.RecordQuery(videos).AndWhere(dbx.HashExp{"platform": platformID}).AndWhere(dbx.In("platform_id", values...)).All(&records); err != nil {
			return nil, err
		}
		for _, record := range records {
			results[record.GetString("platform_id")] = record
		}
	}
	return results, nil
}

// CachedChannelVideos lists the unexpired cached videos of a channel that userID may see: the public ones
// and those the account owns. The channel is addressed by its record id.
func (s *Store) CachedChannelVideos(channelRecordID, userID string) ([]platform.Video, error) {
	records, err := s.app.FindRecordsByFilter(videos, "channel = {:id} && expires_at > {:now}", "-published", 0, 0, dbx.Params{"id": channelRecordID, "now": time.Now().UTC().Format(platform.StoredLayout)})
	if err != nil {
		return nil, err
	}
	visible := records[:0]
	for _, r := range records {
		if VisibleTo(r, userID) {
			visible = append(visible, r)
		}
	}
	return s.VideosOf(visible), nil
}

// CachedAnswer presents a stored video that may answer a request for its details without asking the
// platform: its metadata was verified, it has its chapters when wantChapters, it is fresh, and it is not a
// stream in progress (that one is only true right now).
func (s *Store) CachedAnswer(r *core.Record, wantChapters bool) (platform.Video, bool) {
	meta := MetadataOf(r)
	if r.GetBool("live") || !meta.OriginalTitleVerified || (wantChapters && !meta.ChaptersLoaded) || !meta.Fresh() {
		return platform.Video{}, false
	}
	return s.VideoOf(r), true
}

// VideoOf presents a stored video, with its owner channel's identity.
func (s *Store) VideoOf(r *core.Record) platform.Video {
	return s.VideosOf([]*core.Record{r})[0]
}

// VideosOf presents stored videos, in order. Every owner channel is loaded with one query per queryBatch
// channels, not one per video. A channel that cannot be loaded leaves the video without the channel's
// name and native id; the native id is never invented.
func (s *Store) VideosOf(records []*core.Record) []platform.Video {
	owners := s.loadChannels(records)
	out := make([]platform.Video, len(records))
	for i, r := range records {
		out[i] = videoBase(r)
		if channel := owners[r.GetString("channel")]; channel != nil {
			out[i].ChannelID = channel.GetString("platform_id")
			out[i].ChannelName = channel.GetString("name")
			out[i].ChannelThumbnail = platform.ChannelImageURL(channel.GetString("thumbnail"))
		}
	}
	return out
}

func (s *Store) loadChannels(records []*core.Record) map[string]*core.Record {
	var ids []string
	seen := map[string]bool{}
	for _, r := range records {
		if id := r.GetString("channel"); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	loaded := make(map[string]*core.Record, len(ids))
	for chunk := range slices.Chunk(ids, queryBatch) {
		found, err := s.app.FindRecordsByIds(channels, chunk)
		if err != nil {
			logging.Of(s.app).Warn("Failed to load the channels of stored videos", "error", err)
			continue
		}
		for _, channel := range found {
			loaded[channel.Id] = channel
		}
	}
	return loaded
}

// videoBase presents the fields of a videos record itself, without its channel's.
func videoBase(r *core.Record) platform.Video {
	title, originalTitle := TitlesOf(r)
	v := platform.Video{RecordID: r.Id, ChannelRecordID: r.GetString("channel"), ID: r.GetString("platform_id"), Platform: r.GetString("platform"), Title: title, OriginalTitle: originalTitle, Description: r.GetString("description"), Duration: r.GetFloat("duration"), ViewCount: r.GetFloat("view_count"), Published: PublishedOf(r), Tags: []string{}}
	_ = r.UnmarshalJSONField("thumbnails", &v.Thumbnails)
	_ = r.UnmarshalJSONField("creators", &v.Creators)
	if len(v.Thumbnails) > 0 {
		v.Thumbnail = v.Thumbnails[0]
	}
	meta := MetadataOf(r)
	v.OriginalLanguage = meta.OriginalLanguage
	v.OriginalTitleVerified = meta.OriginalTitleVerified
	v.LikeCount = meta.LikeCount
	v.DislikeCount = meta.DislikeCount
	v.SubscriberOnly = r.GetBool("subscriber_only")
	v.Live = r.GetBool("live")
	v.Category = r.GetString("category")
	v.Chapters = []platform.Chapter{}
	_ = r.UnmarshalJSONField("chapters", &v.Chapters)
	if v.Chapters == nil {
		v.Chapters = []platform.Chapter{}
	}
	// JSON fields may contain either a native slice or PocketBase's JSONRaw value.
	if data, err := json.Marshal(r.Get("tags")); err == nil {
		_ = json.Unmarshal(data, &v.Tags)
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	return v
}

// ChannelOf presents a stored channel; nil for a stub that has no name yet (a channel known by id only is
// no profile).
func ChannelOf(r *core.Record) *platform.Channel {
	if r.GetString("name") == "" {
		return nil
	}
	return &platform.Channel{RecordID: r.Id, ID: r.GetString("platform_id"), Platform: r.GetString("platform"), Name: r.GetString("name"), Thumbnail: platform.ChannelImageURL(r.GetString("thumbnail")), Description: r.GetString("description"), SubscriberCount: r.GetFloat("subscriber_count")}
}
