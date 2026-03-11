package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// connector is the session of a YouTube account, imported as a Netscape cookie file.
type connector struct{ p *Platform }

// NormalizeCookies keeps only YouTube-domain rows. Authentication is checked with YouTube itself (see
// Validate); merely finding a recognizable cookie name is not proof.
func (connector) NormalizeCookies(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > platform.CookieLimit || !utf8.ValidString(raw) || strings.ContainsRune(raw, '\x00') {
		return "", apierr.InvalidInput("Import a Netscape cookies.txt file no larger than 256 KiB.")
	}
	raw = strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	header := false
	hasAuth, hasLoginInfo, expiredAuth := false, false, false
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
		if domain != "youtube.com" && !strings.HasSuffix(domain, ".youtube.com") {
			continue
		}
		if (fields[1] != "TRUE" && fields[1] != "FALSE") || (fields[3] != "TRUE" && fields[3] != "FALSE") || !strings.HasPrefix(fields[2], "/") || fields[5] == "" || strings.ContainsAny(line, "\r\n") {
			return "", apierr.InvalidInput("The cookie file contains an invalid YouTube cookie row.")
		}
		expires, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil || expires < 0 {
			return "", apierr.InvalidInput("The cookie file contains an invalid expiry date.")
		}
		auth := false
		switch fields[5] {
		case "SID", "SAPISID", "__Secure-1PSID", "__Secure-3PSID", "__Secure-1PAPISID", "__Secure-3PAPISID", "LOGIN_INFO":
			auth = fields[6] != ""
		}
		if expires != 0 && expires <= now {
			expiredAuth = expiredAuth || auth
			continue
		}
		hasAuth = hasAuth || (auth && (fields[5] == "SAPISID" || fields[5] == "__Secure-1PAPISID" || fields[5] == "__Secure-3PAPISID"))
		hasLoginInfo = hasLoginInfo || (auth && fields[5] == "LOGIN_INFO")
		if httpOnly {
			normalized.WriteString("#HttpOnly_")
		}
		normalized.WriteString(line)
		normalized.WriteByte('\n')
	}
	if !header {
		return "", apierr.InvalidInput("The cookie file must use Netscape cookies.txt format.")
	}
	if !hasAuth || !hasLoginInfo {
		if expiredAuth {
			return "", apierr.SessionInvalid()
		}
		return "", apierr.InvalidInput("The cookie file does not contain YouTube authentication cookies.")
	}
	return normalized.String(), nil
}

// downloaderError classifies what yt-dlp said while running as a connected account, whether or not it
// failed: a refused session, or a history write that did not happen. It never retains or returns the text.
func downloaderError(stderr string) error {
	message := strings.ToLower(stderr)
	for _, marker := range []string{
		"sign in", "log in", "not logged in", "login required", "login_required",
		"authentication required", "requires authentication", "cookies are no longer valid",
		"cookies have expired", "cookies expired", "session has expired", "session expired",
		"cookie is no longer valid", "account cookies are no longer valid",
	} {
		if strings.Contains(message, marker) {
			return apierr.SessionInvalid()
		}
	}
	for _, marker := range []string{
		"unable to mark watched", "unable to mark fully watched", "unable to mark as watched",
		"failed to mark", "unable to update watch history",
		"playlists that require authentication may not extract correctly",
	} {
		if strings.Contains(message, marker) {
			return apierr.RequestFailed()
		}
	}
	return nil
}

// withSession runs yt-dlp with a private cookie jar made for this one process and removed afterwards.
func (c connector) withSession(ctx context.Context, cookies string, run func(cookieFile string) error) error {
	cookieFile, cleanup, err := c.p.runner.CookieFile(cookies)
	if err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	defer cleanup()
	return fail(ctx, run(cookieFile), true)
}

