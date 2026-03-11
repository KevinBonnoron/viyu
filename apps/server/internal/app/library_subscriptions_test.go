package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// libSubscriptionJSON is a subscription as the client reads it.
type libSubscriptionJSON struct {
	ID        string `json:"id"`
	Profile   string `json:"profile"`
	Channel   string `json:"channel"`
	Creator   string `json:"creator"`
	Ungrouped bool   `json:"ungrouped"`
}

func TestSubscribingIsOneSubscriptionWhateverTheRequestsAndTheBodyIsNotTrusted(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "subscribe@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)
	channelRecord := storedChannel(t, app, "youtube", libChannelID("public"), "Public")
	// The channel is addressed by its record id; the platform and the name of the body are not trusted.
	body := map[string]string{"profile": profile.Id, "platform": "twitch", "channelId": channelRecord, "name": "Private"}

	const count = 4
	var wg sync.WaitGroup
	answers := make(chan libSubscriptionJSON, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := libDo(t, handler, http.MethodPost, "/api/subscriptions", token, body)
			if res.Code != http.StatusOK {
				t.Errorf("subscription status %d: %s", res.Code, res.Body)
				return
			}
			answers <- libOK[libSubscriptionJSON](t, res)
		}()
	}
	wg.Wait()
	close(answers)
	subs, err := app.FindAllRecords("subscriptions")
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions: %d %v", len(subs), err)
	}
	for answer := range answers {
		if answer.ID != subs[0].Id || answer.Profile != profile.Id || answer.Channel != channelRecord {
			t.Fatalf("every request is answered with the one subscription: %+v want %s", answer, subs[0].Id)
		}
	}
	if channel := libChannelOf(t, app, subs[0]); channel.GetString("name") != "Public" || channel.GetString("platform") != "youtube" {
		t.Fatalf("the channel was not the stored one: %v %v", channel.GetString("name"), channel.GetString("platform"))
	}
	if raw := libDo(t, handler, http.MethodPost, "/api/subscriptions", token, body).Body.String(); strings.Contains(raw, "channel_name") || strings.Contains(raw, "Private") || strings.Contains(raw, libChannelID("public")) {
		t.Fatalf("the subscription shows denormalized metadata or a native id: %s", raw)
	}
}

func TestSubscribingRefusesWhatCannotBeFollowed(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "create-owner@example.test"), testUser(t, app, "create-other@example.test")
	profile, foreign := profileOf(t, app, owner), profileOf(t, app, other)
	token := tokenOf(t, owner)
	good := storedChannel(t, app, "youtube", libChannelID("good"), "Good")
	malformed := storedChannel(t, app, "youtube", "UCshort", "Malformed")
	subscribe := func(token string, body map[string]string) *httptest.ResponseRecorder {
		return libDo(t, handler, http.MethodPost, "/api/subscriptions", token, body)
	}

	for name, test := range map[string]struct {
		token  string
		body   map[string]string
		status int
		code   string
	}{
		"anonymous":             {"", map[string]string{"profile": profile.Id, "channelId": good}, http.StatusUnauthorized, ""},
		"a native id":           {token, map[string]string{"profile": profile.Id, "channelId": libChannelID("good")}, http.StatusBadRequest, "INVALID_REQUEST"},
		"no channel":            {token, map[string]string{"profile": profile.Id}, http.StatusBadRequest, "INVALID_REQUEST"},
		"a channel never known": {token, map[string]string{"profile": profile.Id, "channelId": "neverseen00000"}, http.StatusBadRequest, "INVALID_REQUEST"},
		"a malformed channel":   {token, map[string]string{"profile": profile.Id, "channelId": malformed}, http.StatusBadRequest, "INVALID_REQUEST"},
		"another account":       {token, map[string]string{"profile": foreign.Id, "channelId": good}, http.StatusNotFound, "NOT_FOUND"},
		"an unknown profile":    {token, map[string]string{"profile": "missing", "channelId": good}, http.StatusNotFound, "NOT_FOUND"},
		"a creator of nobody":   {token, map[string]string{"profile": profile.Id, "channelId": good, "creator": "nobody"}, http.StatusBadRequest, "INVALID_REQUEST"},
		"an oversized creator":  {token, map[string]string{"profile": profile.Id, "channelId": good, "creator": strings.Repeat("g", 65)}, http.StatusBadRequest, "INVALID_REQUEST"},
	} {
		libFail(t, subscribe(test.token, test.body), test.status, test.code)
		if t.Failed() {
			t.Fatalf("%s", name)
		}
	}
	if subs, err := app.FindAllRecords("subscriptions"); err != nil || len(subs) != 0 {
		t.Fatalf("a refused subscription was stored: %d %v", len(subs), err)
	}
	// A channel another account's session cached privately does not exist for this one.
	privateRecord, err := newStore(app).UpsertChannel("youtube", libChannelID("private"), "Private", "", other.Id)
	if err != nil {
		t.Fatal(err)
	}
	libFail(t, subscribe(token, map[string]string{"profile": profile.Id, "channelId": privateRecord.Id}), http.StatusBadRequest, "INVALID_REQUEST")
}

