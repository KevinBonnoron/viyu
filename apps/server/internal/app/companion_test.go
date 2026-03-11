package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// Identifiers the YouTube platform accepts.
const (
	companionChannelOne = "UCcompanioncompanioncom1"
	companionChannelTwo = "UCcompanioncompanioncom2"
	companionVideo      = "companion01"
)

// companionSend performs a request of the extension: no account, the device token in its header.
func companionSend(t *testing.T, h http.Handler, method, path, deviceToken, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if deviceToken != "" {
		req.Header.Set("X-Viyu-Companion", deviceToken)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}

// companionPair pairs a device to the profile through the routes, as the client and the extension do.
func companionPair(t *testing.T, h http.Handler, accountToken string, profile *core.Record, name string) (token, deviceID string) {
	t.Helper()
	res := send(t, h, http.MethodPost, "/api/companion/pairings", accountToken, acctJSON(t, map[string]string{"profile": profile.Id}))
	acctExpect(t, res, http.StatusOK, "")
	var pairing struct{ Code, ExpiresAt string }
	acctDecode(t, res, &pairing)
	if pairing.Code == "" {
		t.Fatalf("no pairing code: %s", res.Body)
	}
	res = companionSend(t, h, http.MethodPost, "/api/companion/pair", "", acctJSON(t, map[string]string{"code": pairing.Code, "name": name}))
	acctExpect(t, res, http.StatusOK, "")
	var paired struct {
		Token  string
		Device struct{ ID string }
	}
	acctDecode(t, res, &paired)
	if paired.Token == "" || paired.Device.ID == "" {
		t.Fatalf("pair answer: %s", res.Body)
	}
	return paired.Token, paired.Device.ID
}

// A device is paired from a signed-in session, then syncs with its token alone, and unpairing itself ends it.
func TestCompanionPairsSyncsAndUnpairs(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-owner@example.test")
	ownerToken := tokenOf(t, owner)
	profile := profileOf(t, app, owner)

	res := send(t, handler, http.MethodPost, "/api/companion/pairings", ownerToken, acctJSON(t, map[string]string{"profile": profile.Id}))
	acctExpect(t, res, http.StatusOK, "")
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a pairing code is cacheable")
	}
	var pairing struct {
		Code      string `json:"code"`
		ExpiresAt string `json:"expiresAt"`
	}
	acctDecode(t, res, &pairing)
	expires, err := time.Parse(time.RFC3339, pairing.ExpiresAt)
	if err != nil || time.Until(expires) < 9*time.Minute || time.Until(expires) > 10*time.Minute {
		t.Fatalf("expiresAt = %q: %v", pairing.ExpiresAt, err)
	}

	body := acctJSON(t, map[string]string{"code": pairing.Code, "name": "Chrome on Linux"})
	res = companionSend(t, handler, http.MethodPost, "/api/companion/pair", "", body)
	acctExpect(t, res, http.StatusOK, "")
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a device token is cacheable")
	}
	var paired struct {
		Token  string `json:"token"`
		Device struct{ ID, Name, ProfileName string }
	}
	acctDecode(t, res, &paired)
	if paired.Token == "" || paired.Device.Name != "Chrome on Linux" || paired.Device.ProfileName != profile.GetString("name") {
		t.Fatalf("pair answer: %s", res.Body)
	}
	// The code is spent.
	acctExpect(t, companionSend(t, handler, http.MethodPost, "/api/companion/pair", "", body), http.StatusBadRequest, "INVALID_REQUEST")

	res = companionSend(t, handler, http.MethodGet, "/api/companion/session", paired.Token, "")
	acctExpect(t, res, http.StatusOK, "")
	var session struct {
		Device struct{ ID, Name, ProfileName, LastSyncAt string }
	}
	acctDecode(t, res, &session)
	if session.Device.ID != paired.Device.ID || session.Device.LastSyncAt != "" {
		t.Fatalf("session: %s", res.Body)
	}

	res = companionSend(t, handler, http.MethodPost, "/api/companion/youtube/subscriptions", paired.Token,
		acctJSON(t, map[string]any{"channels": []string{companionChannelOne, companionChannelTwo, "nope"}, "complete": true}))
	acctExpect(t, res, http.StatusOK, "")
	var subscriptions map[string]int
	acctDecode(t, res, &subscriptions)
	if subscriptions["created"] != 2 || subscriptions["skipped"] != 1 || subscriptions["removed"] != 0 || subscriptions["unchanged"] != 0 {
		t.Fatalf("subscriptions: %s", res.Body)
	}
	if rows, err := app.FindRecordsByFilter("subscriptions", "profile = {:profile} && synced = true", "", 0, 0, map[string]any{"profile": profile.Id}); err != nil || len(rows) != 2 {
		t.Fatalf("synced subscriptions: %v %v", rows, err)
	}

	watched := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	res = companionSend(t, handler, http.MethodPost, "/api/companion/youtube/history", paired.Token,
		acctJSON(t, map[string]any{"entries": []map[string]any{
			{"videoId": companionVideo, "watchedAt": watched, "position": 12.5, "duration": 300, "watched": false},
			{"videoId": "bad id", "watchedAt": watched},
		}}))
	acctExpect(t, res, http.StatusOK, "")
	var history map[string]int
	acctDecode(t, res, &history)
	if history["created"] != 1 || history["skipped"] != 1 || history["updated"] != 0 || history["unchanged"] != 0 {
		t.Fatalf("history: %s", res.Body)
	}

	// The account sees the device, synced, with nothing that could authenticate it.
	res = request(t, handler, "/api/companion/devices", ownerToken)
	acctExpect(t, res, http.StatusOK, "")
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the device list is cacheable")
	}
	var devices []map[string]any
	acctDecode(t, res, &devices)
	if len(devices) != 1 || devices[0]["id"] != paired.Device.ID || devices[0]["lastSyncAt"] == "" || devices[0]["profileName"] != profile.GetString("name") {
		t.Fatalf("devices: %s", res.Body)
	}
	if strings.Contains(res.Body.String(), paired.Token) || strings.Contains(strings.ToLower(res.Body.String()), "hash") || strings.Contains(strings.ToLower(res.Body.String()), "token") {
		t.Fatalf("the device list leaks a credential: %s", res.Body)
	}

	// Unpairing from the extension ends the token at once.
	acctExpect(t, companionSend(t, handler, http.MethodDelete, "/api/companion/session", paired.Token, ""), http.StatusNoContent, "")
	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/companion/session", ""},
		{http.MethodDelete, "/api/companion/session", ""},
		{http.MethodPost, "/api/companion/youtube/subscriptions", `{"channels":[],"complete":false}`},
		{http.MethodPost, "/api/companion/youtube/history", `{"entries":[]}`},
	} {
		acctExpect(t, companionSend(t, handler, call.method, call.path, paired.Token, call.body), http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
	}
	res = request(t, handler, "/api/companion/devices", ownerToken)
	acctDecode(t, res, &devices)
	if len(devices) != 0 {
		t.Fatalf("devices after unpairing: %s", res.Body)
	}
}

