package youtube

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

func watchURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

func channelURL(id string) string {
	if strings.HasPrefix(id, "UC") {
		return "https://www.youtube.com/channel/" + id + "/videos"
	}
	return "https://www.youtube.com/@" + strings.TrimPrefix(id, "@") + "/videos"
}

// VideoMetadata reads a video from its own page.
func (p *Platform) VideoMetadata(ctx context.Context, id, cookieFile string) (platform.Video, error) {
	entries, err := p.entries(ctx, []string{"--dump-json", "--no-download", watchURL(id)}, cookieFile)
	if err != nil {
		return platform.Video{}, err
	}
	if len(entries) == 0 {
		return platform.Video{}, apierr.RequestFailed().WithCause(errors.New("no video metadata"))
	}
	video := entries[0].video()
	video.FullMetadata = true
	video.OriginalTitleVerified = true
	video.Manifest = entries[0].playbackManifest()
	return video, nil
}

// ChannelListing reads a channel's profile and its latest videos.
func (p *Platform) ChannelListing(ctx context.Context, id string, limit int, cookieFile string) (platform.Channel, []platform.Video, error) {
	entries, err := p.entries(ctx, []string{channelURL(id), "--dump-single-json", "--flat-playlist", "--playlist-end", strconv.Itoa(limit), "--no-download"}, cookieFile)
	if err != nil {
		return platform.Channel{}, nil, err
	}
	if len(entries) != 1 || entries[0].Type != "playlist" || entries[0].Entries == nil {
		return platform.Channel{}, nil, apierr.RequestFailed().WithCause(errors.New("invalid channel listing"))
	}
	playlist := entries[0]
	info := platform.Channel{
		ID:       platform.First(playlist.ChannelID, strings.TrimPrefix(playlist.UploaderID, "@"), id),
		Platform: platformID, Name: platform.First(playlist.Channel, playlist.Uploader, playlist.Title),
		Thumbnail: playlist.channelThumbnail(), Description: playlist.Description, SubscriberCount: playlist.Followers,
	}
	videos := make([]platform.Video, 0, len(playlist.Entries))
	for _, entry := range playlist.Entries {
		videos = append(videos, channelEntryVideo(entry, info))
	}
	return info, videos, nil
}

func channelEntryVideo(entry ytEntry, info platform.Channel) platform.Video {
	// Flat entries may omit their owner; the playlist owns the channel identity.
	entry.ChannelID = platform.First(entry.ChannelID, info.ID)
	entry.Channel = platform.First(entry.Channel, entry.Uploader, info.Name)
	entry.ChannelThumbnail = platform.First(entry.ChannelThumbnail, info.Thumbnail)
	return entry.video()
}

// PlaybackManifest finds the HLS manifest the video plays from.
func (p *Platform) PlaybackManifest(ctx context.Context, id, cookieFile string) (platform.Manifest, error) {
	entries, err := p.entries(ctx, []string{"--skip-download", "--dump-single-json", "--extractor-args", "youtube:player_client=default,web_safari", watchURL(id)}, cookieFile)
	if err != nil {
		return platform.Manifest{}, err
	}
	if len(entries) != 1 {
		return platform.Manifest{}, apierr.RequestFailed().WithCause(errors.New("expected one video for playback"))
	}
	manifest := entries[0].playbackManifest()
	if manifest.URL == "" {
		return platform.Manifest{}, apierr.RequestFailed().WithCause(errors.New("YouTube did not provide a playable HLS manifest"))
	}
	return manifest, nil
}

type catalogSyncer struct{ p *Platform }

// Catalog streams the provider's entire /videos tab without a playlist-end cap. Lazy extraction makes each
// entry available before the provider finishes pagination. An error of visit stops the listing and is
// returned as it is: it is the caller's failure, not the platform's.
func (s catalogSyncer) Catalog(ctx context.Context, channel platform.Channel, cookieFile string, visit func(platform.Video) error) error {
	var visitErr error
	args := []string{channelURL(channel.ID), "--dump-json", "--flat-playlist", "--lazy-playlist", "--no-download"}
	err := ytdlp.Each(ctx, s.p.runner, spec(args, cookieFile), func(entry ytEntry) error {
		if entry.ID == "" || entry.Type == "playlist" {
			return fmt.Errorf("invalid channel catalog entry")
		}
		if visitErr = visit(channelEntryVideo(entry, channel)); visitErr != nil {
			return visitErr
		}
		return nil
	})
	if visitErr != nil {
		return visitErr
	}
	return fail(ctx, err, cookieFile != "")
}

// Collections: a YouTube channel has playlists, which this platform does not read.
func (catalogSyncer) Collections(context.Context, platform.Channel) ([]platform.Collection, error) {
	return nil, nil
}