func TestSubscribingJoinsOnlyCreatorsOfTheSameProfile(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "group-create@example.test"), testUser(t, app, "group-create-other@example.test")
	profile, foreignProfile := profileOf(t, app, owner), profileOf(t, app, other)
	token := tokenOf(t, owner)
	youtube := libSubscribe(t, app, profile, "youtube", libChannelID("create"), "Create")
	ownerCreator := libLink(t, app, profile, "Owner", youtube)
	foreign := libSubscribe(t, app, foreignProfile, "youtube", libChannelID("foreign"), "Foreign")
	foreignCreator := libLink(t, app, foreignProfile, "Foreign", foreign)
	created := storedChannel(t, app, "twitch", "create", "")
	join := func(creator string) *httptest.ResponseRecorder {
		return libDo(t, handler, http.MethodPost, "/api/subscriptions", token, map[string]any{"profile": profile.Id, "channelId": created, "creator": creator})
	}
	libFail(t, join(foreignCreator), http.StatusBadRequest, "INVALID_REQUEST")
	libFail(t, join(strings.Repeat("g", 65)), http.StatusBadRequest, "INVALID_REQUEST")
	if subs, err := app.FindAllRecords("subscriptions"); err != nil || len(subs) != 2 {
		t.Fatalf("rejected joins created subscriptions: %d %v", len(subs), err)
	}
	if answer := libOK[libSubscriptionJSON](t, join(ownerCreator)); answer.Creator != ownerCreator || answer.Channel != created {
		t.Fatalf("created subscription: %+v", answer)
	}
	// The same platform cannot join a creator twice.
	again := storedChannel(t, app, "youtube", libChannelID("another"), "Another")
	libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions", token, map[string]any{"profile": profile.Id, "channelId": again, "creator": ownerCreator}), http.StatusBadRequest, "INVALID_REQUEST")
}

