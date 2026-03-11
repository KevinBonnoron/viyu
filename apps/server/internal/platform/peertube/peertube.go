// Package peertube is the PeerTube implementation of platform.Platform.
//
// PeerTube is a federation of independent servers (instances), each hosting its own videos. A video or a
// channel is therefore named together with its instance: videos are "host:uuid" and channels "name@host".
// Only the instances the server lists (VIYU_PEERTUBE_INSTANCES, or the defaults below) can be reached, so a
// crafted identifier cannot make the server contact an arbitrary host. Search goes through Sepia Search,
// the index of the whole network, and keeps what lives on a listed instance. The platform is public only:
// it has no account to connect.
package peertube

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/safehttp"
)

const (
	platformID = "peertube"

	envInstances = "VIYU_PEERTUBE_INSTANCES"
	// DefaultSepiaURL is the production Sepia Search endpoint.
	DefaultSepiaURL = "https://sepiasearch.org"

	jsonLimit    = 8 << 20
	commentLimit = 100
	// channelsPerPage is how many channels a page of results carries next to its videos.
	channelsPerPage = 5
)

var (
	hostPattern    = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)
	videoRef       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	channelRef     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,50}$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	htmlTags       = regexp.MustCompile(`<[^>]*>`)
	htmlLineBreaks = regexp.MustCompile(`(?i)<br\s*/?>|</p>\s*<p[^>]*>`)
)

// defaultInstances are the instances reachable when none is configured.
func defaultInstances() []string {
	return []string{"framatube.org", "video.blender.org", "tilvids.com", "diode.zone", "makertube.net"}
}

// configuredInstances reads the instance list from the environment, falling back to the defaults.
func configuredInstances() []string {
	var hosts []string
	for _, host := range strings.Split(os.Getenv(envInstances), ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); hostPattern.MatchString(host) {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return defaultInstances()
	}
	return hosts
}

// Options configure a Platform; the zero value is the production configuration.
type Options struct {
	// HTTP is the client for every request; nil means safehttp.Public().
	HTTP *http.Client
	// Scheme is the URL scheme of an instance; default "https" (tests use "http").
	Scheme string
	// SepiaURL is the root of the Sepia Search endpoint; default DefaultSepiaURL.
	SepiaURL string
	// Instances are the hosts that may be contacted; nil reads VIYU_PEERTUBE_INSTANCES at construction
	// time and falls back to the defaults.
	Instances []string
	// Log receives diagnostics; nil discards them.
	Log *slog.Logger
}

// Platform is the PeerTube network as a platform.Platform.
type Platform struct {
	client    *http.Client
	scheme    string
	sepiaURL  string
	instances []string
	log       *slog.Logger
}

var _ platform.Platform = (*Platform)(nil)

// New builds the platform from options.
func New(opts Options) *Platform {
	p := &Platform{client: opts.HTTP, scheme: opts.Scheme, sepiaURL: opts.SepiaURL, log: opts.Log}
	if p.client == nil {
		p.client = safehttp.Public()
	}
	if p.scheme == "" {
		p.scheme = "https"
	}
	if p.sepiaURL == "" {
		p.sepiaURL = DefaultSepiaURL
	}
	if p.log == nil {
		p.log = slog.New(slog.DiscardHandler)
	}
	if opts.Instances != nil {
		p.instances = slices.Clone(opts.Instances)
	} else {
		p.instances = configuredInstances()
	}
	return p
}

func notFound() *apierr.Error {
	return apierr.New(http.StatusNotFound, "SOURCE_NOT_FOUND", "This video or channel does not exist on its instance.")
}

func streamUnsupported() *apierr.Error {
	return apierr.New(http.StatusUnprocessableEntity, "SOURCE_STREAM_UNSUPPORTED", "This video has no HLS stream, which Viyu needs to play it.")
}

func (p *Platform) ID() string { return platformID }

func (p *Platform) Descriptor() platform.Descriptor {
	return platform.Descriptor{
		Platform: platformID, Name: "PeerTube",
		Description:       "Search and watch videos from the federated PeerTube network. No account needed.",
		Capabilities:      []string{"search", "video", "channel", "stream"},
		ConnectionMethods: []string{},
	}
}

// A public platform has no session to connect.
func (p *Platform) Connector() platform.Connector { return nil }

// PeerTube is opt-in: its results come from other people's servers and a search would otherwise start
// including them for every existing account.
func (p *Platform) DefaultEnabled() bool { return false }

