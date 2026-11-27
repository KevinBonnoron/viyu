package feed

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// feedVisibility is shared by bounded subscription selection and the final discovery filter. Correlated
// subqueries match a profile and its status on the same row; another profile cannot hide a video.
func (s *Service) feedVisibility(userID, profileID string) (dbx.Expression, error) {
	settings, err := s.app.FindFirstRecordByFilter("settings", "user = {:user}", dbx.Params{"user": userID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	params := dbx.Params{"feedProfile": profileID, "feedUser": userID}
	activity := dbx.NewExp(`NOT EXISTS (
		SELECT 1 FROM {{video_reactions}} AS [[reaction]]
		WHERE [[reaction.video]] = [[videos.id]] AND [[reaction.profile]] = {:feedProfile}
		AND [[reaction.reaction]] = 'not_interested'
	)`, params)
	var allowedActivity dbx.Expression = activity
	if settings != nil && settings.GetBool("hide_watched") {
		allowedActivity = dbx.And(allowedActivity, dbx.NewExp(`NOT EXISTS (
			SELECT 1 FROM {{watch_history}} AS [[history]]
			WHERE [[history.video]] = [[videos.id]] AND [[history.profile]] = {:feedProfile}
			AND [[history.watched]] = 1
		)`, params))
	}
	visible := dbx.And(
		dbx.NewExp("json_array_length(COALESCE(NULLIF([[videos.owners]], ''), '[]')) = 0 OR EXISTS (SELECT 1 FROM json_each([[videos.owners]]) WHERE value = {:feedUser} AND {:feedUser} != '')", params),
		dbx.Or(dbx.HashExp{"videos.live": true}, allowedActivity),
	)
	if settings != nil && settings.GetBool("hide_subscriber_only") {
		visible = dbx.And(visible, dbx.HashExp{"videos.subscriber_only": false})
	}
	return visible, nil
}

// homeCandidates keeps the original bounded, unfiltered listing as discovery seeds, and separately reads
// up to candidatesPerQuery eligible videos per batch. Hidden rows never consume that selection limit.
func (s *Service) homeCandidates(ctx context.Context, userID, profileID string, visibility dbx.Expression) (seeds, candidates []platform.Video, err error) {
	channels, err := s.subscriptionChannelIDs(ctx, userID, profileID)
	if err != nil {
		return nil, nil, err
	}
	seeds, err = s.cachedVideos(userID, channels, time.Time{}, nil)
	if err != nil {
		return nil, nil, err
	}
	var records []*core.Record
	for chunk := range slices.Chunk(channels, queryBatch) {
		ids := make([]any, len(chunk))
		for i, id := range chunk {
			ids[i] = id
		}
		var found []*core.Record
		if err := s.app.RecordQuery("videos").
			AndWhere(dbx.In("videos.channel", ids...)).AndWhere(visibility).
			OrderBy("videos.published DESC", "videos.id").Limit(candidatesPerQuery).All(&found); err != nil {
			return nil, nil, err
		}
		records = append(records, found...)
	}
	return seeds, s.store.VideosOf(records), nil
}

// visibleFeedVideos rechecks discovery result ids against the same predicate as subscription selection.
func (s *Service) visibleFeedVideos(visibility dbx.Expression, videos []platform.Video) ([]platform.Video, error) {
	ids := make([]any, 0, len(videos))
	for _, video := range videos {
		ids = append(ids, video.RecordID)
	}
	allowed := make(map[string]bool, len(videos))
	for chunk := range slices.Chunk(ids, queryBatch) {
		var visibleIDs []string
		if err := s.app.RecordQuery("videos").Select("videos.id").
			AndWhere(dbx.In("videos.id", chunk...)).AndWhere(visibility).Column(&visibleIDs); err != nil {
			return nil, err
		}
		for _, id := range visibleIDs {
			allowed[id] = true
		}
	}
	visible := videos[:0]
	for _, video := range videos {
		if allowed[video.RecordID] {
			visible = append(visible, video)
		}
	}
	return visible, nil
}
