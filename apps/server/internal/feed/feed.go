// Package feed builds the discovery rankings of a profile: its feed (what its subscribed channels published,
// favoring the topics it liked) and the recommendations shown next to a video.
//
// Both are READ-ONLY on the platforms: a request never reaches yt-dlp or a platform. Candidates come from
// the videos the catalog already stores for the subscribed channels; a channel whose listing is stale is
// handed to the Refresher, so the next request finds fresh rows. A feed built from an empty cache is empty,
// not an error.
//
// Expired video rows still count as candidates. A video's expiry says when its metadata should be looked up
// again, not that the video is gone, and a feed that went empty every 24 hours (or whenever the refresh is
// rate limited) would be worse than one that shows the last known listing until it is replaced.
package feed

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

const (
	// feedLimit is how many videos the feed keeps.
	feedLimit = 50
	// recommendationLimit is how many videos the recommendations keep.
	recommendationLimit = 20
	// candidatesPerQuery bounds the newest videos one candidate query reads, so a channel with a whole
	// synchronized catalog cannot make a request read thousands of rows. The newest ones win, which is
	// what a feed wants.
	candidatesPerQuery = 300
	// queryBatch bounds how many channel or video ids one query looks up.
	queryBatch = 100
)

// Refresher refreshes the listings of channels in the background. Schedule never blocks and never fails:
// it is told which channels were found stale, once per request.
type Refresher interface {
	Schedule(channels []*core.Record)
}

// Service ranks the cached videos of a profile. It is safe for concurrent use.
type Service struct {
	app       core.App
	store     *catalog.Store
	accounts  *sources.Service
	refresher Refresher
}

// New returns the service. refresher may be nil: stale channels are then simply not refreshed.
func New(app core.App, store *catalog.Store, accounts *sources.Service, refresher Refresher) *Service {
	return &Service{app: app, store: store, accounts: accounts, refresher: refresher}
}

func failed(message string, cause error) *apierr.Error { return apierr.Internal(message, cause) }

// Feed ranks the cached videos of the profile's subscribed channels: the top 50 by score (see scoreVideo),
// presented newest first. The caller has checked that the profile belongs to the account. The platforms the
// account turned off are left out. A cancelled ctx is reported as its own error.
func (s *Service) Feed(ctx context.Context, userID, profileID string) ([]platform.Video, error) {
	const message = "Feed generation failed"
	frequency, err := s.likedTagFrequency(profileID)
	if err != nil {
		return nil, failed(message, err)
	}
	candidates, err := s.subscriptionCandidates(ctx, userID, profileID)
	if err != nil {
		return nil, s.gatherError(ctx, err, message)
	}
	now := time.Now()
	ranked := make([]scored, 0, len(candidates))
	for _, v := range candidates {
		ranked = append(ranked, scored{v, scoreVideo(v, frequency, nil, now)})
	}
	return top(ranked, feedLimit), nil
}

// Recommendations ranks the videos to watch after the video with the record id anchorVideoRecordID: the
// subscriptions' candidates plus the cached videos of the anchor's own channel (same-channel videos matter
// even without a subscription, and watching the video has most likely cached them), pulled up when they
// share the anchor's channel or tags. The top 20 are presented newest first.
//
// An account that hides subscriber-only videos never sees them here, and a video the profile already watched
// is not suggested again. An anchor that is not stored (or that the account may not see) anchors nothing:
// the answer is then the generic ranking. The caller has checked that the profile belongs to the account.
func (s *Service) Recommendations(ctx context.Context, userID, profileID, anchorVideoRecordID string) ([]platform.Video, error) {
	const message = "Recommendations failed"
	anchor, err := s.anchorOf(userID, anchorVideoRecordID)
	if err != nil {
		return nil, failed(message, err)
	}
	frequency, err := s.likedTagFrequency(profileID)
	if err != nil {
		return nil, failed(message, err)
	}
	candidates, err := s.subscriptionCandidates(ctx, userID, profileID)
	if err != nil {
		return nil, s.gatherError(ctx, err, message)
	}
	if anchor != nil {
		more, err := s.anchorChannelCandidates(userID, anchor.channelRecordID)
		if err != nil {
			return nil, failed(message, err)
		}
		candidates = append(candidates, more...)
	}
	hideSubscriberOnly, err := s.hidesSubscriberOnly(userID)
	if err != nil {
		return nil, failed(message, err)
	}
	// Recommendations are discovery: what the profile already watched is not suggested again.
	watched, err := s.watched(profileID, candidates)
	if err != nil {
		return nil, failed(message, err)
	}
	now := time.Now()
	seen := map[string]bool{}
	ranked := make([]scored, 0, len(candidates))
	for _, v := range candidates {
		if seen[v.RecordID] || watched[v.RecordID] || v.RecordID == anchorVideoRecordID || (hideSubscriberOnly && v.SubscriberOnly) {
			continue
		}
		seen[v.RecordID] = true
		ranked = append(ranked, scored{v, scoreVideo(v, frequency, anchor, now)})
	}
	return top(ranked, recommendationLimit), nil
}

// gatherError reports a cancelled request as its context error and anything else as the generic failure.
func (s *Service) gatherError(ctx context.Context, err error, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var safe *apierr.Error
	if errors.As(err, &safe) {
		return safe
	}
	return failed(message, err)
}