func (p *Platform) allowed(host string) bool { return slices.Contains(p.instances, host) }

// splitVideo splits "host:uuid" at the last colon, since a host may carry a port.
func splitVideo(id string) (host, uuid string, ok bool) {
	cut := strings.LastIndex(id, ":")
	if cut <= 0 {
		return "", "", false
	}
	host, uuid = id[:cut], id[cut+1:]
	return host, uuid, hostPattern.MatchString(host) && videoRef.MatchString(uuid)
}

// splitChannel splits "name@host".
func splitChannel(id string) (name, host string, ok bool) {
	cut := strings.LastIndex(id, "@")
	if cut <= 0 {
		return "", "", false
	}
	name, host = id[:cut], id[cut+1:]
	return name, host, channelRef.MatchString(name) && hostPattern.MatchString(host)
}

// ClaimsVideoID accepts any well-formed "host:uuid", listed instance or not: an unlisted one must fail as
// a PeerTube video rather than be mistaken for another platform's.
func (p *Platform) ClaimsVideoID(id string) bool {
	_, _, ok := splitVideo(id)
	return ok
}

func (p *Platform) ValidVideoID(id string) bool {
	host, _, ok := splitVideo(id)
	return ok && p.allowed(host)
}

func (p *Platform) OriginalURL(id string) string {
	host, uuid, ok := splitVideo(id)
	if !ok || !p.allowed(host) {
		return ""
	}
	return p.base(host) + "/w/" + url.PathEscape(uuid)
}

func (p *Platform) ValidChannelID(id string) bool {
	_, host, ok := splitChannel(id)
	return ok && p.allowed(host)
}

func (p *Platform) ValidLiveChannel(id string) bool { return p.ValidChannelID(id) }

// ResolveURL accepts the watch pages of a listed instance: /w/<uuid> or /videos/watch/<uuid>.
func (p *Platform) ResolveURL(link platform.Link) (string, bool) {
	uuid := ""
	switch {
	case len(link.Parts) == 2 && link.Parts[0] == "w":
		uuid = link.Parts[1]
	case len(link.Parts) == 3 && link.Parts[0] == "videos" && link.Parts[1] == "watch":
		uuid = link.Parts[2]
	}
	if uuid == "" || !uuidPattern.MatchString(uuid) || !p.allowed(link.Host) {
		return "", false
	}
	return link.Host + ":" + uuid, true
}

func (p *Platform) ChannelIDAliases(id string) []string { return []string{id} }

func (p *Platform) SingleTitle() bool { return true }

// TrustedMediaHost trusts the listed instances: their playlists point at their own servers.
func (p *Platform) TrustedMediaHost(host string) bool { return p.allowed(host) }

func (p *Platform) CatalogSync() platform.CatalogSyncer { return nil }

func (p *Platform) Chat() platform.Chat { return nil }

// EnrichListing has nothing to add: an instance's listing already carries exact dates and titles.
func (p *Platform) EnrichListing(context.Context, string, []platform.Video) {}

type avatar struct {
	Path  string `json:"path"`
	URL   string `json:"url"`
	Width int    `json:"width"`
}

