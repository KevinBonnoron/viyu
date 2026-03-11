package jobs

import (
	"context"
	"slices"

	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// Thumbnails are read only for the videos whose title gives no episode number, and only when an
// administrator allowed it and the build has a reader. Readings are jobs of a bounded queue whose finished
// results are the cache: they are not stored, and a result that leaves the queue is read again if a series
// still needs it.

// applyThumbnails queues the reading of the unnumbered videos of the series and gives the ones already read
// their number.
func (a *Analysis) applyThumbnails(channelID string, list []series.Series) {
	if !a.ocrReady() {
		return
	}
	episodes := map[string]int{}
	seen := map[string]bool{}
	for _, v := range series.ThumbnailCandidates(list) {
		id := v.Platform + ":" + v.ID
		if seen[id] {
			continue
		}
		seen[id] = true
		entry, queued := a.submitReading(channelID, v)
		switch {
		case !queued:
			a.deferred.add(channelID)
		case entry.Status == Complete && entry.Out > 0:
			episodes[id] = entry.Out
		}
	}
	a.parser.WithEpisodes(list, episodes)
}

// submitReading makes sure the thumbnails of a video are being read, or were. A reading of other thumbnails
// than the video has now is stale.
func (a *Analysis) submitReading(channelID string, v platform.Video) (Entry[platform.Video, int], bool) {
	stale := func(entry Entry[platform.Video, int]) bool {
		return !slices.Equal(entry.In.ThumbnailURLs(), v.ThumbnailURLs())
	}
	return ensureJob(a.ocr, workKey{channel: channelID, platform: v.Platform, video: v.ID}, v, stale)
}

// ApplyEpisodes gives the unnumbered videos of stored series the episode numbers read from their thumbnails
// so far (series.Store keeps none). It reads the results of the finished readings; it starts nothing.
func (a *Analysis) ApplyEpisodes(list []series.Series) {
	if !a.ocrReady() {
		return
	}
	episodes := map[string]int{}
	for _, s := range list {
		for _, item := range s.Items {
			if item.Episode != nil {
				continue
			}
			v := item.Video
			if entry, ok := a.ocr.Get(workKey{channel: s.ChannelRecordID, platform: v.Platform, video: v.ID}); ok && entry.Status == Complete && entry.Out > 0 {
				episodes[v.Platform+":"+v.ID] = entry.Out
			}
		}
	}
	a.parser.WithEpisodes(list, episodes)
}

// readThumbnails is the job: it reads the episode number off the thumbnails of a video, 0 when they show
// none.
func (a *Analysis) readThumbnails(ctx context.Context, _ workKey, v platform.Video) (int, error) {
	if a.reader == nil {
		return 0, errNoReader
	}
	return a.reader.Episode(ctx, v.ThumbnailURLs())
}

func (a *Analysis) ocrFinished(key workKey, entry Entry[platform.Video, int]) {
	if entry.Status == Failed {
		logging.Of(a.app).Warn("Thumbnail recognition failed", "video", key.video, "code", entry.Code)
	}
	a.jobFinished(key.channel, a.ocr.Unfinished(), a.opts.OCRQueueLimit)
}

// ocrRetrying handles a reading that failed and will be tried again later: the channel is analysed with what
// the other jobs brought, as if this one had finished.
func (a *Analysis) ocrRetrying(key workKey, entry Entry[platform.Video, int]) {
	logging.Of(a.app).Warn("Thumbnail recognition failed, trying again later", "video", key.video, "code", entry.Code, "failures", entry.Failures, "retryAt", entry.RetryAt)
	a.jobFinished(key.channel, a.ocr.Unfinished(), a.opts.OCRQueueLimit)
}
