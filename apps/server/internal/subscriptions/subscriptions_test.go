package subscriptions

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestOwnedProfileHidesWhatTheAccountDoesNotOwn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user(t, "owned-owner@example.test"), f.user(t, "owned-other@example.test")
	profile := f.profile(t, owner, "Mine")
	got, err := f.service.OwnedProfile(owner.Id, profile.Id)
	if err != nil || got.Id != profile.Id {
		t.Fatalf("own profile: %v %v", got, err)
	}
	for name, ask := range map[string]struct{ user, profile string }{
		"another account": {other.Id, profile.Id},
		"unknown profile": {owner.Id, "missing"},
		"empty profile":   {owner.Id, ""},
		"no account":      {"", profile.Id},
	} {
		if _, err := f.service.OwnedProfile(ask.user, ask.profile); err == nil {
			t.Errorf("%s: no error", name)
		} else {
			requireCode(t, err, http.StatusNotFound, apierr.CodeNotFound)
		}
	}
}

func TestCreateDeduplicatesAndKeepsTheStoredChannelMetadata(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "subscribe@example.test")
	profile := f.profile(t, owner, "Subscriptions")
	channel := f.channel(t, alpha, "public", "Public")
	const count = 4
	var wg sync.WaitGroup
	errs := make(chan error, count)
	ids := make(chan string, count)
	for range count {
		wg.Go(func() {
			subscription, err := f.service.Create(owner.Id, profile.Id, channel.Id, "")
			errs <- err
			if err == nil {
				ids <- subscription.Id
			}
		})
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent subscribe: %v", err)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("subscribing twice made two subscriptions: %s %s", first, id)
		}
	}
	subs, err := f.app.FindAllRecords(subscriptionsCollection)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions: %d %v", len(subs), err)
	}
	if got := f.channelOf(t, subs[0]); got.GetString("name") != "Public" {
		t.Fatalf("channel was not resolved publicly: %v", got)
	}
	data, err := json.Marshal(subs[0])
	if err != nil || strings.Contains(string(data), "channel_name") {
		t.Fatalf("subscription contains denormalized metadata: %s %v", data, err)
	}
}

func TestCreateRefusesWhatTheAccountMayNotSubscribeTo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user(t, "create-owner@example.test"), f.user(t, "create-other@example.test")
	profile := f.profile(t, owner, "Mine")
	otherProfile := f.profile(t, other, "Theirs")
	public := f.channel(t, alpha, "public", "Public")
	private := f.channel(t, alpha, "private", "Private")
	private.Set("owners", []string{other.Id})
	if err := f.app.Save(private); err != nil {
		t.Fatal(err)
	}
	shared := f.channel(t, alpha, "shared", "Shared")
	shared.Set("owners", []string{other.Id, owner.Id})
	if err := f.app.Save(shared); err != nil {
		t.Fatal(err)
	}
	rogue := f.channel(t, strict, "rogue", "Rogue")
	valid := f.channel(t, strict, "ok-fine", "Fine")

	for name, test := range map[string]struct {
		user, profile, channel string
		status                 int
		code                   string
	}{
		"another account's profile":       {owner.Id, otherProfile.Id, public.Id, http.StatusNotFound, apierr.CodeNotFound},
		"unknown profile":                 {owner.Id, "missing", public.Id, http.StatusNotFound, apierr.CodeNotFound},
		"unknown channel":                 {owner.Id, profile.Id, "missing", http.StatusBadRequest, apierr.CodeInvalidRequest},
		"no channel":                      {owner.Id, profile.Id, "", http.StatusBadRequest, apierr.CodeInvalidRequest},
		"private to another":              {owner.Id, profile.Id, private.Id, http.StatusBadRequest, apierr.CodeInvalidRequest},
		"identifier the platform refuses": {owner.Id, profile.Id, rogue.Id, http.StatusBadRequest, apierr.CodeInvalidRequest},
	} {
		if _, err := f.service.Create(test.user, test.profile, test.channel, ""); err == nil {
			t.Errorf("%s: subscribed", name)
		} else {
			requireCode(t, err, test.status, test.code)
		}
	}
	// A platform the service does not register (the schema only knows the real ones, so the service is the narrow one).
	registry := platform.NewRegistry(fakePlatform{id: alpha}, fakePlatform{id: beta})
	narrow := New(f.app, catalog.New(f.app, registry, catalog.Options{}), f.sources, registry)
	_, err := narrow.Create(owner.Id, profile.Id, valid.Id, "")
	requireCode(t, err, http.StatusBadRequest, apierr.CodeInvalidRequest)
	if n := f.count(t, subscriptionsCollection); n != 0 {
		t.Fatalf("refused requests created %d subscriptions", n)
	}
	for name, channel := range map[string]string{"public": public.Id, "shared with the account": shared.Id, "valid on a strict platform": valid.Id, "padded id": " " + public.Id + " "} {
		if _, err := f.service.Create(owner.Id, profile.Id, channel, ""); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCreateRefusesAPlatformTheAccountTurnedOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "create-off@example.test")
	profile := f.profile(t, owner, "Off")
	channel := f.channel(t, beta, "muted", "Muted")
	if err := f.sources.SetEnabled(owner.Id, beta, false); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.Create(owner.Id, profile.Id, channel.Id, "")
	requireCode(t, err, http.StatusConflict, apierr.CodeDisabled)
	if err := f.sources.SetEnabled(owner.Id, beta, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Create(owner.Id, profile.Id, channel.Id, ""); err != nil {
		t.Fatalf("subscribing once the platform is back: %v", err)
	}
}

