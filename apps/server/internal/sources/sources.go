// Package sources owns what an account does with the platforms: which ones it uses (activation) and the
// authenticated session it may import for one (connection). A session is a cookie file the platform's
// Connector normalizes and validates; it is stored encrypted, bound to the account and the platform, and
// only ever decrypted into a private temporary jar for the duration of one request.
package sources

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// ConnectionsCollection holds one encrypted session per account and platform.
const ConnectionsCollection = "source_connections"

// maxLabelRunes bounds the name an account gives a connection.
const maxLabelRunes = 120

// Source is the public catalog/connection descriptor. Credentials are never part of this type, including
// when a connection is checked or replaced.
type Source struct {
	platform.Descriptor
	Connected bool `json:"connected"`
	// Enabled is the account's activation of the source, independent of whether it is connected.
	Enabled      bool   `json:"enabled"`
	ConnectionID string `json:"connectionId,omitempty"`
	Label        string `json:"label,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

// Options configures a Service.
type Options struct {
	// KeyFile is the absolute path of the master key. Empty reads $VIYU_SOURCE_KEY_FILE, then falls back to
	// <data dir>/source_connections.key.
	KeyFile string
}

// Service stores and uses the connected sessions of the accounts. It is safe for concurrent use.
type Service struct {
	app       core.App
	platforms *platform.Registry
	runner    *ytdlp.Runner
	cipher    cipher.AEAD
}

// New builds the service: it empties the runner's cookie directory (jars left by a crashed process never
// linger) and loads, or creates, the master key.
func New(app core.App, platforms *platform.Registry, runner *ytdlp.Runner, opts Options) (*Service, error) {
	if err := runner.ResetCookieDir(); err != nil {
		return nil, err
	}
	keyFile := opts.KeyFile
	if keyFile == "" {
		keyFile = os.Getenv(keyFileEnv)
	}
	key, err := masterKey(app.DataDir(), keyFile)
	if err != nil {
		return nil, err
	}
	aead, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	return &Service{app: app, platforms: platforms, runner: runner, cipher: aead}, nil
}

func unknownSource() error { return apierr.InvalidInput("Unknown source.") }

// connector returns the platform's session capability: Unknown source for an id nobody registered, and
// NotConnectable for a platform with no account to import.
func (s *Service) connector(platformID string) (platform.Connector, error) {
	p, ok := s.platforms.Get(platformID)
	if !ok {
		return nil, unknownSource()
	}
	connector := p.Connector()
	if connector == nil {
		return nil, apierr.NotConnectable()
	}
	return connector, nil
}

func isNotConnected(err error) bool {
	var safe *apierr.Error
	return errors.As(err, &safe) && safe.Code == apierr.CodeNotConnected
}

// connection returns the account's stored session record: NotConnected when there is none.
func (s *Service) connection(userID, platformID string) (*core.Record, error) {
	if _, ok := s.platforms.Get(platformID); !ok {
		return nil, unknownSource()
	}
	record, err := s.app.FindFirstRecordByFilter(ConnectionsCollection, "user = {:user} && platform = {:platform}", dbx.Params{"user": userID, "platform": platformID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotConnected()
	}
	if err != nil {
		return nil, apierr.RequestFailed().WithCause(err)
	}
	return record, nil
}

// storedConnection returns the connector, the connection record and its decrypted, normalized cookies.
func (s *Service) storedConnection(userID, platformID string) (platform.Connector, *core.Record, string, error) {
	connector, err := s.connector(platformID)
	if err != nil {
		return nil, nil, "", err
	}
	record, err := s.connection(userID, platformID)
	if err != nil {
		return nil, nil, "", err
	}
	cookies, err := s.decrypt(record.GetString("encrypted_cookies"), userID, platformID)
	if err != nil {
		return nil, nil, "", err
	}
	cookies, err = connector.NormalizeCookies(cookies)
	if err != nil {
		return nil, nil, "", apierr.From(err)
	}
	return connector, record, cookies, nil
}

// StoredCookies returns the account's decrypted, normalized session cookies for the platform.
func (s *Service) StoredCookies(userID, platformID string) (string, error) {
	_, _, cookies, err := s.storedConnection(userID, platformID)
	return cookies, err
}

// SessionVersion binds short-lived playback sessions to this exact connection. Reconnecting always creates
// a new connection record (even when the same cookies are imported again), while the session refreshes
// stored by the cookie jar cleanup keep the record, so they do not end playback.
func (s *Service) SessionVersion(userID, platformID string) (string, error) {
	record, err := s.connection(userID, platformID)
	if err != nil {
		return "", err
	}
	version := sha256.Sum256([]byte(record.Id + "\x00" + record.GetString("created")))
	return hex.EncodeToString(version[:]), nil
}

// describe presents one platform for an account: its activation and, when there is a record, its connection.
func (s *Service) describe(p platform.Platform, record *core.Record, enabled bool) Source {
	source := Source{Descriptor: p.Descriptor(), Enabled: enabled}
	if record != nil {
		source.Connected = true
		source.ConnectionID = record.Id
		source.Label = record.GetString("label")
		source.UpdatedAt = record.GetDateTime("updated").String()
	}
	return source
}

// List describes every platform, in registry order, as the account sees it.
func (s *Service) List(userID string) ([]Source, error) {
	active, err := s.Activations(userID)
	if err != nil {
		return nil, err
	}
	records, err := s.app.FindRecordsByFilter(ConnectionsCollection, "user = {:user}", "", 0, 0, dbx.Params{"user": userID})
	if err != nil {
		return nil, apierr.RequestFailed().WithCause(err)
	}
	connections := make(map[string]*core.Record, len(records))
	for _, record := range records {
		connections[record.GetString("platform")] = record
	}
	result := make([]Source, 0, len(s.platforms.All()))
	for _, p := range s.platforms.All() {
		result = append(result, s.describe(p, connections[p.ID()], active[p.ID()]))
	}
	return result, nil
}

// Current describes one platform as the account sees it: its activation and, when there is one, its connection.
func (s *Service) Current(userID, platformID string) (Source, error) {
	record, err := s.connection(userID, platformID)
	if err != nil && !isNotConnected(err) {
		return Source{}, err
	}
	return s.describeFor(userID, platformID, record)
}

func (s *Service) describeFor(userID, platformID string, record *core.Record) (Source, error) {
	p, ok := s.platforms.Get(platformID)
	if !ok {
		return Source{}, unknownSource()
	}
	active, err := s.Activations(userID)
	if err != nil {
		return Source{}, err
	}
	return s.describe(p, record, active[platformID]), nil
}

// Connect validates the imported cookies with the platform and stores them, replacing any earlier
// connection of the account with a new record.
func (s *Service) Connect(ctx context.Context, userID, platformID, rawCookies, label string) (Source, error) {
	connector, err := s.connector(platformID)
	if err != nil {
		return Source{}, err
	}
	if err = s.RequireEnabled(userID, platformID); err != nil {
		return Source{}, err
	}
	label = strings.TrimSpace(label)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > maxLabelRunes || strings.ContainsAny(label, "\r\n\x00") {
		return Source{}, apierr.InvalidInput("The connection label must be at most 120 characters on one line.")
	}
	cookies, err := connector.NormalizeCookies(rawCookies)
	if err != nil {
		return Source{}, apierr.From(err)
	}
	if err = connector.Validate(ctx, cookies); err != nil {
		return Source{}, apierr.From(err)
	}
	if ctx.Err() != nil {
		return Source{}, apierr.RequestFailed().WithCause(ctx.Err())
	}
	secret, err := s.encrypt(userID, platformID, cookies)
	if err != nil {
		return Source{}, err
	}
	var saved *core.Record
	err = s.app.RunInTransaction(func(tx core.App) error {
		// A reconnection is a new connection record: SessionVersion (and with it every playback session,
		// catalog and comment cache bound to the old connection) changes with the record.
		existing, err := tx.FindFirstRecordByFilter(ConnectionsCollection, "user = {:user} && platform = {:platform}", dbx.Params{"user": userID, "platform": platformID})
		if err == nil {
			if err := tx.Delete(existing); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		collection, err := tx.FindCollectionByNameOrId(ConnectionsCollection)
		if err != nil {
			return err
		}
		record := core.NewRecord(collection)
		record.Set("user", userID)
		record.Set("platform", platformID)
		record.Set("label", label)
		record.Set("encrypted_cookies", secret)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := tx.Save(record); err != nil {
			return err
		}
		saved = record
		return nil
	})
	if err != nil {
		return Source{}, apierr.RequestFailed().WithCause(err)
	}
	return s.describeFor(userID, platformID, saved)
}

// Check asks the platform whether the stored session still works.
func (s *Service) Check(ctx context.Context, userID, platformID string) (Source, error) {
	connector, err := s.connector(platformID)
	if err != nil {
		return Source{}, err
	}
	if err = s.RequireEnabled(userID, platformID); err != nil {
		return Source{}, err
	}
	cookies, err := s.StoredCookies(userID, platformID)
	if err != nil {
		return Source{}, err
	}
	if err = connector.Validate(ctx, cookies); err != nil {
		return Source{}, apierr.From(err)
	}
	record, err := s.connection(userID, platformID)
	if err != nil {
		return Source{}, err
	}
	return s.describeFor(userID, platformID, record)
}

// Disconnect drops the stored session; an account that has none is already disconnected.
func (s *Service) Disconnect(userID, platformID string) error {
	record, err := s.connection(userID, platformID)
	if isNotConnected(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.app.Delete(record); err != nil {
		return apierr.RequestFailed().WithCause(err)
	}
	return nil
}

// Feed lists one page of the account's personal feed on the platform.
func (s *Service) Feed(ctx context.Context, userID, platformID, kind string, page, limit int) (platform.Feed, error) {
	if _, _, err := platform.PageRange(page, limit); err != nil {
		return platform.Feed{}, err
	}
	connector, err := s.connector(platformID)
	if err != nil {
		return platform.Feed{}, err
	}
	if err = s.RequireEnabled(userID, platformID); err != nil {
		return platform.Feed{}, err
	}
	cookies, err := s.StoredCookies(userID, platformID)
	if err != nil {
		return platform.Feed{}, err
	}
	feed, err := connector.Feed(ctx, cookies, kind, page, limit)
	if err != nil {
		return platform.Feed{}, apierr.From(err)
	}
	return feed, nil
}

// minWatched is how long a video has to be played before it counts as watched (the whole video when it
// is shorter).
const minWatched = 30

// Watch records a watched video, given by its native id, on the account. Nothing reaches the platform
// unless the video really was played: a preloaded or barely started video is refused.
func (s *Service) Watch(ctx context.Context, userID, platformID, videoID string, played, duration float64) error {
	p, ok := s.platforms.Get(platformID)
	if !ok {
		return unknownSource()
	}
	if !p.ValidVideoID(videoID) || math.IsNaN(played) || math.IsInf(played, 0) || math.IsNaN(duration) || math.IsInf(duration, 0) || played < 0 || duration <= 0 {
		return apierr.InvalidInput("Provide a valid video and finite playback duration.")
	}
	if played < math.Min(minWatched, duration) {
		return apierr.InvalidInput(fmt.Sprintf("Play at least %d seconds, or the full shorter video, before marking it watched.", minWatched))
	}
	connector := p.Connector()
	if connector == nil {
		return apierr.NotConnectable()
	}
	if err := s.RequireEnabled(userID, platformID); err != nil {
		return err
	}
	cookies, err := s.StoredCookies(userID, platformID)
	if err != nil {
		return err
	}
	if err = connector.MarkWatched(ctx, cookies, videoID); err != nil {
		return apierr.From(err)
	}
	return nil
}
