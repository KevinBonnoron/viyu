package youtube

import (
	"context"
	"errors"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// LiveStream looks for a stream in progress on a channel's Streams tab, whose flat listing carries each
// entry's live status. A channel without that tab is not live.
func (p *Platform) LiveStream(ctx context.Context, channel string) (platform.LiveStream, error) {
	address := strings.TrimSuffix(channelURL(channel), "/videos") + "/streams"
	entries, err := p.entries(ctx, []string{address, "--dump-single-json", "--flat-playlist", "--playlist-end", "5", "--no-download"}, "")
	if err != nil {
		var failed *ytdlp.Error
		if errors.As(err, &failed) && strings.Contains(strings.ToLower(failed.Stderr), "does not have a streams tab") {
			return platform.LiveStream{}, nil
		}
		return platform.LiveStream{}, err
	}
	for _, playlist := range entries {
		for _, entry := range playlist.Entries {
			if entry.LiveStatus == "is_live" && videoIDPattern.MatchString(entry.ID) {
				return platform.LiveStream{Live: true, VideoID: entry.ID, Title: entry.Title, Viewers: entry.ViewCount}, nil
			}
		}
	}
	return platform.LiveStream{}, nil
}
