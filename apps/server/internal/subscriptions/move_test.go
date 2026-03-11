package subscriptions

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// profileOf returns the profile a subscription belongs to.
func (f *fixture) profileOf(t testing.TB, id string) string {
	t.Helper()
	record, err := f.app.FindRecordById(subscriptionsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("profile")
}

func (f *fixture) exists(collection, id string) bool {
	_, err := f.app.FindRecordById(collection, id)
	return err == nil
}

func (f *fixture) creatorProfile(t testing.TB, id string) string {
	t.Helper()
	record, err := f.app.FindRecordById(creatorsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("profile")
}

func TestMoveHandsSubscriptionsToAnotherProfileOfTheAccount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "move-owner@example.test")
	from, to := f.profile(t, owner, "Loisir"), f.profile(t, owner, "Gaming")
	first := f.subscribe(t, from, alpha, "move-a", "Alpha")
	second := f.subscribe(t, from, alpha, "move-b", "Bravo")
	stay := f.subscribe(t, from, alpha, "move-c", "Charlie")

	result, err := f.service.Move(owner.Id, from.Id, to.Id, []string{first.Id, " " + second.Id + " ", first.Id}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved != 2 || result.Merged != 0 || len(result.MovedIDs) != 2 || len(result.MergedIDs) != 0 {
		t.Fatalf("result: %+v", result)
	}
	if f.profileOf(t, first.Id) != to.Id || f.profileOf(t, second.Id) != to.Id || f.profileOf(t, stay.Id) != from.Id {
		t.Fatal("only the named subscriptions move")
	}
}

func TestMoveIsScopedToTheProfilesAndSubscriptionsOfTheAccount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, stranger := f.user(t, "move-scope-owner@example.test"), f.user(t, "move-scope-stranger@example.test")
	from, to := f.profile(t, owner, "From"), f.profile(t, owner, "To")
	elsewhere := f.profile(t, owner, "Elsewhere")
	foreign := f.profile(t, stranger, "Foreign")
	mine := f.subscribe(t, from, alpha, "scope-a", "Alpha")
	sibling := f.subscribe(t, elsewhere, alpha, "scope-b", "Bravo")
	theirs := f.subscribe(t, foreign, alpha, "scope-c", "Charlie")

	for name, test := range map[string]struct {
		user, from, to string
		ids            []string
		status         int
		code           string
	}{
		"another account's source":          {stranger.Id, from.Id, foreign.Id, []string{mine.Id}, http.StatusNotFound, apierr.CodeNotFound},
		"another account's target":          {owner.Id, from.Id, foreign.Id, []string{mine.Id}, http.StatusNotFound, apierr.CodeNotFound},
		"unknown target":                    {owner.Id, from.Id, "missing", []string{mine.Id}, http.StatusNotFound, apierr.CodeNotFound},
		"unknown source":                    {owner.Id, "", to.Id, []string{mine.Id}, http.StatusNotFound, apierr.CodeNotFound},
		"a subscription of another profile": {owner.Id, from.Id, to.Id, []string{mine.Id, sibling.Id}, http.StatusNotFound, apierr.CodeNotFound},
		"a subscription of another account": {owner.Id, from.Id, to.Id, []string{theirs.Id}, http.StatusNotFound, apierr.CodeNotFound},
		"the same profile":                  {owner.Id, from.Id, from.Id, []string{mine.Id}, http.StatusBadRequest, apierr.CodeInvalidRequest},
		"nothing to move":                   {owner.Id, from.Id, to.Id, []string{" ", ""}, http.StatusBadRequest, apierr.CodeInvalidRequest},
	} {
		if _, err := f.service.Move(test.user, test.from, test.to, test.ids, false); err == nil {
			t.Errorf("%s: moved", name)
		} else {
			requireCode(t, err, test.status, test.code)
		}
	}
	if f.profileOf(t, mine.Id) != from.Id || f.profileOf(t, sibling.Id) != elsewhere.Id || f.profileOf(t, theirs.Id) != foreign.Id {
		t.Fatal("a refused move changed a subscription")
	}
}

