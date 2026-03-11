package twitch

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// connector is the cookie session of a Twitch account: the auth-token cookie is an OAuth token the public
// web client uses.
type connector struct{ p *Platform }

var _ platform.Connector = connector{}

func (connector) NormalizeCookies(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > platform.CookieLimit || !utf8.ValidString(raw) || strings.ContainsRune(raw, '\x00') {
		return "", apierr.InvalidInput("Import a Netscape cookies.txt file no larger than 256 KiB.")
	}
	raw = strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	header, authenticated, expiredAuth := false, false, false
	var normalized strings.Builder
	normalized.WriteString("# Netscape HTTP Cookie File\n")
	now := time.Now().Unix()
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !header {
			if line != "# Netscape HTTP Cookie File" && line != "# HTTP Cookie File" {
				return "", apierr.InvalidInput("The cookie file must use Netscape cookies.txt format.")
			}
			header = true
			continue
		}
		httpOnly := strings.HasPrefix(line, "#HttpOnly_")
		if strings.HasPrefix(line, "#") && !httpOnly {
			continue
		}
		if httpOnly {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return "", apierr.InvalidInput("The cookie file contains an invalid Netscape cookie row.")
		}
		domain := strings.ToLower(strings.TrimPrefix(fields[0], "."))
		if domain != "twitch.tv" && !strings.HasSuffix(domain, ".twitch.tv") {
			continue
		}
		if (fields[1] != "TRUE" && fields[1] != "FALSE") || (fields[3] != "TRUE" && fields[3] != "FALSE") || !strings.HasPrefix(fields[2], "/") || fields[5] == "" || strings.ContainsAny(line, "\r\n") {
			return "", apierr.InvalidInput("The cookie file contains an invalid Twitch cookie row.")
		}
		expires, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil || expires < 0 {
			return "", apierr.InvalidInput("The cookie file contains an invalid expiry date.")
		}
		auth := fields[5] == "auth-token" && fields[6] != ""
		if expires != 0 && expires <= now {
			expiredAuth = expiredAuth || auth
			continue
		}
		authenticated = authenticated || auth
		if httpOnly {
			normalized.WriteString("#HttpOnly_")
		}
		normalized.WriteString(line)
		normalized.WriteByte('\n')
	}
	if !header {
		return "", apierr.InvalidInput("The cookie file must use Netscape cookies.txt format.")
	}
	if !authenticated {
		if expiredAuth {
			return "", apierr.SessionInvalid()
		}
		return "", apierr.InvalidInput("The cookie file does not contain a Twitch auth-token cookie.")
	}
	return normalized.String(), nil
}

func (c connector) Validate(ctx context.Context, cookies string) error {
	_, err := c.p.identity(ctx, cookies)
	return err
}

func (c connector) Feed(ctx context.Context, cookies, kind string, page, limit int) (platform.Feed, error) {
	return c.p.feed(ctx, cookies, kind, page, limit)
}

func (connector) MarkWatched(context.Context, string, string) error {
	return apierr.InvalidInput("Twitch watch-history updates are not supported.")
}

func authToken(cookies string) (string, error) {
	for _, line := range strings.Split(cookies, "\n") {
		line = strings.TrimPrefix(line, "#HttpOnly_")
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) == 7 && fields[5] == "auth-token" && fields[6] != "" {
			return fields[6], nil
		}
	}
	return "", apierr.SessionInvalid()
}

type identity struct {
	ClientID string `json:"client_id"`
	UserID   string `json:"user_id"`
}

func (p *Platform) identity(ctx context.Context, cookies string) (identity, error) {
	token, err := authToken(cookies)
	if err != nil {
		return identity{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.validateURL, nil)
	if err != nil {
		return identity{}, apierr.RequestFailed().WithCause(err)
	}
	request.Header.Set("Authorization", "OAuth "+token)
	var who identity
	if err := p.readJSON(request, &who); err != nil {
		return identity{}, err
	}
	if who.ClientID == "" || who.UserID == "" {
		return identity{}, apierr.SessionInvalid()
	}
	return who, nil
}

func (p *Platform) api(ctx context.Context, who identity, token, path string, query url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Client-Id", who.ClientID)
	return p.readJSON(request, target)
}

type stream struct {
	UserID       string   `json:"user_id"`
	UserLogin    string   `json:"user_login"`
	UserName     string   `json:"user_name"`
	Title        string   `json:"title"`
	GameName     string   `json:"game_name"`
	ThumbnailURL string   `json:"thumbnail_url"`
	StartedAt    string   `json:"started_at"`
	ViewerCount  float64  `json:"viewer_count"`
	Language     string   `json:"language"`
	Tags         []string `json:"tags"`
}

func (s stream) video() platform.Video {
	published := ""
	if started, err := time.Parse(time.RFC3339, s.StartedAt); err == nil {
		published = started.UTC().Format(time.RFC3339)
	}
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	thumbnail := strings.NewReplacer("{width}", "1280", "{height}", "720").Replace(s.ThumbnailURL)
	thumbnails := []string{}
	if thumbnail != "" {
		thumbnails = append(thumbnails, thumbnail)
	}
	title := platform.First(s.Title, s.UserName, s.UserLogin)
	return platform.Video{
		ID: s.UserLogin, Platform: ID, Source: ID,
		Title: title, OriginalTitle: title, OriginalTitleVerified: true,
		OriginalLanguage: s.Language, ChannelID: s.UserLogin, ChannelName: platform.First(s.UserName, s.UserLogin),
		Thumbnail: thumbnail, Thumbnails: thumbnails, Description: s.GameName, Tags: tags,
		Published: published, ViewCount: s.ViewerCount,
	}
}

// feed lists the live channels the account follows.
func (p *Platform) feed(ctx context.Context, cookies, kind string, page, limit int) (platform.Feed, error) {
	if kind != "home" {
		return platform.Feed{}, apierr.InvalidInput("Twitch provides a followed live-channel home feed.")
	}
	start, end, err := platform.PageRange(page, limit)
	if err != nil {
		return platform.Feed{}, err
	}
	who, err := p.identity(ctx, cookies)
	if err != nil {
		return platform.Feed{}, err
	}
	token, _ := authToken(cookies)
	streams := make([]stream, 0, min(end, 100))
	cursor := ""
	for len(streams) < end {
		query := url.Values{"user_id": {who.UserID}, "first": {strconv.Itoa(min(100, end-len(streams)))}}
		if cursor != "" {
			query.Set("after", cursor)
		}
		var response struct {
			Data       []stream `json:"data"`
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := p.api(ctx, who, token, "/streams/followed", query, &response); err != nil {
			return platform.Feed{}, err
		}
		streams = append(streams, response.Data...)
		if len(response.Data) == 0 || response.Pagination.Cursor == "" {
			break
		}
		if response.Pagination.Cursor == cursor {
			return platform.Feed{}, apierr.RequestFailed().WithCause(errors.New("twitch repeated its pagination cursor"))
		}
		cursor = response.Pagination.Cursor
	}
	result := platform.Feed{Videos: []platform.Video{}, Page: page}
	if len(streams) >= start {
		selected := streams[start-1:]
		result.HasMore = len(selected) > limit
		selected = selected[:min(limit, len(selected))]
		for _, s := range selected {
			if !loginPattern.MatchString(s.UserLogin) {
				return platform.Feed{}, apierr.RequestFailed().WithCause(errors.New("twitch listed an invalid login"))
			}
			result.Videos = append(result.Videos, s.video())
		}
	}
	return result, nil
}