func TestCreateJoinsOnlyCreatorsOfTheSameProfile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user(t, "group-create@example.test"), f.user(t, "group-create-other@example.test")
	profile := f.profile(t, owner, "Create")
	otherProfile := f.profile(t, other, "Create other")
	ownerCreator := f.link(t, profile, "Owner", f.subscribe(t, profile, alpha, "create", "create"))
	foreignCreator := f.link(t, otherProfile, "Foreign", f.subscribe(t, otherProfile, alpha, "foreign", "foreign"))
	created := f.channel(t, beta, "create", "")

	for name, creator := range map[string]string{
		"a creator of another profile": foreignCreator,
		"an oversized creator":         strings.Repeat("g", 65),
		"an unknown creator":           "nobody",
	} {
		if _, err := f.service.Create(owner.Id, profile.Id, created.Id, creator); err == nil {
			t.Errorf("%s: joined", name)
		} else {
			requireCode(t, err, http.StatusBadRequest, apierr.CodeInvalidRequest)
		}
	}
	if n := f.count(t, subscriptionsCollection); n != 2 {
		t.Fatalf("rejected joins created subscriptions: %d", n)
	}
	subscription, err := f.service.Create(owner.Id, profile.Id, created.Id, " "+ownerCreator+" ")
	if err != nil || subscription.GetString("creator") != ownerCreator {
		t.Fatalf("own creator: %v %v", subscription, err)
	}
	// Joining again is the same subscription; joining another creator is a conflict.
	if again, err := f.service.Create(owner.Id, profile.Id, created.Id, ownerCreator); err != nil || again.Id != subscription.Id {
		t.Fatalf("joining twice: %v %v", again, err)
	}
	secondCreator := f.link(t, profile, "Second", f.subscribe(t, profile, alpha, "second", "second"))
	_, err = f.service.Create(owner.Id, profile.Id, created.Id, secondCreator)
	requireCode(t, err, http.StatusConflict, apierr.CodeConflict)
}

func TestCreateRefusesACreatorThatAlreadyHasAChannelOnThePlatform(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "group-platform@example.test")
	profile := f.profile(t, owner, "Platform")
	taken := f.link(t, profile, "Taken", f.subscribe(t, profile, alpha, "first", "first"))
	second := f.channel(t, alpha, "second", "second")
	_, err := f.service.Create(owner.Id, profile.Id, second.Id, taken)
	requireCode(t, err, http.StatusBadRequest, apierr.CodeInvalidRequest)
}

func TestGroupCreatesOneStableCreatorAcrossTwoPlatforms(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "group-owner@example.test")
	profile := f.profile(t, owner, "Grouping")
	first := f.subscribe(t, profile, alpha, "zerator-a", "ZeratoR")
	second := f.subscribe(t, profile, beta, "zerator-b", "")
	records, err := f.service.Group(owner.Id, profile.Id, []string{first.Id, second.Id})
	if err != nil || len(records) != 2 {
		t.Fatalf("group: %v %v", records, err)
	}
	creator := f.creator(t, first.Id)
	if creator == "" || creator != f.creator(t, second.Id) {
		t.Fatalf("subscriptions were not placed in one creator: %q %q", creator, f.creator(t, second.Id))
	}
	if name := f.creatorName(t, creator); name != "ZeratoR" {
		t.Fatalf("creator name: %q", name)
	}
	for _, record := range records {
		if record.GetString("creator") != creator {
			t.Fatalf("returned record %s has creator %q, want %q", record.Id, record.GetString("creator"), creator)
		}
	}
	if _, err := f.service.Group(owner.Id, profile.Id, []string{second.Id, first.Id}); err != nil || f.creator(t, first.Id) != creator || f.creator(t, second.Id) != creator {
		t.Fatalf("regrouping changed the stable creator: %v", err)
	}
	if n := f.count(t, creatorsCollection); n != 1 {
		t.Fatalf("regrouping made another creator: %d", n)
	}
	if err := f.app.Delete(second); err != nil {
		t.Fatal(err)
	}
	if f.creator(t, first.Id) != creator {
		t.Fatal("removing one platform must not remove the other subscription or its creator")
	}
}