// Validate asks YouTube whether the session works. The login-required subscriptions wrapper, not a
// non-empty item list, proves it: a valid new account can have zero subscriptions.
func (c connector) Validate(ctx context.Context, cookies string) error {
	return c.withSession(ctx, cookies, func(cookieFile string) error {
		run := spec([]string{"--dump-single-json", "--flat-playlist", "--skip-download", "--no-mark-watched", "--playlist-end", "1", "https://www.youtube.com/feed/subscriptions"}, cookieFile)
		run.Stderr = downloaderError
		return ytdlp.Decode(ctx, c.p.runner, run, func(decoder *json.Decoder) error {
			var playlist struct {
				Type         string          `json:"_type"`
				ID           string          `json:"id"`
				Extractor    string          `json:"extractor"`
				ExtractorKey string          `json:"extractor_key"`
				Entries      json.RawMessage `json:"entries"`
			}
			if err := decoder.Decode(&playlist); err != nil {
				return err
			}
			if playlist.Type != "playlist" || playlist.ID != "subscriptions" || (playlist.ExtractorKey != "YoutubeTab" && playlist.Extractor != "youtube:tab") || len(playlist.Entries) == 0 || string(playlist.Entries) == "null" {
				return errors.New("missing authenticated playlist wrapper")
			}
			var entries []json.RawMessage
			if err := json.Unmarshal(playlist.Entries, &entries); err != nil {
				return err
			}
			for _, raw := range entries {
				var entry ytEntry
				if err := json.Unmarshal(raw, &entry); err != nil || !videoIDPattern.MatchString(entry.ID) {
					return errors.New("invalid authenticated playlist entry")
				}
			}
			return nil
		})
	})
}

// Feed lists one page of the account's feed. A plain NDJSON feed cannot prove that an empty result was
// authenticated, so the session is validated first, and the feed is read with a fresh cookie jar.
func (c connector) Feed(ctx context.Context, cookies, kind string, page, limit int) (platform.Feed, error) {
	var address string
	switch kind {
	case "home":
		address = "https://www.youtube.com/feed/recommended"
	case "subscriptions":
		address = "https://www.youtube.com/feed/subscriptions"
	case "history":
		address = "https://www.youtube.com/feed/history"
	default:
		return platform.Feed{}, apierr.InvalidInput("Choose a home, subscriptions or history feed.")
	}
	start, end, err := platform.PageRange(page, limit)
	if err != nil {
		return platform.Feed{}, err
	}
	if err = c.Validate(ctx, cookies); err != nil {
		return platform.Feed{}, err
	}
	result := platform.Feed{Videos: []platform.Video{}, Page: page}
	err = c.withSession(ctx, cookies, func(cookieFile string) error {
		run := spec([]string{"--dump-json", "--flat-playlist", "--skip-download", "--no-mark-watched", "--playlist-start", strconv.Itoa(start), "--playlist-end", strconv.Itoa(end), address}, cookieFile)
		run.Stderr = downloaderError
		run.MaxOutput = ytdlp.JSONLimit
		return ytdlp.Each(ctx, c.p.runner, run, func(entry ytEntry) error {
			if !videoIDPattern.MatchString(entry.ID) || len(result.Videos) > limit {
				return errors.New("invalid source feed entry")
			}
			video := entry.video()
			video.Source = platformID
			result.Videos = append(result.Videos, video)
			return nil
		})
	})
	if err != nil {
		return platform.Feed{}, err
	}
	if len(result.Videos) > limit {
		result.HasMore = true
		result.Videos = result.Videos[:limit]
	}
	return result, nil
}

// MarkWatched records the whole video as watched on the account. This is the only operation allowed to
// override the runner's default --no-mark-watched.
func (c connector) MarkWatched(ctx context.Context, cookies, id string) error {
	if !videoIDPattern.MatchString(id) {
		return apierr.InvalidInput("Provide a valid video ID.")
	}
	return c.withSession(ctx, cookies, func(cookieFile string) error {
		run := spec([]string{"--dump-json", "--no-playlist", "--skip-download", "--mark-watched", "--simulate", watchURL(id)}, cookieFile)
		run.Stderr = downloaderError
		return ytdlp.Decode(ctx, c.p.runner, run, func(decoder *json.Decoder) error {
			var video ytEntry
			if err := decoder.Decode(&video); err != nil {
				return err
			}
			if video.ID != id {
				return fmt.Errorf("unexpected watched video")
			}
			return nil
		})
	})
}
