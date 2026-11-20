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

// yt-dlp's max_comments tuple is total, parents, replies, replies per thread, depth.
// The list starts at depth 1 and reply threads at depth 2: depth 1 silently excludes replies.
// Keep at most 100 parents and 50 replies (5 per thread), since each reply thread costs another request.
const (
	commentArgs      = "150,100,50,5,2"
	commentsLimit    = 100
	repliesLimit     = 50
	repliesPerThread = 5
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
	repliesByParent := make(map[string][]platform.Comment)
	for _, item := range data.Comments {
		if item.ID == "" {
			return nil, apierr.RequestFailed().WithCause(errors.New("comment without an id"))
		}
		isReply := item.Parent != "" && item.Parent != "root"
		if isReply {
			if len(repliesByParent[item.Parent]) >= repliesPerThread {
				continue
			}
		} else if len(comments) == commentsLimit {
			// Keep reading replies to the last included parent, even after reaching the parent limit.
			continue
		}
		comment := platform.Comment{ID: item.ID, Author: item.Author, Text: item.Text}
		if item.Timestamp != nil && *item.Timestamp > 0 {
			comment.PublishedAt = time.Unix(*item.Timestamp, 0).UTC().Format(time.RFC3339)
		}
		if item.LikeCount != nil && *item.LikeCount > 0 {
			comment.LikeCount = *item.LikeCount
		}
		if isReply {
			repliesByParent[item.Parent] = append(repliesByParent[item.Parent], comment)
			continue
		}
		comments = append(comments, comment)
	}
	// Inline yt-dlp replies bypass its limits. Apply the global cap in parent order, excluding orphans.
	remaining := repliesLimit
	for index := range comments {
		if remaining == 0 {
			break
		}
		replies := repliesByParent[comments[index].ID]
		replies = replies[:min(len(replies), remaining)]
		comments[index].Replies = replies
		remaining -= len(replies)
	}
	return comments, nil
}