func TestGroupReusesExistingCreatorAndPullsItsMembers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "group-existing@example.test")
	profile := f.profile(t, owner, "Existing")
	first := f.subscribe(t, profile, alpha, "existing", "")
	second := f.subscribe(t, profile, beta, "existing", "")
	third := f.subscribe(t, profile, strict, "ok-existing", "")
	existing := f.link(t, profile, "Existing", first, second)
	// Naming the lone subscription and one member brings the whole creator.
	records, err := f.service.Group(owner.Id, profile.Id, []string{third.Id, first.Id})
	if err != nil || len(records) != 3 {
		t.Fatalf("group: %v %v", records, err)
	}
	for _, id := range []string{first.Id, second.Id, third.Id} {
		if f.creator(t, id) != existing {
			t.Fatalf("subscription %s is not in the existing creator: %q", id, f.creator(t, id))
		}
	}
}

func TestGroupRefusesWhatCannotBeLinked(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "group-platforms@example.test")
	profile := f.profile(t, owner, "Platforms")
	first := f.subscribe(t, profile, alpha, "first", "")
	second := f.subscribe(t, profile, alpha, "second", "")
	other := f.subscribe(t, profile, beta, "other", "")
	another := f.subscribe(t, profile, strict, "ok-another", "")
	one := f.link(t, profile, "One", first, other)
	two := f.link(t, profile, "Two", second, another)
	for name, test := range map[string]struct {
		ids    []string
		status int
	}{
		"one subscription":     {[]string{first.Id}, http.StatusBadRequest},
		"duplicated id":        {[]string{first.Id, first.Id}, http.StatusBadRequest},
		"empty":                {nil, http.StatusBadRequest},
		"blank ids":            {[]string{" ", ""}, http.StatusBadRequest},
		"missing subscription": {[]string{first.Id, "missingsubscr0"}, http.StatusNotFound},
		"two creators":         {[]string{first.Id, second.Id}, http.StatusConflict},
	} {
		if _, err := f.service.Group(owner.Id, profile.Id, test.ids); err == nil {
			t.Errorf("%s: grouped", name)
		} else {
			requireCode(t, err, test.status, "")
		}
	}
	lonely := f.subscribe(t, profile, alpha, "third", "")
	// Same platform as a member of the creator it would join.
	_, err := f.service.Group(owner.Id, profile.Id, []string{lonely.Id, other.Id})
	requireCode(t, err, http.StatusBadRequest, apierr.CodeInvalidRequest)
	if f.creator(t, lonely.Id) != "" || f.creator(t, first.Id) != one || f.creator(t, second.Id) != two {
		t.Fatal("rejected grouping modified subscriptions")
	}
}

func TestGroupRejectsOtherAccountsAndProfiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user(t, "group-private-owner@example.test"), f.user(t, "group-private-other@example.test")
	ownerProfile := f.profile(t, owner, "Owner")
	siblingProfile := f.profile(t, owner, "Sibling")
	otherProfile := f.profile(t, other, "Other")
	first := f.subscribe(t, ownerProfile, alpha, "private", "")
	second := f.subscribe(t, ownerProfile, beta, "private-2", "")
	sibling := f.subscribe(t, siblingProfile, beta, "private-3", "")
	foreign := f.subscribe(t, otherProfile, beta, "private-4", "")
	for name, test := range map[string]struct {
		user, profile string
		ids           []string
	}{
		"another account cannot use the profile":      {other.Id, ownerProfile.Id, []string{first.Id, second.Id}},
		"the owner cannot group with another profile": {owner.Id, ownerProfile.Id, []string{first.Id, sibling.Id}},
		"the owner cannot group another account's":    {owner.Id, ownerProfile.Id, []string{first.Id, foreign.Id}},
		"another account with its own profile":        {other.Id, otherProfile.Id, []string{first.Id, foreign.Id}},
	} {
		_, err := f.service.Group(test.user, test.profile, test.ids)
		if err == nil {
			t.Errorf("%s: grouped", name)
			continue
		}
		requireCode(t, err, http.StatusNotFound, apierr.CodeNotFound)
	}
	for _, id := range []string{first.Id, second.Id, sibling.Id, foreign.Id} {
		if creator := f.creator(t, id); creator != "" {
			t.Fatalf("rejected request grouped %s into %q", id, creator)
		}
	}
	if n := f.count(t, creatorsCollection); n != 0 {
		t.Fatalf("rejected requests created %d creators", n)
	}
}