func TestMoveMergesWhatTheTargetAlreadyFollows(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "move-merge@example.test")
	from, to := f.profile(t, owner, "From"), f.profile(t, owner, "To")
	shared := f.channel(t, alpha, "merge-shared", "Shared")
	dup := f.record(t, subscriptionsCollection, map[string]any{"profile": from.Id, "channel": shared.Id})
	twin := f.record(t, subscriptionsCollection, map[string]any{"profile": to.Id, "channel": shared.Id})
	fresh := f.subscribe(t, from, alpha, "merge-fresh", "Fresh")

	result, err := f.service.Move(owner.Id, from.Id, to.Id, []string{dup.Id, fresh.Id}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved != 1 || result.Merged != 1 || !slices.Equal(result.MovedIDs, []string{fresh.Id}) || !slices.Equal(result.MergedIDs, []string{twin.Id}) {
		t.Fatalf("result: %+v", result)
	}
	if f.exists(subscriptionsCollection, dup.Id) {
		t.Fatal("the merged source row was kept")
	}
	if n := f.count(t, subscriptionsCollection, dbx.HashExp{"profile": to.Id, "channel": shared.Id}); n != 1 {
		t.Fatalf("the target holds %d subscriptions to the shared channel", n)
	}
	if !f.exists(subscriptionsCollection, twin.Id) || f.profileOf(t, fresh.Id) != to.Id {
		t.Fatal("the target's own row must stay and the other subscription must arrive")
	}
}

func TestMoveCarriesAWholeCreatorAndSplitsAPartialOne(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "move-groups@example.test")
	from, to := f.profile(t, owner, "From"), f.profile(t, owner, "To")
	wholeA, wholeB := f.subscribe(t, from, alpha, "group-a", "Whole"), f.subscribe(t, from, beta, "group-b", "Whole")
	whole := f.link(t, from, "Whole", wholeA, wholeB)
	// A group of three: moving one member splits it, moving two leaves a single member whose link is pruned.
	partA, partB, partC := f.subscribe(t, from, alpha, "part-a", "Part"), f.subscribe(t, from, beta, "part-b", "Part"), f.subscribe(t, from, strict, "ok-part-c", "Part")
	part := f.link(t, from, "Part", partA, partB, partC)
	// A group whose remainder keeps two members.
	keepA, keepB, keepC := f.subscribe(t, from, alpha, "keep-a", "Keep"), f.subscribe(t, from, beta, "keep-b", "Keep"), f.subscribe(t, from, strict, "ok-keep-c", "Keep")
	keep := f.link(t, from, "Keep", keepA, keepB, keepC)

	result, err := f.service.Move(owner.Id, from.Id, to.Id, []string{wholeA.Id, wholeB.Id, partA.Id, partB.Id, keepA.Id}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved != 5 || result.Merged != 0 {
		t.Fatalf("result: %+v", result)
	}
	if f.creator(t, wholeA.Id) != whole || f.creator(t, wholeB.Id) != whole || f.creatorProfile(t, whole) != to.Id {
		t.Fatal("a creator whose members all move goes along with them")
	}
	for _, id := range []string{partA.Id, partB.Id, keepA.Id} {
		if f.creator(t, id) != "" || f.profileOf(t, id) != to.Id {
			t.Fatalf("moved member %s must leave its group: creator %q", id, f.creator(t, id))
		}
	}
	if f.exists(creatorsCollection, part) || f.creator(t, partC.Id) != "" {
		t.Fatal("the creator left with one member must be pruned")
	}
	if !f.exists(creatorsCollection, keep) || f.creatorProfile(t, keep) != from.Id || f.creator(t, keepB.Id) != keep || f.creator(t, keepC.Id) != keep {
		t.Fatal("the group that keeps two members stays on the source")
	}
}