func TestGroupingSubscriptionsCreatesOneStableCreatorAcrossTwoPlatforms(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "group-owner@example.test")
	profile := libProfile(t, app, owner, "Grouping")
	token := tokenOf(t, owner)
	youtube := libSubscribe(t, app, profile, "youtube", libChannelID("zerator"), "ZeratoR")
	twitch := libSubscribe(t, app, profile, "twitch", "zerator", "ZeratoR")
	body := map[string]any{"profile": profile.Id, "subscriptionIds": []string{youtube.Id, twitch.Id}}

	records := libOK[[]libSubscriptionJSON](t, libDo(t, handler, http.MethodPost, "/api/subscriptions/group", token, body))
	if len(records) != 2 {
		t.Fatalf("the answer lists the members of the creator: %+v", records)
	}
	creator := libCreatorOf(t, app, youtube.Id)
	if creator == "" || creator != libCreatorOf(t, app, twitch.Id) {
		t.Fatalf("subscriptions were not placed in one creator: %q %q", creator, libCreatorOf(t, app, twitch.Id))
	}
	for _, record := range records {
		if record.Creator != creator || record.Profile != profile.Id {
			t.Fatalf("answer record %+v, want creator %q", record, creator)
		}
	}
	if again := libDo(t, handler, http.MethodPost, "/api/subscriptions/group", token, body); again.Code != http.StatusOK || libCreatorOf(t, app, youtube.Id) != creator || libCreatorOf(t, app, twitch.Id) != creator {
		t.Fatalf("regrouping changed the stable creator: status=%d body=%s", again.Code, again.Body)
	}
	if err := app.Delete(twitch); err != nil {
		t.Fatal(err)
	}
	if libCreatorOf(t, app, youtube.Id) != creator {
		t.Fatal("removing one platform must not remove the other subscription or its creator")
	}
}

func TestGroupingSubscriptionsReusesAnExistingCreatorAndRefusesWhatCannotBeLinked(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "group-existing@example.test")
	profile := libProfile(t, app, owner, "Existing")
	token := tokenOf(t, owner)
	youtube := libSubscribe(t, app, profile, "youtube", libChannelID("existing"), "Existing")
	existing := libLink(t, app, profile, "Existing", youtube)
	twitch := libSubscribe(t, app, profile, "twitch", "existing", "Existing")
	res := libDo(t, handler, http.MethodPost, "/api/subscriptions/group", token, map[string]any{"profile": profile.Id, "subscriptionIds": []string{twitch.Id, youtube.Id}})
	if res.Code != http.StatusOK || libCreatorOf(t, app, twitch.Id) != existing || libCreatorOf(t, app, youtube.Id) != existing {
		t.Fatalf("the existing creator was not reused: status=%d body=%s", res.Code, res.Body)
	}

	first := libSubscribe(t, app, profile, "youtube", libChannelID("first"), "First")
	second := libSubscribe(t, app, profile, "youtube", libChannelID("second"), "Second")
	for name, test := range map[string]struct {
		ids    []string
		status int
	}{
		"one subscription":  {[]string{first.Id}, http.StatusBadRequest},
		"a duplicated id":   {[]string{first.Id, first.Id}, http.StatusBadRequest},
		"the same platform": {[]string{first.Id, second.Id}, http.StatusBadRequest},
		"nothing":           {[]string{}, http.StatusBadRequest},
		"a missing":         {[]string{first.Id, "missingsubscr0"}, http.StatusNotFound},
		"another creator":   {[]string{first.Id, twitch.Id}, http.StatusBadRequest},
	} {
		res := libDo(t, handler, http.MethodPost, "/api/subscriptions/group", token, map[string]any{"profile": profile.Id, "subscriptionIds": test.ids})
		code := "INVALID_REQUEST"
		if test.status == http.StatusNotFound {
			code = "NOT_FOUND"
		}
		libFail(t, res, test.status, code)
		if t.Failed() {
			t.Fatalf("%s", name)
		}
	}
	if libCreatorOf(t, app, first.Id) != "" || libCreatorOf(t, app, second.Id) != "" {
		t.Fatal("a refused grouping modified subscriptions")
	}
	// Two creators cannot be merged: that is a conflict with what the account chose.
	sibling := libSubscribe(t, app, profile, "twitch", "sibling", "Sibling")
	other := libSubscribe(t, app, profile, "youtube", libChannelID("other"), "Other")
	libLink(t, app, profile, "Second", sibling, other)
	libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions/group", token, map[string]any{"profile": profile.Id, "subscriptionIds": []string{youtube.Id, sibling.Id}}), http.StatusConflict, "CONFLICT")
}

