package companion

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
)

// A pairing code is exchanged once, for the profile it was made for, and the token it yields is the only
// thing that authenticates the device.
func TestPairingCodeIsSingleUseAndGivesADeviceToken(t *testing.T) {
	f := newFixture(t)
	user := f.user(t, "owner@example.test")
	profile := f.profile(t, user, "Main")

	code, expires, err := f.svc.CreatePairing(user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	if expires != now.Add(10*time.Minute) {
		t.Fatalf("expires = %v", expires)
	}
	token, device, err := f.svc.Pair(code, "  Chrome on Linux ")
	if err != nil {
		t.Fatal(err)
	}
	if device.Name != "Chrome on Linux" || device.ProfileName != "Main" || device.Profile != profile.Id || device.LastSyncAt != "" {
		t.Fatalf("device = %+v", device)
	}
	if _, _, err := f.svc.Pair(code, "again"); err == nil {
		t.Fatal("a pairing code was accepted twice")
	} else {
		requireCode(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	}

	authenticated, err := f.svc.Authenticate(token)
	if err != nil || authenticated.ID != device.ID || authenticated.UserID() != user.Id {
		t.Fatalf("authenticate = %+v, %v", authenticated, err)
	}
	stored, err := f.app.FindRecordById("companion_devices", device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GetString("token_hash") == token || stored.GetString("token_hash") != hashToken(token) {
		t.Fatal("the token is stored as its hash, never as itself")
	}
}

func TestPairingCodeExpires(t *testing.T) {
	f := newFixture(t)
	user := f.user(t, "owner@example.test")
	profile := f.profile(t, user, "Main")

	late, _, err := f.svc.CreatePairing(user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.now = func() time.Time { return now.Add(PairingTTL) }
	if _, _, err := f.svc.Pair(late, ""); err == nil {
		t.Fatal("an expired code was accepted")
	} else {
		requireCode(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	}

	f.svc.now = func() time.Time { return now }
	fresh, _, err := f.svc.CreatePairing(user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.now = func() time.Time { return now.Add(PairingTTL - time.Second) }
	if _, device, err := f.svc.Pair(fresh, ""); err != nil || device.Name != "Browser extension" {
		t.Fatalf("a code just before its expiry must work: %+v %v", device, err)
	}
}

func TestPairingRefusesAProfileOfAnotherAccountAndAnUnknownCode(t *testing.T) {
	f := newFixture(t)
	owner := f.user(t, "owner@example.test")
	other := f.user(t, "other@example.test")
	profile := f.profile(t, owner, "Main")

	if _, _, err := f.svc.CreatePairing(other.Id, profile.Id); err == nil {
		t.Fatal("a pairing code was made for another account's profile")
	} else {
		requireCode(t, err, http.StatusNotFound, "NOT_FOUND")
	}
	if _, _, err := f.svc.CreatePairing(owner.Id, "missing"); err == nil {
		t.Fatal("a pairing code was made for a missing profile")
	} else {
		requireCode(t, err, http.StatusNotFound, "NOT_FOUND")
	}
	if _, _, err := f.svc.Pair("never-issued", ""); err == nil {
		t.Fatal("an unknown code was accepted")
	} else {
		requireCode(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	}
}

// A profile deleted between the code and its exchange leaves nothing to pair to.
func TestPairingFailsWhenTheProfileIsGone(t *testing.T) {
	f := newFixture(t)
	user := f.user(t, "owner@example.test")
	profile := f.profile(t, user, "Main")
	code, _, err := f.svc.CreatePairing(user.Id, profile.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.Delete(profile); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Pair(code, ""); err == nil {
		t.Fatal("paired to a deleted profile")
	} else {
		requireCode(t, err, http.StatusBadRequest, "INVALID_REQUEST")
	}
}

func TestPairedDeviceNameIsTrimmedAndLimited(t *testing.T) {
	f := newFixture(t)
	user := f.user(t, "owner@example.test")
	profile := f.profile(t, user, "Main")
	_, device := f.paired(t, user, profile, strings.Repeat("é", 120))
	if got := []rune(device.Name); len(got) != 80 {
		t.Fatalf("name has %d runes", len(got))
	}
	_, device = f.paired(t, user, profile, "   ")
	if device.Name != "Browser extension" {
		t.Fatalf("name = %q", device.Name)
	}
}

func TestAuthenticateRefusesAMissingUnknownOrRevokedToken(t *testing.T) {
	f := newFixture(t)
	user := f.user(t, "owner@example.test")
	profile := f.profile(t, user, "Main")
	token, device := f.paired(t, user, profile, "Chrome")

	for _, bad := range []string{"", "unknown", token + "x"} {
		if _, err := f.svc.Authenticate(bad); err == nil {
			t.Fatalf("token %q was accepted", bad)
		} else {
			requireCode(t, err, http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
		}
	}
	if err := f.svc.Revoke(user.Id, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(token); err == nil {
		t.Fatal("a revoked token was accepted")
	} else {
		requireCode(t, err, http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
	}
}

// The list is the account's own, newest first, and carries nothing that could authenticate a device.
func TestDevicesAreListedPerAccountWithoutTheirToken(t *testing.T) {
	f := newFixture(t)
	owner := f.user(t, "owner@example.test")
	other := f.user(t, "other@example.test")
	main := f.profile(t, owner, "Main")
	kids := f.profile(t, owner, "Kids")
	token, first := f.paired(t, owner, main, "First")
	time.Sleep(2 * time.Millisecond)
	_, second := f.paired(t, owner, kids, "Second")
	f.paired(t, other, f.profile(t, other, "Theirs"), "Foreign")

	devices, err := f.svc.Devices(owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].ID != second.ID || devices[1].ID != first.ID {
		t.Fatalf("devices = %+v", devices)
	}
	if devices[0].ProfileName != "Kids" || devices[1].ProfileName != "Main" || devices[0].LastSyncAt != "" {
		t.Fatalf("devices = %+v", devices)
	}
	encoded, err := json.Marshal(devices)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(encoded, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if len(row) != 6 {
			t.Fatalf("a device exposes %v", row)
		}
		for _, key := range []string{"id", "name", "profile", "profileName", "created", "lastSyncAt"} {
			if _, ok := row[key]; !ok {
				t.Fatalf("a device lacks %s: %v", key, row)
			}
		}
	}
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), hashToken(token)) {
		t.Fatal("the list leaks a token or its hash")
	}
}

func TestRevokeIsLimitedToTheOwnerOfTheDevice(t *testing.T) {
	f := newFixture(t)
	owner := f.user(t, "owner@example.test")
	other := f.user(t, "other@example.test")
	token, device := f.paired(t, owner, f.profile(t, owner, "Main"), "Chrome")

	err := f.svc.Revoke(other.Id, device.ID)
	requireCode(t, err, http.StatusNotFound, "NOT_FOUND")
	requireCode(t, f.svc.Revoke(owner.Id, "missing"), http.StatusNotFound, "NOT_FOUND")
	if _, err := f.svc.Authenticate(token); err != nil {
		t.Fatalf("another account revoked the device: %v", err)
	}
	if err := f.svc.Revoke(owner.Id, device.ID); err != nil {
		t.Fatal(err)
	}
	if rows, err := f.app.FindRecordsByFilter("companion_devices", "user = {:user}", "", 0, 0, dbx.Params{"user": owner.Id}); err != nil || len(rows) != 0 {
		t.Fatalf("the device was not deleted: %v %v", rows, err)
	}
}

// Deleting a profile unpairs the devices that synced into it.
func TestDevicesGoWithTheirProfile(t *testing.T) {
	f := newFixture(t)
	owner := f.user(t, "owner@example.test")
	profile := f.profile(t, owner, "Main")
	token, _ := f.paired(t, owner, profile, "Chrome")
	if err := f.app.Delete(profile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(token); err == nil {
		t.Fatal("a device outlived its profile")
	}
}
