package jobs

import (
	"context"
	"errors"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// The page of a video is fetched only when a series needs what a listing lacks (the dates that tell apart a
// numbering that starts over). A rate limit holds back the whole metadata queue and retries the job a bounded
// number of times; any other failure is tried again later, after a pause that grows with each failure,
// unless it is permanent (the video is gone or closed), which ends the job for good.

// submitMetadata makes sure the page of a video of the channel is being fetched, or was. A job that finished
// successfully is stale: the caller only asks for a video that is still not settled, so its freshness window
// ran out. A job that failed for good stays failed. false means the queue is full.
func (a *Analysis) submitMetadata(channelID string, v platform.Video) bool {
	key := workKey{channel: channelID, platform: v.Platform, video: v.ID}
	stale := func(entry Entry[platform.Video, struct{}]) bool { return entry.Status == Complete }
	_, queued := ensureJob(a.metadata, key, v, stale)
	return queued
}

// fetchMetadata is the job: it asks the platform for the video's page, checks that it is the video, owned by
// the channel the catalog says, and stores it. A fetch that did not leave verified, fresh metadata in the
// store failed.
func (a *Analysis) fetchMetadata(ctx context.Context, _ workKey, listed platform.Video) (struct{}, error) {
	p, ok := a.platforms.Get(listed.Platform)
	if !ok {
		return struct{}{}, errors.New("unknown platform")
	}
	video, err := p.VideoMetadata(ctx, listed.ID, "")
	if err != nil {
		return struct{}{}, err
	}
	if video.Platform == "" {
		video.Platform = p.ID()
	}
	if video.ID != listed.ID || video.Platform != listed.Platform || video.Source != "" || canonicalChannel(p, video.ChannelID) != canonicalChannel(p, listed.ChannelID) {
		return struct{}{}, errOwnerMismatch
	}
	if err := a.store.StoreVideo(&video, ""); err != nil {
		return struct{}{}, err
	}
	record, err := a.store.FindVideo(video.Platform, video.ID)
	if err != nil {
		return struct{}{}, err
	}
	if meta := catalog.MetadataOf(record); !meta.OriginalTitleVerified || !meta.Fresh() {
		return struct{}{}, errNotCached
	}
	return struct{}{}, nil
}

// canonicalChannel is the canonical form of a native channel id.
func canonicalChannel(p platform.Platform, id string) string {
	if aliases := p.ChannelIDAliases(id); len(aliases) > 0 {
		return aliases[0]
	}
	return id
}

func (a *Analysis) metadataFinished(key workKey, entry Entry[platform.Video, struct{}]) {
	if entry.Status == Failed {
		logging.Of(a.app).Warn("Series publication metadata unavailable", "video", key.video, "code", entry.Code)
	}
	a.jobFinished(key.channel, a.metadata.Unfinished(), a.opts.MetadataQueueLimit)
}

// metadataRetrying handles a fetch that failed and will be tried again later: the channel is analysed with
// what the other jobs brought, as if this one had finished.
func (a *Analysis) metadataRetrying(key workKey, entry Entry[platform.Video, struct{}]) {
	logging.Of(a.app).Warn("Series publication metadata unavailable, trying again later", "video", key.video, "code", entry.Code, "failures", entry.Failures, "retryAt", entry.RetryAt)
	a.jobFinished(key.channel, a.metadata.Unfinished(), a.opts.MetadataQueueLimit)
}

// jobFinished wakes the channel of a job that finished, unless other jobs of it are still running (the last
// one will), and refills what a full queue left out.
func (a *Analysis) jobFinished(channelID string, unfinished, limit int) {
	if !a.busy(channelID) {
		a.Wake(channelID)
	}
	a.refill(unfinished, limit)
}

// busy tells whether a channel still has a job pending or running. A job that failed and sleeps until its
// next attempt is not work in progress: it does not keep the channel from being analysed.
func (a *Analysis) busy(channelID string) bool {
	counts := a.jobCounts(channelID)
	active := func(c Counts) int { return c.Pending - c.Waiting + c.Running }
	return active(counts.metadata)+active(counts.ocr) > 0
}

// channelCounts tallies the jobs of a channel in the two queues.
type channelCounts struct{ metadata, ocr Counts }

func (a *Analysis) jobCounts(channelID string) channelCounts {
	own := func(key workKey) bool { return key.channel == channelID }
	return channelCounts{metadata: a.metadata.Counts(own), ocr: a.ocr.Counts(own)}
}