func TestUngroupClearsTheCreatorMarksEachMemberAndIsScopedToTheOwner(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, stranger := f.user(t, "ungroup-owner@example.test"), f.user(t, "ungroup-stranger@example.test")
	profile := f.profile(t, owner, "Mine")
	first := f.subscribe(t, profile, alpha, "mine", "")
	second := f.subscribe(t, profile, beta, "mine", "")
	creator := f.link(t, profile, "Mine", first, second)

	_, err := f.service.Ungroup(stranger.Id, profile.Id, creator)
	requireCode(t, err, http.StatusNotFound, apierr.CodeNotFound)
	if f.creator(t, first.Id) != creator {
		t.Fatal("another account separated a creator")
	}
	for name, id := range map[string]string{"blank": "  ", "unknown": "nobody"} {
		if _, err := f.service.Ungroup(owner.Id, profile.Id, id); err == nil {
			t.Errorf("%s creator: separated", name)
		}
	}
	members, err := f.service.Ungroup(owner.Id, profile.Id, " "+creator+" ")
	if err != nil || len(members) != 2 {
		t.Fatalf("ungroup: %v %v", members, err)
	}
	for _, id := range []string{first.Id, second.Id} {
		if f.creator(t, id) != "" || !f.ungrouped(t, id) {
			t.Fatalf("separated subscription %s must have no creator and be ungrouped", id)
		}
	}
	if n := f.count(t, creatorsCollection); n != 0 {
		t.Fatalf("the emptied creator was kept: %d", n)
	}
}

func TestPruneCreatorsDeletesCreatorsBelowTwoSubscriptions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "prune@example.test")
	profile := f.profile(t, owner, "Prune")
	kept := f.link(t, profile, "Kept", f.subscribe(t, profile, alpha, "k-a", ""), f.subscribe(t, profile, beta, "k-b", ""))
	lone := f.subscribe(t, profile, alpha, "lone", "")
	f.link(t, profile, "Lone", lone)
	f.link(t, profile, "Empty")
	if err := PruneCreators(f.app, profile.Id); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, creatorsCollection); n != 1 {
		t.Fatalf("creators after pruning: %d", n)
	}
	if _, err := f.app.FindRecordById(creatorsCollection, kept); err != nil {
		t.Fatalf("a creator of two subscriptions was pruned: %v", err)
	}
	if f.creator(t, lone.Id) != "" {
		t.Fatal("the subscription of a pruned creator still points at it")
	}
}

func TestSubscribedListsTheChannelsOfOneProfileInOneQuery(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "subscribed@example.test")
	mine, other := f.profile(t, owner, "Mine"), f.profile(t, owner, "Other")
	first := f.subscribe(t, mine, alpha, "first", "First")
	second := f.subscribe(t, mine, beta, "second", "Second")
	f.subscribe(t, other, alpha, "elsewhere", "Elsewhere")
	if channels, err := f.service.Subscribed(f.profile(t, owner, "Empty").Id); err != nil || len(channels) != 0 {
		t.Fatalf("no subscription: %v %v", channels, err)
	}
	var got []string
	queries := countQueries(f.app, "channels", func() {
		channels, err := f.service.Subscribed(mine.Id)
		if err != nil {
			t.Fatal(err)
		}
		for _, channel := range channels {
			got = append(got, channel.Id)
		}
	})
	if len(got) != 2 || !(got[0] == first.GetString("channel") && got[1] == second.GetString("channel") || got[1] == first.GetString("channel") && got[0] == second.GetString("channel")) {
		t.Fatalf("subscribed channels: %v", got)
	}
	if queries != 1 {
		t.Fatalf("channels were read with %d queries, want 1", queries)
	}
	if n := f.count(t, subscriptionsCollection, dbx.HashExp{"profile": mine.Id}); n != 2 {
		t.Fatalf("fixture: %d", n)
	}
}