func TestMoveAdoptsAnUngroupedTwinIntoTheCreatorThatTravels(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "move-twin@example.test")
	from, to := f.profile(t, owner, "From"), f.profile(t, owner, "To")
	a, b := f.subscribe(t, from, alpha, "twin-a", "Twin"), f.subscribe(t, from, beta, "twin-b", "Twin")
	creator := f.link(t, from, "Twin", a, b)
	twin := f.record(t, subscriptionsCollection, map[string]any{"profile": to.Id, "channel": a.GetString("channel")})

	result, err := f.service.Move(owner.Id, from.Id, to.Id, []string{a.Id, b.Id}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved != 1 || result.Merged != 1 {
		t.Fatalf("result: %+v", result)
	}
	if f.creator(t, twin.Id) != creator || f.creator(t, b.Id) != creator || f.creatorProfile(t, creator) != to.Id {
		t.Fatal("the twin joins the creator that travels")
	}
}

func TestMoveCopyLeavesTheSourceAndBuildsAnEquivalentGroupOnTheTarget(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "move-copy@example.test")
	from, to := f.profile(t, owner, "From"), f.profile(t, owner, "To")
	a, b := f.subscribe(t, from, alpha, "copy-a", "Copy"), f.subscribe(t, from, beta, "copy-b", "Copy")
	creator := f.link(t, from, "Copy", a, b)
	loose := f.subscribe(t, from, alpha, "copy-c", "Loose")

	result, err := f.service.Move(owner.Id, from.Id, to.Id, []string{a.Id, b.Id, loose.Id}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved != 3 || result.Merged != 0 {
		t.Fatalf("result: %+v", result)
	}
	for _, id := range []string{a.Id, b.Id, loose.Id} {
		if f.profileOf(t, id) != from.Id {
			t.Fatalf("copying moved %s", id)
		}
	}
	if f.creator(t, a.Id) != creator || f.creatorProfile(t, creator) != from.Id {
		t.Fatal("copying changed the source group")
	}
	copied := f.creator(t, result.MovedIDs[0])
	if copied == "" || copied == creator || f.creatorProfile(t, copied) != to.Id || f.creatorName(t, copied) != "Copy" || f.creator(t, result.MovedIDs[1]) != copied {
		t.Fatalf("the copied group: %q", copied)
	}
	if f.creator(t, result.MovedIDs[2]) != "" || f.count(t, subscriptionsCollection, dbx.HashExp{"profile": to.Id}) != 3 {
		t.Fatal("copied rows")
	}
}

func TestMoveIsAtomic(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "move-atomic@example.test")
	from, to := f.profile(t, owner, "From"), f.profile(t, owner, "To")
	a, b := f.subscribe(t, from, alpha, "atomic-a", "Atomic"), f.subscribe(t, from, beta, "atomic-b", "Atomic")
	creator := f.link(t, from, "Atomic", a, b)
	c, d := f.subscribe(t, from, alpha, "atomic-c", "Charlie"), f.subscribe(t, from, alpha, "atomic-d", "Delta")
	failing := errors.New("injected failure")
	f.app.OnRecordUpdate(subscriptionsCollection).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.Id == d.Id {
			return failing
		}
		return e.Next()
	})

	if _, err := f.service.Move(owner.Id, from.Id, to.Id, []string{a.Id, b.Id, c.Id, d.Id}, false); err == nil {
		t.Fatal("the move succeeded")
	} else {
		requireCode(t, err, http.StatusInternalServerError, "")
	}
	for _, id := range []string{a.Id, b.Id, c.Id, d.Id} {
		if f.profileOf(t, id) != from.Id {
			t.Fatalf("subscription %s moved although the move failed", id)
		}
	}
	if f.creatorProfile(t, creator) != from.Id || f.creator(t, a.Id) != creator {
		t.Fatal("the creator moved although the move failed")
	}
}
