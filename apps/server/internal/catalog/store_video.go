package catalog

import (
	"errors"
	"fmt"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// StoreVideo stores a video and its owner channel, and reports the record ids back through v. ownerID is
// empty for a public fetch and the account's id for a connected one: see applyOwners. On failure v keeps
// an empty RecordID (see the package comment).
func (s *Store) StoreVideo(v *platform.Video, ownerID string) error {
	return s.storeVideos([]*platform.Video{v}, ownerID)
}

// StoreVideos stores a batch of videos and their channels in place: each gets its RecordID and
// ChannelRecordID. The videos that could not be stored keep an empty RecordID and are reported by the
// returned error, which joins one error per failed video; the others are stored regardless.
func (s *Store) StoreVideos(videos []platform.Video, ownerID string) error {
	pointers := make([]*platform.Video, len(videos))
	for i := range videos {
		pointers[i] = &videos[i]
	}
	return s.storeVideos(pointers, ownerID)
}

func (s *Store) storeVideos(videos []*platform.Video, ownerID string) error {
	var errs []error
	var w wanted
	for _, chunk := range chunks(videos) {
		chunkWanted, err := s.storeChunk(chunk, ownerID)
		w.merge(chunkWanted)
		if err != nil {
			errs = append(errs, err)
		}
	}
	s.wake(w)
	return errors.Join(errs...)
}

// stored is what storing one video produced.
type stored struct {
	recordID, channelRecordID string
	err                       error
}

// storeChunk stores at most batchSize videos in one transaction. The record ids are handed to the videos
// only once the transaction committed.
func (s *Store) storeChunk(chunk []*platform.Video, ownerID string) (wanted, error) {
	results := make([]stored, len(chunk))
	var w wanted
	err := s.write(func(tx *Store) error {
		channelOf := map[string]storedChannel{}
		for i, v := range chunk {
			var itemWanted wanted
			results[i], itemWanted = tx.storeVideo(v, channelOf, ownerID)
			w.merge(itemWanted)
		}
		return nil
	})
	if err != nil {
		for _, v := range chunk {
			v.RecordID, v.ChannelRecordID = "", ""
		}
		return wanted{}, err
	}
	var errs []error
	for i, v := range chunk {
		if results[i].err != nil {
			v.RecordID, v.ChannelRecordID = "", ""
			errs = append(errs, fmt.Errorf("video %s/%s: %w", v.Platform, v.ID, results[i].err))
			continue
		}
		v.RecordID, v.ChannelRecordID = results[i].recordID, results[i].channelRecordID
	}
	return w, errors.Join(errs...)
}

// storedChannel is the outcome of storing the owner channel of the videos of a batch, once per channel.
type storedChannel struct {
	id  string
	err error
}

// storeVideo stores one video and, the first time the batch names it, its owner channel. A video whose
// channel could not be stored fails with it: storing it detached would leave a video that names a channel
// it has no record of.
func (s *Store) storeVideo(v *platform.Video, channelOf map[string]storedChannel, ownerID string) (stored, wanted) {
	var w wanted
	channelID := ""
	if v.ChannelID != "" {
		key := v.Platform + "\x00" + v.ChannelID
		channel, seen := channelOf[key]
		if !seen {
			r, written, err := s.upsertChannel(v.Platform, v.ChannelID, v.ChannelName, v.ChannelThumbnail, ownerID)
			channel = storedChannel{err: err}
			if err == nil {
				channel.id = r.Id
				w.profiles = written && profileMissing(r)
			}
			channelOf[key] = channel
		}
		if channel.err != nil {
			return stored{err: fmt.Errorf("channel %s: %w", v.ChannelID, channel.err)}, w
		}
		channelID = channel.id
	}
	// Listings that name no owner (a flat search result) are stored without a channel: the video stays
	// addressable by record id, and a later listing fills the owner in.
	r, written, err := s.upsertVideo(v, channelID, ownerID)
	if err != nil {
		return stored{err: err}, w
	}
	w.publications = written && PublishedOf(r) == ""
	return stored{recordID: r.Id, channelRecordID: r.GetString("channel")}, w
}

func (s *Store) upsertVideo(v *platform.Video, channelID, ownerID string) (*core.Record, bool, error) {
	return s.upsert(videos, v.Platform, v.ID, func(r *core.Record) error {
		if len(v.Thumbnails) == 0 && v.Thumbnail != "" {
			v.Thumbnails = []string{v.Thumbnail}
		}
		if v.FullMetadata {
			v.OriginalTitleVerified = true
		}
		if p, ok := s.platforms.Get(v.Platform); ok && p.SingleTitle() {
			// The platform has a single title: there is no translation to tell apart from an original, so the
			// clients, which read this record directly, must never wait for a verification.
			v.OriginalTitleVerified = true
			v.OriginalTitle = platform.First(v.OriginalTitle, v.Title)
		}
		v.Published = platform.CarryPublished(v.Published, PublishedOf(r))
		if !v.FullMetadata && !r.IsNew() {
			mergeListingVideo(v, videoBase(r))
		}
		now := time.Now()
		r.Load(map[string]any{
			"thumbnails":  v.Thumbnails,
			"description": truncateDescription(v.Description),
			"duration":    v.Duration,
			"view_count":  v.ViewCount,
			"tags":        v.Tags,
			"published":   platform.PublishedToStored(v.Published),
		})
		if expiry, ok := renewal(r.GetDateTime("expires_at").Time(), now); ok {
			r.Set("expires_at", expiry)
		}
		applyOwners(r, ownerID)
		SetTitles(r, v.Title, v.OriginalTitle)
		if channelID != "" {
			r.Set("channel", channelID)
		}
		if len(v.Creators) > 0 || r.IsNew() {
			r.Set("creators", v.Creators)
		}
		meta := MetadataOf(r)
		meta.OriginalLanguage = v.OriginalLanguage
		meta.OriginalTitleVerified = v.OriginalTitleVerified
		meta.LikeCount = v.LikeCount
		meta.DislikeCount = v.DislikeCount
		r.Set("live", v.Live)
		if v.SubscriberChecked {
			r.Set("subscriber_only", v.SubscriberOnly)
		}
		if v.FullMetadata {
			chapters := v.Chapters
			if chapters == nil {
				chapters = []platform.Chapter{}
			}
			r.Set("chapters", chapters)
			r.Set("category", v.Category)
			meta.ChaptersLoaded = true
			current, _ := time.Parse(platform.StoredLayout, meta.MetadataExpiresAt)
			if expiry, ok := renewal(current, now); ok {
				meta.MetadataExpiresAt = expiry.UTC().Format(platform.StoredLayout)
			}
		}
		SetMetadata(r, meta)
		return nil
	})
}

// mergeListingVideo completes a flat listing with what is cached. A flat listing is useful for discovery,
// not for replacing metadata obtained from the video or its publication feed, even after that metadata
// expires.
func mergeListingVideo(listing *platform.Video, cached platform.Video) {
	listing.Chapters = cached.Chapters
	if cached.OriginalTitleVerified {
		listing.Title = cached.Title
		listing.OriginalTitle = cached.OriginalTitle
		listing.OriginalTitleVerified = true
		listing.OriginalLanguage = cached.OriginalLanguage
		listing.Description = cached.Description
		listing.Duration = cached.Duration
		listing.Tags = cached.Tags
	}
	// What is cached wins, but a day is refined by the exact time the listing gives for that day.
	listing.Published = platform.CarryPublished(cached.Published, listing.Published)
	if len(cached.Thumbnails) > 0 {
		merged := append([]string(nil), cached.Thumbnails...)
		seen := make(map[string]bool, len(merged)+len(listing.Thumbnails))
		for _, thumbnail := range merged {
			seen[thumbnail] = true
		}
		for _, thumbnail := range listing.Thumbnails {
			if thumbnail != "" && !seen[thumbnail] {
				seen[thumbnail] = true
				merged = append(merged, thumbnail)
			}
		}
		listing.Thumbnails = merged
		listing.Thumbnail = merged[0]
	}
	if listing.LikeCount == nil {
		listing.LikeCount = cached.LikeCount
	}
	if listing.DislikeCount == nil {
		listing.DislikeCount = cached.DislikeCount
	}
}
