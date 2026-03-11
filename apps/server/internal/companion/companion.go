// Package companion pairs a browser extension to a profile and takes in what the extension reads from the
// YouTube account the browser is signed into: the subscriptions and the watch history, one way, into Viyu.
// The extension never hands Viyu a cookie; it authenticates with a token of its own, made when the user
// pairs it from a signed-in session and revocable at any time.
//
// Concurrency: the pending pairing codes live in memory behind one mutex. Everything else is stored; a sync
// reads and writes in one transaction (RunInTransaction serializes writers), so two syncs of one profile
// never interleave.
package companion

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
)

const (
	devicesCollection = "companion_devices"
	profilesName      = "profiles"

	// PairingTTL is how long a pairing code can be exchanged for a device token.
	PairingTTL = 10 * time.Minute
	// maxPendingPerUser bounds the codes one account keeps waiting: the oldest is dropped past it, so a
	// signed-in client cannot grow the memory of the server.
	maxPendingPerUser = 20

	maxNameRunes = 80
	defaultName  = "Browser extension"

	// tokenBytes is the entropy of a pairing code and of a device token.
	tokenBytes = 32
)

// Service pairs devices and applies their syncs. It is safe for concurrent use.
type Service struct {
	app           core.App
	store         *catalog.Store
	subscriptions *subscriptions.Service
	sources       *sources.Service
	now           func() time.Time

	mu      sync.Mutex
	pending map[[sha256.Size]byte]pairing
}

// pairing is a code waiting to be exchanged: the account and profile it was made for.
type pairing struct {
	userID, profileID string
	expires           time.Time
}

// Device is a paired browser as the account sees it. The token and its hash are never part of it.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Profile     string `json:"profile"`
	ProfileName string `json:"profileName"`
	Created     string `json:"created"`
	// LastSyncAt is empty until the device synced.
	LastSyncAt string `json:"lastSyncAt"`

	userID string
}

// UserID is the account the device syncs into.
func (d *Device) UserID() string { return d.userID }

// New returns the service of app.
func New(app core.App, store *catalog.Store, subscriptions *subscriptions.Service, sources *sources.Service) *Service {
	return &Service{
		app:           app,
		store:         store,
		subscriptions: subscriptions,
		sources:       sources,
		now:           time.Now,
		pending:       map[[sha256.Size]byte]pairing{},
	}
}

func invalidCode() *apierr.Error {
	return apierr.Invalid("This pairing code is invalid or has expired.")
}

// randomToken returns tokenBytes of randomness as base64url without padding.
func randomToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashToken is the form in which a device token is stored: only a leaked database that also knows the
// token could act as the device, and the token has the entropy to make the hash unguessable.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreatePairing makes a single-use code for a profile of the account. A profile of someone else is
// answered like a missing one. The code lives in memory: a restart drops the codes still waiting.
func (s *Service) CreatePairing(userID, profileID string) (string, time.Time, error) {
	if _, err := s.subscriptions.OwnedProfile(userID, profileID); err != nil {
		return "", time.Time{}, err
	}
	code, err := randomToken()
	if err != nil {
		return "", time.Time{}, apierr.Internal("Failed to create a pairing code", err)
	}
	now := s.now()
	expires := now.Add(PairingTTL)

	s.mu.Lock()
	defer s.mu.Unlock()
	var oldest [sha256.Size]byte
	var oldestExpires time.Time
	waiting := 0
	for key, p := range s.pending {
		switch {
		case !now.Before(p.expires):
			delete(s.pending, key)
		case p.userID == userID:
			waiting++
			if oldestExpires.IsZero() || p.expires.Before(oldestExpires) {
				oldest, oldestExpires = key, p.expires
			}
		}
	}
	if waiting >= maxPendingPerUser {
		delete(s.pending, oldest)
	}
	s.pending[sha256.Sum256([]byte(code))] = pairing{userID: userID, profileID: profileID, expires: expires}
	return code, expires, nil
}