// anchorOf resolves the video recommendations are for; nil when it is not stored or not visible to the account.
func (s *Service) anchorOf(userID, recordID string) (*anchor, error) {
	record, err := s.store.Video(recordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !catalog.VisibleTo(record, userID) {
		return nil, nil
	}
	return &anchor{channelRecordID: record.GetString("channel"), tags: s.store.VideoOf(record).Tags}, nil
}

// likedTagFrequency counts how often each tag appears across the videos a profile liked; the rankings use it
// to favor topics the profile already reacted to. The liked videos are loaded and presented in batches: a
// bounded number of queries whatever the number of likes.
func (s *Service) likedTagFrequency(profileID string) (map[string]int, error) {
	reactions, err := s.app.FindRecordsByFilter("video_reactions", "profile = {:profile} && reaction = 'like'", "", 0, 0, dbx.Params{"profile": profileID})
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, reaction := range reactions {
		if id := reaction.GetString("video"); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var liked []*core.Record
	for chunk := range slices.Chunk(ids, queryBatch) {
		records, err := s.app.FindRecordsByIds("videos", chunk)
		if err != nil {
			return nil, err
		}
		liked = append(liked, records...)
	}
	frequency := map[string]int{}
	for _, video := range s.store.VideosOf(liked) {
		for _, tag := range video.Tags {
			frequency[tag]++
		}
	}
	return frequency, nil
}

// subscriptionCandidates lists the cached videos of the profile's subscribed channels that the account may
// see, from the platforms it uses. The channels whose listing is stale are scheduled for a refresh, in one
// call; nothing else leaves the database.
func (s *Service) subscriptionCandidates(ctx context.Context, userID, profileID string) ([]platform.Video, error) {
	active, err := s.accounts.Activations(userID)
	if err != nil {
		return nil, err
	}
	channels, err := s.subscribedChannels(profileID, active)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.scheduleStale(channels)
	ids := make([]string, len(channels))
	for i, channel := range channels {
		ids[i] = channel.Id
	}
	return s.cachedVideos(userID, ids)
}

// anchorChannelCandidates lists the cached videos of the anchor's channel when its platform is on.
func (s *Service) anchorChannelCandidates(userID, channelRecordID string) ([]platform.Video, error) {
	if channelRecordID == "" {
		return nil, nil
	}
	channel, err := s.store.Channel(channelRecordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	active, err := s.accounts.Activations(userID)
	if err != nil {
		return nil, err
	}
	if !active[channel.GetString("platform")] {
		return nil, nil
	}
	return s.cachedVideos(userID, []string{channel.Id})
}

// subscribedChannels loads the channels the profile subscribes to, with IN queries, keeping those of the
// platforms the account uses.
func (s *Service) subscribedChannels(profileID string, active map[string]bool) ([]*core.Record, error) {
	subscriptions, err := s.app.FindRecordsByFilter("subscriptions", "profile = {:profile}", "", 0, 0, dbx.Params{"profile": profileID})
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, subscription := range subscriptions {
		if id := subscription.GetString("channel"); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var channels []*core.Record
	for chunk := range slices.Chunk(ids, queryBatch) {
		records, err := s.app.FindRecordsByIds("channels", chunk)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if active[record.GetString("platform")] {
				channels = append(channels, record)
			}
		}
	}
	return channels, nil
}

// scheduleStale hands the channels whose listing expired (or was never fetched) to the refresher.
func (s *Service) scheduleStale(channels []*core.Record) {
	if s.refresher == nil {
		return
	}
	now := time.Now()
	var stale []*core.Record
	for _, channel := range channels {
		if !channel.GetDateTime("expires_at").Time().After(now) {
			stale = append(stale, channel)
		}
	}
	if len(stale) > 0 {
		s.refresher.Schedule(stale)
	}
}

// cachedVideos reads the stored videos of the channels (by record id) that the account may see: the public
// ones and those it owns. At most candidatesPerQuery of the newest per batch of channels are read, and they
// are presented with one batch (see catalog.Store.VideosOf).
func (s *Service) cachedVideos(userID string, channelIDs []string) ([]platform.Video, error) {
	var records []*core.Record
	for chunk := range slices.Chunk(channelIDs, queryBatch) {
		params := dbx.Params{"user": userID}
		filter := "("
		for i, id := range chunk {
			if i > 0 {
				filter += " || "
			}
			name := "c" + strconv.Itoa(i)
			filter += "channel = {:" + name + "}"
			params[name] = id
		}
		// The visibility clause is catalog.VisibleTo, in SQL, so the limit counts visible rows only.
		filter += ") && (owners:length = 0 || owners.id ?= {:user})"
		found, err := s.app.FindRecordsByFilter("videos", filter, "-published", candidatesPerQuery, 0, params)
		if err != nil {
			return nil, err
		}
		records = append(records, found...)
	}
	return s.store.VideosOf(records), nil
}

// hidesSubscriberOnly reads the account's setting: it applies to listings in the grid and, since this panel
// renders the API answer as it comes, to the recommendations too.
func (s *Service) hidesSubscriberOnly(userID string) (bool, error) {
	settings, err := s.app.FindFirstRecordByFilter("settings", "user = {:user}", dbx.Params{"user": userID})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return settings.GetBool("hide_subscriber_only"), nil
}

// watched returns the record ids, among the candidates, of the videos the profile has a history row for.
func (s *Service) watched(profileID string, candidates []platform.Video) (map[string]bool, error) {
	watched := map[string]bool{}
	ids := make([]any, 0, len(candidates))
	for _, v := range candidates {
		ids = append(ids, v.RecordID)
	}
	for chunk := range slices.Chunk(ids, queryBatch) {
		var rows []*core.Record
		if err := s.app.RecordQuery("watch_history").AndWhere(dbx.HashExp{"profile": profileID}).AndWhere(dbx.In("video", chunk...)).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			watched[row.GetString("video")] = true
		}
	}
	return watched, nil
}
