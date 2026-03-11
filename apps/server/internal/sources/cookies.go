package sources

import (
	"os"
	"slices"
	"sort"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Open prepares a request that runs as the account on the platform: the session carries the account, a new
// private cookie jar for the downloader and the decrypted cookies for platforms that call their own API
// with them. The cleanup removes the jar, after storing back what the platform rotated in it (see
// CookieFile). It does not apply the account's activation; callers do that with RequireEnabled.
func (s *Service) Open(userID, platformID string) (platform.Session, func(), error) {
	path, cookies, cleanup, err := s.open(userID, platformID)
	if err != nil {
		return platform.Session{}, nil, err
	}
	return platform.Session{UserID: userID, CookieFile: path, Cookies: cookies}, cleanup, nil
}

// CookieFile returns a new decrypted, private temporary jar for one process. yt-dlp rewrites its jar when
// the platform rotates session cookies; the cleanup function stores a changed jar back (re-encrypted)
// before removing the file, so the connection does not silently expire.
func (s *Service) CookieFile(userID, platformID string) (string, func(), error) {
	path, _, cleanup, err := s.open(userID, platformID)
	return path, cleanup, err
}

func (s *Service) open(userID, platformID string) (path, cookies string, cleanup func(), err error) {
	connector, record, cookies, err := s.storedConnection(userID, platformID)
	if err != nil {
		return "", "", nil, err
	}
	path, remove, err := s.runner.CookieFile(cookies)
	if err != nil {
		return "", "", nil, apierr.RequestFailed().WithCause(err)
	}
	return path, cookies, func() {
		s.persistRefreshedCookies(connector, record.Id, userID, platformID, cookies, path)
		remove()
	}, nil
}

// persistRefreshedCookies saves the jar a downloader left in path when it still is a valid session and
// differs from the stored one. The write only applies to the connection the file was made from: a
// connection replaced or removed meanwhile is never overwritten with an older session. Rotating the
// ciphertext does not change SessionVersion, so playback in progress keeps working.
func (s *Service) persistRefreshedCookies(connector platform.Connector, connectionID, userID, platformID, original, path string) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 2*platform.CookieLimit {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	refreshed, err := connector.NormalizeCookies(string(data))
	if err != nil || sameCookieRows(refreshed, original) {
		return
	}
	secret, err := s.encrypt(userID, platformID, refreshed)
	if err != nil {
		return
	}
	err = s.app.RunInTransaction(func(tx core.App) error {
		record, err := tx.FindRecordById(ConnectionsCollection, connectionID)
		if err != nil {
			return nil
		}
		record.Set("encrypted_cookies", secret)
		return tx.Save(record)
	})
	if err != nil {
		logging.Of(s.app).Warn("Failed to store a refreshed source session", "platform", platformID)
	}
}

// sameCookieRows compares two Netscape cookie files by their rows, whatever order a downloader wrote them in.
func sameCookieRows(a, b string) bool {
	rows := func(raw string) []string {
		var result []string
		for line := range strings.SplitSeq(raw, "\n") {
			if line = strings.TrimSpace(line); line != "" && (!strings.HasPrefix(line, "#") || strings.HasPrefix(line, "#HttpOnly_")) {
				result = append(result, line)
			}
		}
		sort.Strings(result)
		return result
	}
	return slices.Equal(rows(a), rows(b))
}
