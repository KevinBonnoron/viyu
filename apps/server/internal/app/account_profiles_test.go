package app

import (
	"net/http"
	"testing"
)

// A profile belongs to one account: another account can neither subscribe on it nor remove what it holds.
// (The default profile and the last-profile rule are tested in accounts.)
func TestAccountCannotUseAnotherAccountsProfile(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "profile-owner@example.test")
	other := testUser(t, app, "other-profile-owner@example.test")
	token, otherToken := tokenOf(t, owner), tokenOf(t, other)
	profile, foreign := profileOf(t, app, owner), profileOf(t, app, other)
	yt := testPlatforms().Default().ID()

	foreignChannel := storedChannel(t, app, yt, "UCforeignforeignforeignf", "")
	res := send(t, handler, http.MethodPost, "/api/subscriptions", token, `{"profile":"`+foreign.Id+`","channelId":"`+foreignChannel+`"}`)
	acctExpect(t, res, http.StatusNotFound, "NOT_FOUND")

	ownedChannel := storedChannel(t, app, yt, "UCownedownedownedownedow", "Owned")
	res = send(t, handler, http.MethodPost, "/api/subscriptions", token, `{"profile":"`+profile.Id+`","channelId":"`+ownedChannel+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("own-profile subscription was rejected: %d %s", res.Code, res.Body)
	}
	records, err := app.FindAllRecords("subscriptions")
	if err != nil || len(records) != 1 || records[0].GetString("profile") != profile.Id {
		t.Fatalf("unexpected persisted subscriptions: %+v %v", records, err)
	}

	path := "/api/collections/subscriptions/records/" + records[0].Id
	if res := send(t, handler, http.MethodDelete, path, otherToken, ""); res.Code >= 200 && res.Code < 300 {
		t.Fatalf("another account deleted the subscription: %d", res.Code)
	}
	if res := send(t, handler, http.MethodDelete, path, token, ""); res.Code != http.StatusNoContent {
		t.Fatalf("the owner could not unsubscribe: %d %s", res.Code, res.Body)
	}
}