func TestCompanionRoutesNeedAValidDeviceTokenAndAccountRoutesAnAccount(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-auth@example.test")
	ownerToken := tokenOf(t, owner)
	deviceToken, _ := companionPair(t, handler, ownerToken, profileOf(t, app, owner), "Chrome")

	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/companion/session", ""},
		{http.MethodDelete, "/api/companion/session", ""},
		{http.MethodPost, "/api/companion/youtube/subscriptions", `{"channels":[]}`},
		{http.MethodPost, "/api/companion/youtube/history", `{"entries":[]}`},
	} {
		acctExpect(t, companionSend(t, handler, call.method, call.path, "", call.body), http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
		acctExpect(t, companionSend(t, handler, call.method, call.path, "not-a-token", call.body), http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
		// An account token is not a device token.
		acctExpect(t, companionSend(t, handler, call.method, call.path, ownerToken, call.body), http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
	}

	// The device token is no account: it opens none of the account's routes.
	for _, call := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/companion/pairings", `{"profile":"x"}`},
		{http.MethodGet, "/api/companion/devices", ""},
		{http.MethodDelete, "/api/companion/devices/x", ""},
	} {
		if res := send(t, handler, call.method, call.path, "", call.body); res.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without an account: status=%d", call.method, call.path, res.Code)
		}
		if res := companionSend(t, handler, call.method, call.path, deviceToken, call.body); res.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s with a device token: status=%d", call.method, call.path, res.Code)
		}
	}

	// A pairing request that is not a request is refused as such, not as a missing code.
	acctExpect(t, companionSend(t, handler, http.MethodPost, "/api/companion/pair", "", `{"code":"x","other":1}`), http.StatusBadRequest, "INVALID_SOURCE_INPUT")
}

func TestCompanionPairingRefusesAProfileOfAnotherAccount(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-owner@example.test")
	other := testUser(t, app, "companion-other@example.test")
	ownerProfile := profileOf(t, app, owner)

	res := send(t, handler, http.MethodPost, "/api/companion/pairings", tokenOf(t, other), acctJSON(t, map[string]string{"profile": ownerProfile.Id}))
	acctExpect(t, res, http.StatusNotFound, "NOT_FOUND")
	res = send(t, handler, http.MethodPost, "/api/companion/pairings", tokenOf(t, other), acctJSON(t, map[string]string{"profile": "missing"}))
	acctExpect(t, res, http.StatusNotFound, "NOT_FOUND")
}