func TestGroupingAndUngroupingNeverTouchAnotherAccountsOrProfilesSubscriptions(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "group-private-owner@example.test"), testUser(t, app, "group-private-other@example.test")
	ownerProfile := libProfile(t, app, owner, "Owner")
	siblingProfile := libProfile(t, app, owner, "Sibling")
	otherProfile := libProfile(t, app, other, "Other")
	ownerToken, otherToken := tokenOf(t, owner), tokenOf(t, other)
	youtube := libSubscribe(t, app, ownerProfile, "youtube", libChannelID("private"), "Private")
	twitch := libSubscribe(t, app, ownerProfile, "twitch", "private", "Private")
	siblingTwitch := libSubscribe(t, app, siblingProfile, "twitch", "private", "Private")
	foreignTwitch := libSubscribe(t, app, otherProfile, "twitch", "private", "Private")
	for name, test := range map[string]struct {
		token   string
		profile string
		ids     []string
	}{
		"another account cannot use the owner's profile":    {otherToken, ownerProfile.Id, []string{youtube.Id, twitch.Id}},
		"the owner cannot group with another profile":       {ownerToken, ownerProfile.Id, []string{youtube.Id, siblingTwitch.Id}},
		"the owner cannot group with another account's":     {ownerToken, ownerProfile.Id, []string{youtube.Id, foreignTwitch.Id}},
		"another account cannot group with the owner's":     {otherToken, otherProfile.Id, []string{youtube.Id, foreignTwitch.Id}},
		"nobody groups under a profile that does not exist": {ownerToken, "missing", []string{youtube.Id, twitch.Id}},
	} {
		libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions/group", test.token, map[string]any{"profile": test.profile, "subscriptionIds": test.ids}), http.StatusNotFound, "NOT_FOUND")
		if t.Failed() {
			t.Fatalf("%s", name)
		}
	}
	for _, id := range []string{youtube.Id, twitch.Id, siblingTwitch.Id, foreignTwitch.Id} {
		if creator := libCreatorOf(t, app, id); creator != "" {
			t.Fatalf("a refused request grouped %s into %q", id, creator)
		}
	}

	grouped := libSubscribe(t, app, ownerProfile, "twitch", "mine", "Mine")
	mine := libLink(t, app, ownerProfile, "Mine", grouped)
	libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions/ungroup", otherToken, map[string]any{"profile": ownerProfile.Id, "creator": mine}), http.StatusNotFound, "NOT_FOUND")
	libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions/ungroup", ownerToken, map[string]any{"profile": siblingProfile.Id, "creator": mine}), http.StatusNotFound, "NOT_FOUND")
	libFail(t, libDo(t, handler, http.MethodPost, "/api/subscriptions/ungroup", ownerToken, map[string]any{"profile": ownerProfile.Id, "creator": ""}), http.StatusBadRequest, "INVALID_REQUEST")
	if libCreatorOf(t, app, grouped.Id) != mine {
		t.Fatal("the creator changed")
	}
}