type channel struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"displayName"`
	Host           string   `json:"host"`
	Description    *string  `json:"description"`
	FollowersCount float64  `json:"followersCount"`
	Avatars        []avatar `json:"avatars"`
}

type video struct {
	UUID          string               `json:"uuid"`
	Name          string               `json:"name"`
	Description   *string              `json:"description"`
	Duration      float64              `json:"duration"`
	Views         float64              `json:"views"`
	Viewers       float64              `json:"viewers"`
	Likes         *float64             `json:"likes"`
	Dislikes      *float64             `json:"dislikes"`
	PublishedAt   string               `json:"publishedAt"`
	ThumbnailPath string               `json:"thumbnailPath"`
	PreviewPath   string               `json:"previewPath"`
	ThumbnailURL  string               `json:"thumbnailUrl"`
	IsLive        bool                 `json:"isLive"`
	Tags          []string             `json:"tags"`
	Language      struct{ ID *string } `json:"language"`
	Category      struct {
		ID    *int   `json:"id"`
		Label string `json:"label"`
	} `json:"category"`
	Channel            channel `json:"channel"`
	StreamingPlaylists []struct {
		Type        int    `json:"type"`
		PlaylistURL string `json:"playlistUrl"`
	} `json:"streamingPlaylists"`
}

// base is the root URL of an instance.
func (p *Platform) base(host string) string { return p.scheme + "://" + host }

// get decodes a JSON answer of an instance (or of Sepia Search) into target. A missing resource and any
// other failure are told apart; neither carries the instance's own message. A cancelled context is
// returned as it is.
func (p *Platform) get(ctx context.Context, address string, target any) error {
	failed := func(cause error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.log.Debug("PeerTube request failed", "url", address, "error", cause)
		return apierr.RequestFailed().WithCause(cause)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return failed(err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return failed(err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return notFound()
	case response.StatusCode != http.StatusOK:
		return failed(errors.New("unexpected status " + response.Status))
	}
	limited := &io.LimitedReader{R: response.Body, N: jsonLimit + 1}
	if err := json.NewDecoder(limited).Decode(target); err != nil {
		return failed(err)
	}
	if limited.N <= 0 {
		return failed(errors.New("answer too large"))
	}
	return nil
}

func avatarURL(base string, avatars []avatar) string {
	best := avatar{}
	for _, a := range avatars {
		if a.Width >= best.Width {
			best = a
		}
	}
	return absolute(base, platform.First(best.URL, best.Path))
}

// absolute makes a path an address on the instance, and leaves an address alone.
func absolute(base, reference string) string {
	if reference == "" {
		return ""
	}
	if strings.HasPrefix(reference, "http://") || strings.HasPrefix(reference, "https://") {
		return reference
	}
	return base + reference
}

func channelID(c channel) string { return c.Name + "@" + c.Host }

// video turns an instance's (or Sepia's) description of a video into a Viyu video; host is the instance the
// answer came from, used when a path has to become an address.
func (p *Platform) video(item video, host string) platform.Video {
	origin := platform.First(item.Channel.Host, host)
	base := p.base(origin)
	thumbnail := platform.First(item.ThumbnailURL, absolute(base, item.ThumbnailPath))
	thumbnails := []string{}
	for _, candidate := range []string{absolute(base, item.PreviewPath), thumbnail} {
		if candidate != "" {
			thumbnails = append(thumbnails, candidate)
		}
	}
	tags := item.Tags
	if tags == nil {
		tags = []string{}
	}
	result := platform.Video{
		ID: origin + ":" + item.UUID, Platform: platformID,
		Title: item.Name, OriginalTitle: item.Name, OriginalTitleVerified: true,
		ChannelID: channelID(channel{Name: item.Channel.Name, Host: origin}), ChannelName: platform.First(item.Channel.DisplayName, item.Channel.Name),
		ChannelThumbnail: avatarURL(base, item.Channel.Avatars),
		Thumbnail:        thumbnail, Thumbnails: thumbnails,
		Duration: item.Duration, Tags: tags, Chapters: []platform.Chapter{},
		ViewCount: item.Views, LikeCount: item.Likes, DislikeCount: item.Dislikes,
		Live: item.IsLive,
	}
	if item.Description != nil {
		result.Description = *item.Description
	}
	if item.Language.ID != nil {
		result.OriginalLanguage = *item.Language.ID
	}
	// An unset category comes as {id: null, label: "Unknown"}.
	if item.Category.ID != nil {
		result.Category = item.Category.Label
	}
	if published, err := time.Parse(time.RFC3339Nano, item.PublishedAt); err == nil {
		result.Published = published.UTC().Format(time.RFC3339)
	}
	return result
}

func (p *Platform) videoURL(host, uuid string) string {
	return p.base(host) + "/api/v1/videos/" + url.PathEscape(uuid)
}

// detail fetches one video from its instance. An instance must be listed before it is contacted.
func (p *Platform) detail(ctx context.Context, id string) (video, string, error) {
	host, uuid, ok := splitVideo(id)
	if !ok || !p.allowed(host) {
		return video{}, "", apierr.InvalidInput("Provide a video of a supported PeerTube instance.")
	}
	var item video
	if err := p.get(ctx, p.videoURL(host, uuid), &item); err != nil {
		return video{}, "", err
	}
	if item.UUID == "" {
		return video{}, "", apierr.RequestFailed().WithCause(errors.New("video answer without uuid"))
	}
	return item, host, nil
}

func (p *Platform) VideoMetadata(ctx context.Context, id, _ string) (platform.Video, error) {
	item, host, err := p.detail(ctx, id)
	if err != nil {
		return platform.Video{}, err
	}
	// A video's own description is cut in the answer above; the full text has an endpoint of its own. It only
	// decorates the page, so a failure keeps the short one.
	var full struct {
		Description string `json:"description"`
	}
	if p.get(ctx, p.videoURL(host, item.UUID)+"/description", &full) == nil && full.Description != "" {
		item.Description = &full.Description
	}
	result := p.video(item, host)
	result.ID = id
	result.FullMetadata = true
	if playlist := hlsPlaylist(item); playlist != "" {
		result.Manifest = platform.Manifest{URL: playlist}
	}
	return result, nil
}

// hlsPlaylist is the master playlist of a video, empty when the instance only offers progressive files.
func hlsPlaylist(item video) string {
	for _, playlist := range item.StreamingPlaylists {
		if playlist.Type == 1 && (strings.HasPrefix(playlist.PlaylistURL, "https://") || strings.HasPrefix(playlist.PlaylistURL, "http://")) {
			return playlist.PlaylistURL
		}
	}
	return ""
}

func (p *Platform) PlaybackManifest(ctx context.Context, id, _ string) (platform.Manifest, error) {
	item, _, err := p.detail(ctx, id)
	if err != nil {
		return platform.Manifest{}, err
	}
	playlist := hlsPlaylist(item)
	if playlist == "" {
		return platform.Manifest{}, streamUnsupported()
	}
	return platform.Manifest{URL: playlist}, nil
}

func (p *Platform) channelURL(name, host string) string {
	return p.base(host) + "/api/v1/video-channels/" + url.PathEscape(name+"@"+host)
}

func (p *Platform) ChannelListing(ctx context.Context, id string, limit int, _ string) (platform.Channel, []platform.Video, error) {
	name, host, ok := splitChannel(id)
	if !ok || !p.allowed(host) {
		return platform.Channel{}, nil, apierr.InvalidInput("Provide a channel of a supported PeerTube instance.")
	}
	var profile channel
	if err := p.get(ctx, p.channelURL(name, host), &profile); err != nil {
		return platform.Channel{}, nil, err
	}
	base := p.base(host)
	info := platform.Channel{ID: name + "@" + host, Platform: platformID, Name: platform.First(profile.DisplayName, profile.Name), Thumbnail: avatarURL(base, profile.Avatars), SubscriberCount: profile.FollowersCount}
	if profile.Description != nil {
		info.Description = *profile.Description
	}
	var listing struct {
		Data []video `json:"data"`
	}
	query := url.Values{"start": {"0"}, "count": {strconv.Itoa(min(limit, 100))}, "sort": {"-publishedAt"}}
	if err := p.get(ctx, p.channelURL(name, host)+"/videos?"+query.Encode(), &listing); err != nil {
		return platform.Channel{}, nil, err
	}
	videos := make([]platform.Video, 0, len(listing.Data))
	for _, item := range listing.Data {
		if item.UUID == "" {
			continue
		}
		if item.Channel.Name == "" {
			item.Channel = profile
		}
		listed := p.video(item, host)
		listed.ChannelThumbnail = platform.First(listed.ChannelThumbnail, info.Thumbnail)
		videos = append(videos, listed)
	}
	return info, videos, nil
}

func (p *Platform) Search(ctx context.Context, _ platform.Session, query string, page int) (platform.SearchPage, error) {
	result := platform.SearchPage{Results: []platform.Result{}, Page: page}
	videoStart, channelStart := (page-1)*platform.SearchPageSize, (page-1)*channelsPerPage
	params := func(start, count int) string {
		return url.Values{"search": {query}, "start": {strconv.Itoa(start)}, "count": {strconv.Itoa(count)}, "nsfw": {"false"}}.Encode()
	}
	var videos struct {
		Total float64 `json:"total"`
		Data  []video `json:"data"`
	}
	if err := p.get(ctx, p.sepiaURL+"/api/v1/search/videos?"+params(videoStart, platform.SearchPageSize), &videos); err != nil {
		return platform.SearchPage{}, err
	}
	// Channels only decorate a page of videos: failing to find them is not failing the search, unless the
	// request itself was cancelled.
	var channels struct {
		Total float64   `json:"total"`
		Data  []channel `json:"data"`
	}
	if err := p.get(ctx, p.sepiaURL+"/api/v1/search/video-channels?"+params(channelStart, channelsPerPage), &channels); err != nil {
		if ctx.Err() != nil {
			return platform.SearchPage{}, ctx.Err()
		}
		channels.Data = nil
	}
	// Channels lead, then videos, the way Twitch's mixed results do.
	for _, c := range channels.Data {
		if p.allowed(c.Host) && channelRef.MatchString(c.Name) {
			result.Results = append(result.Results, platform.Result{Channel: &platform.ChannelCard{
				ID: channelID(c), Platform: platformID, Name: platform.First(c.DisplayName, c.Name), Thumbnail: avatarURL(p.base(c.Host), c.Avatars),
			}})
		}
	}
	for _, item := range videos.Data {
		if item.UUID != "" && videoRef.MatchString(item.UUID) && p.allowed(item.Channel.Host) {
			listed := p.video(item, item.Channel.Host)
			result.Results = append(result.Results, platform.Result{Video: &listed})
		}
	}
	result.HasMore = float64(videoStart+len(videos.Data)) < videos.Total
	return result, nil
}

func (p *Platform) LiveStream(ctx context.Context, channelID string) (platform.LiveStream, error) {
	name, host, ok := splitChannel(channelID)
	if !ok || !p.allowed(host) {
		return platform.LiveStream{}, apierr.InvalidInput("Provide a channel of a supported PeerTube instance.")
	}
	var listing struct {
		Data []video `json:"data"`
	}
	query := url.Values{"isLive": {"true"}, "count": {"1"}, "sort": {"-publishedAt"}}
	if err := p.get(ctx, p.channelURL(name, host)+"/videos?"+query.Encode(), &listing); err != nil {
		return platform.LiveStream{}, err
	}
	for _, item := range listing.Data {
		if item.IsLive && item.UUID != "" {
			return platform.LiveStream{Live: true, VideoID: host + ":" + item.UUID, Title: item.Name, Viewers: firstNonZero(item.Viewers, item.Views), Thumbnail: absolute(p.base(host), platform.First(item.PreviewPath, item.ThumbnailPath))}, nil
		}
	}
	return platform.LiveStream{}, nil
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (p *Platform) LiveThumbnail(stream platform.LiveStream) string { return stream.Thumbnail }

// EndLive turns the channel's finished stream into an ordinary video, as on YouTube: the caller clears the
// live flag of the channel's stream videos and expires their metadata so the next opening fetches the
// final duration and date. A PeerTube stream is stored under its final id: there is no stub video.
func (p *Platform) EndLive(channelID string) platform.LiveEnd {
	return platform.LiveEnd{ChannelIDs: p.ChannelIDAliases(channelID)}
}

func (p *Platform) CommentsMode() string { return platform.CommentsList }

func (p *Platform) Comments(ctx context.Context, videoID, _ string) ([]platform.Comment, error) {
	host, uuid, ok := splitVideo(videoID)
	if !ok || !p.allowed(host) {
		return nil, apierr.InvalidInput("Provide a video of a supported PeerTube instance.")
	}
	var threads struct {
		Data []struct {
			ID        float64 `json:"id"`
			Text      string  `json:"text"`
			CreatedAt string  `json:"createdAt"`
			IsDeleted bool    `json:"isDeleted"`
			Account   struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"account"`
		} `json:"data"`
	}
	query := url.Values{"count": {strconv.Itoa(commentLimit)}, "sort": {"-createdAt"}}
	if err := p.get(ctx, p.videoURL(host, uuid)+"/comment-threads?"+query.Encode(), &threads); err != nil {
		return nil, err
	}
	comments := make([]platform.Comment, 0, len(threads.Data))
	for _, thread := range threads.Data {
		if thread.IsDeleted {
			continue
		}
		comment := platform.Comment{ID: strconv.FormatFloat(thread.ID, 'f', -1, 64), Author: platform.First(thread.Account.DisplayName, thread.Account.Name), Text: plainText(thread.Text)}
		if created, err := time.Parse(time.RFC3339Nano, thread.CreatedAt); err == nil {
			comment.PublishedAt = created.UTC().Format(time.RFC3339)
		}
		comments = append(comments, comment)
	}
	return comments, nil
}

// plainText turns the HTML of a comment into plain text: paragraphs and line breaks become newlines,
// every other tag disappears and entities are decoded.
func plainText(markup string) string {
	text := htmlLineBreaks.ReplaceAllString(markup, "\n")
	text = htmlTags.ReplaceAllString(text, "")
	return strings.TrimSpace(html.UnescapeString(text))
}