func TestCompanionDevicesBelongToTheirAccount(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-owner@example.test")
	other := testUser(t, app, "companion-other@example.test")
	ownerToken, otherToken := tokenOf(t, owner), tokenOf(t, other)
	deviceToken, deviceID := companionPair(t, handler, ownerToken, profileOf(t, app, owner), "Owner's browser")
	companionPair(t, handler, otherToken, profileOf(t, app, other), "Other's browser")

	var devices []struct{ ID, Name string }
	res := request(t, handler, "/api/companion/devices", otherToken)
	acctExpect(t, res, http.StatusOK, "")
	acctDecode(t, res, &devices)
	if len(devices) != 1 || devices[0].Name != "Other's browser" {
		t.Fatalf("devices of the other account: %s", res.Body)
	}

	// A device of another account answers like a missing one, and keeps working.
	acctExpect(t, send(t, handler, http.MethodDelete, "/api/companion/devices/"+deviceID, otherToken, ""), http.StatusNotFound, "NOT_FOUND")
	acctExpect(t, send(t, handler, http.MethodDelete, "/api/companion/devices/missing", otherToken, ""), http.StatusNotFound, "NOT_FOUND")
	acctExpect(t, companionSend(t, handler, http.MethodGet, "/api/companion/session", deviceToken, ""), http.StatusOK, "")

	acctExpect(t, send(t, handler, http.MethodDelete, "/api/companion/devices/"+deviceID, ownerToken, ""), http.StatusNoContent, "")
	acctExpect(t, companionSend(t, handler, http.MethodGet, "/api/companion/session", deviceToken, ""), http.StatusUnauthorized, "COMPANION_UNAUTHORIZED")
}

// The devices hold the hash of a credential: no account reaches them through the generic records API.
func TestCompanionDevicesAreUnreachableThroughTheRecordsAPI(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-owner@example.test")
	ownerToken := tokenOf(t, owner)
	_, deviceID := companionPair(t, handler, ownerToken, profileOf(t, app, owner), "Chrome")

	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/collections/companion_devices/records", ""},
		{http.MethodGet, "/api/collections/companion_devices/records/" + deviceID, ""},
		{http.MethodPost, "/api/collections/companion_devices/records", `{"name":"x","token_hash":"x","user":"` + owner.Id + `","profile":"x"}`},
		{http.MethodPatch, "/api/collections/companion_devices/records/" + deviceID, `{"name":"renamed"}`},
		{http.MethodDelete, "/api/collections/companion_devices/records/" + deviceID, ""},
	} {
		for _, token := range []string{ownerToken, ""} {
			res := send(t, handler, call.method, call.path, token, call.body)
			if res.Code < 400 || strings.Contains(res.Body.String(), "token_hash") {
				t.Fatalf("%s %s (token %q): status=%d %s", call.method, call.path, token, res.Code, res.Body)
			}
		}
	}
	if _, err := app.FindRecordById("companion_devices", deviceID); err != nil {
		t.Fatalf("a device was changed through the records API: %v", err)
	}
}

func TestCompanionSyncIsRefusedWhileTheYouTubeSourceIsOff(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-owner@example.test")
	ownerToken := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	deviceToken, _ := companionPair(t, handler, ownerToken, profile, "Chrome")

	acctExpect(t, send(t, handler, http.MethodPut, "/api/sources/youtube/activation", ownerToken, `{"enabled":false}`), http.StatusOK, "")
	acctExpect(t, companionSend(t, handler, http.MethodPost, "/api/companion/youtube/subscriptions", deviceToken, acctJSON(t, map[string]any{"channels": []string{companionChannelOne}})), http.StatusConflict, "SOURCE_DISABLED")
	acctExpect(t, companionSend(t, handler, http.MethodPost, "/api/companion/youtube/history", deviceToken, acctJSON(t, map[string]any{"entries": []map[string]any{{"videoId": companionVideo, "watchedAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}}})), http.StatusConflict, "SOURCE_DISABLED")
	if rows, err := app.FindAllRecords("subscriptions"); err != nil || len(rows) != 0 {
		t.Fatalf("a disabled source was synced: %v %v", rows, err)
	}

	acctExpect(t, send(t, handler, http.MethodPut, "/api/sources/youtube/activation", ownerToken, `{"enabled":true}`), http.StatusOK, "")
	acctExpect(t, companionSend(t, handler, http.MethodPost, "/api/companion/youtube/subscriptions", deviceToken, acctJSON(t, map[string]any{"channels": []string{companionChannelOne}})), http.StatusOK, "")
}

func TestCompanionSyncRefusesMalformedBodies(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "companion-owner@example.test")
	deviceToken, _ := companionPair(t, handler, tokenOf(t, owner), profileOf(t, app, owner), "Chrome")

	for _, call := range []struct{ path, body string }{
		{"/api/companion/youtube/subscriptions", `{"channels":"UC"}`},
		{"/api/companion/youtube/subscriptions", `{"channels":[],"unknown":true}`},
		{"/api/companion/youtube/subscriptions", `not json`},
		{"/api/companion/youtube/history", `{"entries":[{"videoId":"x","watchedAt":"now","extra":1}]}`},
		{"/api/companion/youtube/history", `{"entries":{}}`},
	} {
		if res := companionSend(t, handler, http.MethodPost, call.path, deviceToken, call.body); res.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: status=%d %s", call.path, call.body, res.Code, res.Body)
		}
	}
}