func TestSubscribingMergesTheCreatorWithoutAskingAndSeparatingIsRemembered(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	listing.mode(t, "fail")
	app, handler := libApp(t, listing.option())
	owner := testUser(t, app, "autogroup-flow@example.test")
	profile := libProfile(t, app, owner, "Flow")
	token := tokenOf(t, owner)
	youtube := libSubscribe(t, app, profile, "youtube", libChannelID("flow"), "Flow Creator")
	// The Twitch channel is stored first: a subscription is made on a channel record.
	twitch := storedChannel(t, app, "twitch", "flowcreator", "Flow Creator")

	created := libOK[libSubscriptionJSON](t, libDo(t, handler, http.MethodPost, "/api/subscriptions", token, map[string]any{"profile": profile.Id, "channelId": twitch}))
	if created.Creator == "" {
		t.Fatalf("the new subscription must come back already grouped: %+v", created)
	}
	if libCreatorOf(t, app, youtube.Id) != created.Creator {
		t.Fatal("the existing subscription was not linked to the new one")
	}

	// Separating deletes the creator and marks each channel, so neither refreshing nor subscribing merges them again.
	members := libOK[[]libSubscriptionJSON](t, libDo(t, handler, http.MethodPost, "/api/subscriptions/ungroup", token, map[string]any{"profile": profile.Id, "creator": created.Creator}))
	if len(members) != 2 {
		t.Fatalf("the answer lists the separated subscriptions: %+v", members)
	}
	for _, id := range []string{youtube.Id, created.ID} {
		if libCreatorOf(t, app, id) != "" {
			t.Fatalf("separated subscription %s kept a creator", id)
		}
	}
	refreshed := libDo(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+profile.Id, token, nil)
	if refreshed.Code != http.StatusOK || libCreatorOf(t, app, youtube.Id) != "" || libCreatorOf(t, app, created.ID) != "" {
		t.Fatalf("a separated creator was merged again by a refresh: status=%d", refreshed.Code)
	}

	// The user can still link them by hand.
	res := libDo(t, handler, http.MethodPost, "/api/subscriptions/group", token, map[string]any{"profile": profile.Id, "subscriptionIds": []string{youtube.Id, created.ID}})
	if res.Code != http.StatusOK || libCreatorOf(t, app, youtube.Id) == "" || libCreatorOf(t, app, youtube.Id) != libCreatorOf(t, app, created.ID) {
		t.Fatalf("manual regrouping of separated channels failed: status=%d body=%s", res.Code, res.Body)
	}
}

func TestUnsubscribingThroughTheRecordsAPIDeletesTheCreatorLeftWithOneSubscription(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner := testUser(t, app, "unsubscribe-creator@example.test")
	profile := libProfile(t, app, owner, "Unsubscribe")
	token := tokenOf(t, owner)
	youtube := libSubscribe(t, app, profile, "youtube", libChannelID("leaving"), "Leaving")
	twitch := libSubscribe(t, app, profile, "twitch", "leaving", "Leaving")
	libLink(t, app, profile, "Leaving", youtube, twitch)

	res := libDo(t, handler, http.MethodDelete, "/api/collections/subscriptions/records/"+twitch.Id, token, nil)
	if res.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", res.Code, res.Body)
	}
	if n, err := app.CountRecords("creators"); err != nil || n != 0 {
		t.Fatalf("the creator left with one subscription was kept: %d %v", n, err)
	}
	if libCreatorOf(t, app, youtube.Id) != "" {
		t.Fatal("the remaining subscription still points at the deleted creator")
	}
}

func TestRefreshingSubscriptionsNeedsAnOwnedProfile(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	app, handler := libApp(t, listing.option())
	owner, other := testUser(t, app, "refresh-owner@example.test"), testUser(t, app, "refresh-other@example.test")
	mine, foreign := libProfile(t, app, owner, "Mine"), libProfile(t, app, other, "Foreign")
	token := tokenOf(t, owner)
	for _, test := range []struct {
		name, profile, token string
		status               int
		code                 string
	}{
		{"anonymous", mine.Id, "", http.StatusUnauthorized, ""},
		{"missing profile", "", token, http.StatusBadRequest, "INVALID_REQUEST"},
		{"unknown profile", "not-a-profile", token, http.StatusNotFound, "NOT_FOUND"},
		{"foreign profile", foreign.Id, token, http.StatusNotFound, "NOT_FOUND"},
	} {
		res := libDo(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+test.profile, test.token, nil)
		libFail(t, res, test.status, test.code)
		if t.Failed() {
			t.Fatal(test.name)
		}
	}
	if result := libOK[map[string]int](t, libDo(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+mine.Id, token, nil)); result["refreshed"] != 0 || result["failed"] != 0 {
		t.Fatalf("no subscriptions, nothing to refresh: %v", result)
	}
	if listing.listings() != 0 {
		t.Fatal("a refresh without subscriptions started the downloader")
	}
}

