package twitch

import (
	"context"
	"errors"
	"strconv"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

func videoURL(id string) (string, error) {
	if vodIDPattern.MatchString(id) {
		return "https://www.twitch.tv/videos/" + id, nil
	}
	if loginPattern.MatchString(id) {
		return "https://www.twitch.tv/" + id, nil
	}
	return "", apierr.InvalidInput("Provide a valid Twitch VOD ID or channel login.")
}

// entries runs yt-dlp and collects the entries it prints.
func (p *Platform) entries(ctx context.Context, args []string, cookieFile string) ([]entry, error) {
	entries, err := ytdlp.All[entry](ctx, p.runner, ytdlp.Spec{Args: ytdlp.WithCookies(args, cookieFile)})
	return entries, fail(err)
}

func (p *Platform) VideoMetadata(ctx context.Context, id, cookieFile string) (platform.Video, error) {
	address, err := videoURL(id)
	if err != nil {
		return platform.Video{}, err
	}
	entries, err := p.entries(ctx, []string{"--dump-json", "--no-download", address}, cookieFile)
	if err != nil {
		return platform.Video{}, err
	}
	if len(entries) != 1 {
		return platform.Video{}, apierr.RequestFailed().WithCause(errors.New("expected one Twitch video"))
	}
	video := entries[0].video()
	if loginPattern.MatchString(id) {
		video.ID = id
		// A channel login, as opposed to a VOD ID, is a stream in progress.
		video.Live = !vodIDPattern.MatchString(id)
	}
	if video.ID != id || (!vodIDPattern.MatchString(video.ID) && !loginPattern.MatchString(video.ID)) {
		return platform.Video{}, apierr.RequestFailed().WithCause(errors.New("invalid Twitch video metadata"))
	}
	video.FullMetadata = true
	video.Manifest = entries[0].playbackManifest()
	if video.Live {
		// yt-dlp names a stream "<channel> (live) <date>": the stream's own title and game, and the audience it has now,
		// come from Twitch. Without them the downloader's values are kept.
		if live, err := p.LiveStream(ctx, id); err == nil && live.Live {
			video.Title, video.OriginalTitle = platform.First(live.Title, video.Title), platform.First(live.Title, video.OriginalTitle)
			video.ViewCount, video.Category = live.Viewers, live.Category
		}
	}
	if cookieFile == "" {
		// Playable without an account: not restricted to subscribers.
		video.SubscriberOnly, video.SubscriberChecked = false, true
	}
	return video, nil
}

func (p *Platform) ChannelListing(ctx context.Context, id string, limit int, cookieFile string) (platform.Channel, []platform.Video, error) {
	if !loginPattern.MatchString(id) {
		return platform.Channel{}, nil, apierr.InvalidInput("Provide a valid Twitch channel login.")
	}
	address := "https://www.twitch.tv/" + id + "/videos?filter=all&sort=time"
	listing, err := p.entries(ctx, []string{address, "--dump-single-json", "--flat-playlist", "--playlist-end", strconv.Itoa(limit), "--no-download"}, cookieFile)
	if err != nil {
		return platform.Channel{}, nil, err
	}
	if len(listing) != 1 || listing[0].Type != "playlist" || listing[0].Entries == nil {
		return platform.Channel{}, nil, apierr.RequestFailed().WithCause(errors.New("invalid Twitch channel listing"))
	}
	playlist := listing[0]
	info := platform.Channel{ID: platform.First(playlist.ID, id), Platform: ID, Name: platform.First(playlist.Channel, playlist.Uploader, id), Thumbnail: playlist.channelThumbnail(), Description: playlist.Description, SubscriberCount: playlist.Followers}
	vods := p.completeChannel(ctx, &info, len(playlist.Entries))
	videos := make([]platform.Video, 0, len(playlist.Entries))
	for _, item := range playlist.Entries {
		item.UploaderID = platform.First(item.UploaderID, info.ID)
		item.Uploader = platform.First(item.Uploader, info.Name)
		item.ChannelThumbnail = platform.First(item.ChannelThumbnail, info.Thumbnail)
		video := item.video()
		if !vodIDPattern.MatchString(video.ID) {
			return platform.Channel{}, nil, apierr.RequestFailed().WithCause(errors.New("invalid Twitch channel entry"))
		}
		if vod, ok := vods[video.ID]; ok {
			vod.fill(&video)
		}
		videos = append(videos, video)
	}
	// A one-VOD listing only reads the channel itself: its restriction is not worth a request.
	if limit > 1 {
		p.markSubscriberOnly(ctx, videos)
	}
	return info, videos, nil
}

func (p *Platform) PlaybackManifest(ctx context.Context, id, cookieFile string) (platform.Manifest, error) {
	address, err := videoURL(id)
	if err != nil {
		return platform.Manifest{}, err
	}
	entries, err := p.entries(ctx, []string{"--skip-download", "--dump-single-json", address}, cookieFile)
	if err != nil {
		return platform.Manifest{}, err
	}
	if len(entries) != 1 {
		return platform.Manifest{}, apierr.RequestFailed().WithCause(errors.New("expected one Twitch video for playback"))
	}
	manifest := entries[0].playbackManifest()
	if manifest.URL == "" {
		return platform.Manifest{}, apierr.RequestFailed().WithCause(errors.New("Twitch did not provide a playable HLS manifest"))
	}
	return manifest, nil
}
