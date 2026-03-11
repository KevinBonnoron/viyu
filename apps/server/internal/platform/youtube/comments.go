package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// yt-dlp exposes a bounded comment extraction, not a resumable cursor. The tuple is max_comments,
// max_parents, max_replies, max_replies_per_thread, max_depth: 100 parent comments with up to 50 replies
// between them (5 per thread) — fetching every answer thread would need a request per thread, which a
// whole comment list cannot afford.
const (
	commentArgs   = "150,100,50,5,1"
	commentsLimit = 100
)

// Comments lists the top-level comments of a video with the replies yt-dlp fetched, nested under their
// parent in the order YouTube returned them. Replies whose parent was not fetched are dropped.
func (p *Platform) Comments(ctx context.Context, id, cookieFile string) ([]platform.Comment, error) {
	if !videoIDPattern.MatchString(id) {
		return nil, apierr.InvalidInput("Provide a valid video ID.")
	}
	run := spec([]string{"--dump-single-json", "--skip-download", "--no-playlist", "--get-comments", "--extractor-args", "youtube:max_comments=" + commentArgs, watchURL(id)}, cookieFile)
	if cookieFile != "" {
		run.Stderr = downloaderError
	}
	var data struct {
		ID       string `json:"id"`
		Comments []struct {
			ID        string `json:"id"`
			Author    string `json:"author"`
			Text      string `json:"text"`
			Timestamp *int64 `json:"timestamp"`
			LikeCount *int64 `json:"like_count"`
			Parent    string `json:"parent"`
		} `json:"comments"`
	}
	err := ytdlp.Decode(ctx, p.runner, run, func(decoder *json.Decoder) error {
		if err := decoder.Decode(&data); err != nil {
			return err
		}
		if data.ID != id {
			return errors.New("invalid comment data")
		}
		return nil
	})
	if err != nil {
		return nil, fail(ctx, err, cookieFile != "")
	}
	comments := make([]platform.Comment, 0, min(len(data.Comments), commentsLimit))
	indexByID := make(map[string]int, len(data.Comments))
	repliesByParent := make(map[string][]platform.Comment)
	for _, item := range data.Comments {
		if item.ID == "" {
			return nil, apierr.RequestFailed().WithCause(errors.New("comment without an id"))
		}
		comment := platform.Comment{ID: item.ID, Author: item.Author, Text: item.Text}
		if item.Timestamp != nil && *item.Timestamp > 0 {
			comment.PublishedAt = time.Unix(*item.Timestamp, 0).UTC().Format(time.RFC3339)
		}
		if item.LikeCount != nil && *item.LikeCount > 0 {
			comment.LikeCount = *item.LikeCount
		}
		if item.Parent != "" && item.Parent != "root" {
			repliesByParent[item.Parent] = append(repliesByParent[item.Parent], comment)
			continue
		}
		indexByID[item.ID] = len(comments)
		comments = append(comments, comment)
		if len(comments) == commentsLimit {
			break
		}
	}
	for parent, replies := range repliesByParent {
		if index, ok := indexByID[parent]; ok {
			comments[index].Replies = replies
		}
	}
	return comments, nil
}