// A search result names a channel, it lists nothing: a subscribed channel whose listing expired is still
// refreshed afterwards, and only a real listing renews it.
func TestASearchResultNamingASubscribedChannelDoesNotHideItsExpiredListing(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	app, handler := libApp(t, listing.option())
	owner := testUser(t, app, "expired-listing@example.test")
	profile := profileOf(t, app, owner)
	token := tokenOf(t, owner)
	nativeID := libChannelID("expired")
	subscription := libSubscribe(t, app, profile, "youtube", nativeID, "Expired channel")
	channel := libChannelOf(t, app, subscription)
	libExpire(t, app, channel)
	listing.card(t, `{"_type":"playlist","ie_key":"YoutubeTab","id":"`+nativeID+`","channel":"Renamed channel","url":"https://www.youtube.com/channel/`+nativeID+`"}`)

	page := libOK[struct {
		Results []platform.ChannelCard `json:"results"`
	}](t, request(t, handler, "/api/search?q=renamed&platform=youtube", token))
	if len(page.Results) != 1 || page.Results[0].RecordID != channel.Id || page.Results[0].ID != channel.Id || page.Results[0].Name != "Renamed channel" {
		t.Fatalf("the search card is not the stored channel: %+v", page.Results)
	}
	stored, err := app.FindRecordById("channels", channel.Id)
	if err != nil || stored.GetString("name") != "Renamed channel" || stored.GetDateTime("expires_at").Time().After(time.Now()) {
		t.Fatalf("a channel card renewed the listing or was not stored: %v %v", stored, err)
	}
	if listing.listings() != 0 {
		t.Fatal("a search listed the channel")
	}

	if result := libOK[map[string]int](t, libDo(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+profile.Id, token, nil)); result["refreshed"] != 1 || result["failed"] != 0 {
		t.Fatalf("the channel card hid the expired listing from the refresh: %v", result)
	}
	stored, err = app.FindRecordById("channels", channel.Id)
	if err != nil || !stored.GetDateTime("expires_at").Time().After(time.Now()) {
		t.Fatalf("a real listing must renew the channel: %v %v", stored, err)
	}
}

func TestRefreshingSubscriptionsOnlyListsStaleChannelsAndToleratesFailures(t *testing.T) {
	t.Parallel()
	listing := newLibListing(t)
	listing.mode(t, "fail")
	app, handler := libApp(t, listing.option())
	owner := testUser(t, app, "refresh-stale@example.test")
	profile := libProfile(t, app, owner, "Refresh")
	token := tokenOf(t, owner)
	refresh := func() map[string]int {
		return libOK[map[string]int](t, libDo(t, handler, http.MethodPost, "/api/subscriptions/refresh?profile="+profile.Id, token, nil))
	}

	// A fresh channel, with a fresh video, is served from the cache: nothing is attempted.
	freshID := libChannelID("fresh")
	storedVideo(t, app, platform.Video{ID: "keptvideo01", Platform: "youtube", Title: "Kept", ChannelID: freshID, ChannelName: "Fresh", Published: "2026-06-01"})
	fresh := libSubscribe(t, app, profile, "youtube", freshID, "Fresh")
	channel := libChannelOf(t, app, fresh)
	libFresh(t, app, channel)
	if result := refresh(); result["refreshed"] != 0 || result["failed"] != 0 || listing.listings() != 0 {
		t.Fatalf("fresh cache: %v (%d listings)", result, listing.listings())
	}

	// An expired channel whose listing fails is counted as failed, not answered as a server error.
	libExpire(t, app, channel)
	if result := refresh(); result["refreshed"] != 0 || result["failed"] != 1 || listing.listings() != 1 {
		t.Fatalf("failed refresh: %v (%d listings)", result, listing.listings())
	}

	// With a working downloader every stale channel is listed and its videos are cached.
	listing.mode(t, "")
	libSubscribe(t, app, profile, "youtube", libChannelID("second"), "Second")
	if result := refresh(); result["refreshed"] != 2 || result["failed"] != 0 {
		t.Fatalf("refresh: %v", result)
	}
	for _, id := range []string{freshID, libChannelID("second")} {
		stored, err := newStore(app).FindChannel("youtube", id)
		if err != nil || stored.GetDateTime("expires_at").Time().Before(time.Now()) {
			t.Fatalf("the listing of %s was not cached: %v", id, err)
		}
		if n, err := app.CountRecords("videos", dbx.HashExp{"channel": stored.Id}); err != nil || n == 0 {
			t.Fatalf("the videos of %s were not cached: %d %v", id, n, err)
		}
	}
	// Their listing is current now: a second refresh has nothing to do.
	before := listing.listings()
	if result := refresh(); result["refreshed"] != 0 || result["failed"] != 0 || listing.listings() != before {
		t.Fatalf("a current listing was listed again: %v", result)
	}
}