// Pair exchanges a code for a device token. The code is spent whatever happens next, so a wrong guess
// never leaves a usable code behind. The token is returned once: only its hash is stored.
func (s *Service) Pair(code, name string) (string, *Device, error) {
	key := sha256.Sum256([]byte(code))
	s.mu.Lock()
	p, found := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if !found || !s.now().Before(p.expires) {
		return "", nil, invalidCode()
	}

	profile, err := s.app.FindRecordById(profilesName, p.profileID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && profile.GetString("user") != p.userID) {
		return "", nil, invalidCode()
	}
	if err != nil {
		return "", nil, apierr.Internal("Failed to pair the device", err)
	}
	token, err := randomToken()
	if err != nil {
		return "", nil, apierr.Internal("Failed to pair the device", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultName
	}
	if runes := []rune(name); len(runes) > maxNameRunes {
		name = strings.TrimSpace(string(runes[:maxNameRunes]))
	}

	collection, err := s.app.FindCollectionByNameOrId(devicesCollection)
	if err != nil {
		return "", nil, apierr.Internal("Failed to pair the device", err)
	}
	record := core.NewRecord(collection)
	record.Set("name", name)
	record.Set("token_hash", hashToken(token))
	record.Set("user", p.userID)
	record.Set("profile", p.profileID)
	if err := s.app.Save(record); err != nil {
		return "", nil, apierr.Internal("Failed to pair the device", err)
	}
	return token, deviceOf(record, profile.GetString("name")), nil
}

// Authenticate returns the device a token belongs to: Unauthorized for a missing, unknown or revoked one.
func (s *Service) Authenticate(token string) (*Device, error) {
	if token == "" {
		return nil, apierr.CompanionUnauthorized()
	}
	record, err := s.app.FindFirstRecordByFilter(devicesCollection, "token_hash = {:hash}", dbx.Params{"hash": hashToken(token)})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.CompanionUnauthorized()
	}
	if err != nil {
		return nil, apierr.Internal("Failed to read the device", err)
	}
	profile, err := s.app.FindRecordById(profilesName, record.GetString("profile"))
	if err != nil {
		return nil, apierr.Internal("Failed to read the device", err)
	}
	return deviceOf(record, profile.GetString("name")), nil
}

// Devices lists the devices paired to the account, newest first.
func (s *Service) Devices(userID string) ([]*Device, error) {
	records, err := s.app.FindRecordsByFilter(devicesCollection, "user = {:user}", "-created,-id", 0, 0, dbx.Params{"user": userID})
	if err != nil {
		return nil, apierr.Internal("Failed to read the devices", err)
	}
	profileIDs := make([]string, 0, len(records))
	for _, record := range records {
		profileIDs = append(profileIDs, record.GetString("profile"))
	}
	names := map[string]string{}
	if len(profileIDs) > 0 {
		profiles, err := s.app.FindRecordsByIds(profilesName, profileIDs)
		if err != nil {
			return nil, apierr.Internal("Failed to read the devices", err)
		}
		for _, profile := range profiles {
			names[profile.Id] = profile.GetString("name")
		}
	}
	devices := make([]*Device, 0, len(records))
	for _, record := range records {
		devices = append(devices, deviceOf(record, names[record.GetString("profile")]))
	}
	return devices, nil
}

// Revoke unpairs a device of the account: its token stops working at once. A device of another account is
// answered like a missing one.
func (s *Service) Revoke(userID, deviceID string) error {
	record, err := s.app.FindRecordById(devicesCollection, deviceID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && record.GetString("user") != userID) {
		return apierr.NotFound("Device")
	}
	if err != nil {
		return apierr.Internal("Failed to read the device", err)
	}
	if err := s.app.Delete(record); err != nil {
		return apierr.Internal("Failed to remove the device", err)
	}
	return nil
}

func deviceOf(record *core.Record, profileName string) *Device {
	device := &Device{
		ID:          record.Id,
		Name:        record.GetString("name"),
		Profile:     record.GetString("profile"),
		ProfileName: profileName,
		Created:     record.GetDateTime("created").Time().UTC().Format(time.RFC3339),
		userID:      record.GetString("user"),
	}
	if synced := record.GetDateTime("last_sync_at"); !synced.IsZero() {
		device.LastSyncAt = synced.Time().UTC().Format(time.RFC3339)
	}
	return device
}

// touch records that the device synced, inside the transaction of the sync: a device revoked meanwhile
// fails the sync, which then writes nothing.
func (s *Service) touch(tx core.App, deviceID string) error {
	record, err := tx.FindRecordById(devicesCollection, deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return apierr.CompanionUnauthorized()
	}
	if err != nil {
		return err
	}
	record.Set("last_sync_at", s.now().UTC())
	return tx.Save(record)
}

// internalError turns err into an *apierr.Error: itself when it is one, an internal failure otherwise.
func internalError(message string, err error) error {
	var safe *apierr.Error
	if errors.As(err, &safe) {
		return safe
	}
	return apierr.Internal(message, err)
}