func TestMovingSubscriptionsBetweenProfilesIsScopedToTheAccountAndReportsWhatMoved(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "move-owner@example.test"), testUser(t, app, "move-other@example.test")
	from, to := libProfile(t, app, owner, "Loisir"), libProfile(t, app, owner, "Gaming")
	foreign := libProfile(t, app, other, "Foreign")
	token, otherToken := tokenOf(t, owner), tokenOf(t, other)
	first := libSubscribe(t, app, from, "youtube", libChannelID("moveone"), "One")
	second := libSubscribe(t, app, from, "youtube", libChannelID("movetwo"), "Two")
	twin := libSubscribe(t, app, to, "youtube", libChannelID("movetwo"), "Two")
	move := func(token, source, target string, ids ...string) *httptest.ResponseRecorder {
		return libDo(t, handler, http.MethodPost, "/api/subscriptions/move", token, map[string]any{"from": source, "to": target, "subscriptionIds": ids})
	}

	libFail(t, move(otherToken, from.Id, foreign.Id, first.Id), http.StatusNotFound, "NOT_FOUND")
	libFail(t, move(token, from.Id, foreign.Id, first.Id), http.StatusNotFound, "NOT_FOUND")
	libFail(t, move(token, from.Id, from.Id, first.Id), http.StatusBadRequest, "INVALID_REQUEST")
	libFail(t, move(token, from.Id, to.Id), http.StatusBadRequest, "INVALID_REQUEST")
	libFail(t, move(token, to.Id, from.Id, first.Id), http.StatusNotFound, "NOT_FOUND")
	if n, err := app.CountRecords("subscriptions", dbx.HashExp{"profile": from.Id}); err != nil || n != 2 {
		t.Fatalf("refused moves changed the source: %d %v", n, err)
	}

	answer := libOK[struct {
		Moved     int      `json:"moved"`
		Merged    int      `json:"merged"`
		MovedIDs  []string `json:"movedIds"`
		MergedIDs []string `json:"mergedIds"`
	}](t, move(token, from.Id, to.Id, first.Id, second.Id))
	if answer.Moved != 1 || answer.Merged != 1 || len(answer.MovedIDs) != 1 || answer.MovedIDs[0] != first.Id || len(answer.MergedIDs) != 1 || answer.MergedIDs[0] != twin.Id {
		t.Fatalf("answer: %+v", answer)
	}
	if n, err := app.CountRecords("subscriptions", dbx.HashExp{"profile": from.Id}); err != nil || n != 0 {
		t.Fatalf("the source keeps %d subscriptions: %v", n, err)
	}
	if n, err := app.CountRecords("subscriptions", dbx.HashExp{"profile": to.Id}); err != nil || n != 2 {
		t.Fatalf("the target holds %d subscriptions: %v", n, err)
	}
}
