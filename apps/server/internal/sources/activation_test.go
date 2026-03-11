package sources

import (
	"context"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

func catalogOf(t *testing.T, f *fixture, userID string) map[string]Source {
	t.Helper()
	catalog, err := f.svc.List(userID)
	if err != nil {
		t.Fatal(err)
	}
	byPlatform := map[string]Source{}
	for _, source := range catalog {
		byPlatform[source.Platform] = source
	}
	return byPlatform
}

func TestActivationDefaultsToThePlatformsChoiceAndBelongsToTheAccount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := newUser(t, f.app, "activation-owner@example.test")
	other := newUser(t, f.app, "activation-other@example.test")

	for id, source := range catalogOf(t, f, owner.Id) {
		// Two platforms are on until chosen otherwise; the third is opt-in.
		if source.Enabled != (id != gammaID) || source.Connected {
			t.Fatalf("%s starts with its default and unconnected: %+v", id, source)
		}
	}

	if err := f.svc.SetEnabled(owner.Id, betaID, false); err != nil {
		t.Fatal(err)
	}
	changed, err := f.svc.Current(owner.Id, betaID)
	if err != nil || changed.Platform != betaID || changed.Enabled {
		t.Fatalf("activation answer: %+v %v", changed, err)
	}
	owned := catalogOf(t, f, owner.Id)
	if owned[betaID].Enabled || !owned[alphaID].Enabled || owned[gammaID].Enabled {
		t.Fatalf("only beta is off for its owner: %+v", owned)
	}
	if elsewhere := catalogOf(t, f, other.Id); !elsewhere[betaID].Enabled {
		t.Fatalf("another account's choice leaked: %+v", elsewhere)
	}

	// Choosing again updates the same choice instead of adding a second one, and an opt-in source can be turned on.
	if err := f.svc.SetEnabled(owner.Id, betaID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetEnabled(owner.Id, gammaID, true); err != nil {
		t.Fatal(err)
	}
	owned = catalogOf(t, f, owner.Id)
	if !owned[betaID].Enabled || !owned[gammaID].Enabled {
		t.Fatalf("the choices were not recorded: %+v", owned)
	}
	if rows, err := f.app.FindRecordsByFilter(ActivationsCollection, "user = {:user} && platform = {:platform}", "", 0, 0, map[string]any{"user": owner.Id, "platform": betaID}); err != nil || len(rows) != 1 {
		t.Fatalf("one row per account and platform: %d %v", len(rows), err)
	}

	assertCode(t, f.svc.SetEnabled(owner.Id, "vimeo", true), apierr.CodeInvalidSourceArg)
	_, err = f.svc.Current(owner.Id, "vimeo")
	assertCode(t, err, apierr.CodeInvalidSourceArg)

	active, err := f.svc.Activations(owner.Id)
	if err != nil || len(active) != 3 || !active[alphaID] || !active[betaID] || !active[gammaID] {
		t.Fatalf("activations: %v %v", active, err)
	}
}

func TestRequireEnabledAppliesTheAccountsChoice(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "require@example.test")
	if err := f.svc.RequireEnabled(user.Id, alphaID); err != nil {
		t.Fatal(err)
	}
	assertCode(t, f.svc.RequireEnabled(user.Id, gammaID), apierr.CodeDisabled)
	if err := f.svc.SetEnabled(user.Id, alphaID, false); err != nil {
		t.Fatal(err)
	}
	assertCode(t, f.svc.RequireEnabled(user.Id, alphaID), apierr.CodeDisabled)
	// A platform nobody registered is not an activation: callers refuse it elsewhere.
	if err := f.svc.RequireEnabled(user.Id, "unknown"); err != nil {
		t.Fatal(err)
	}
}

func TestKeepEnabledDropsTheChannelsOfPlatformsTheAccountTurnedOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "keep@example.test")
	if err := f.svc.SetEnabled(user.Id, betaID, false); err != nil {
		t.Fatal(err)
	}
	collection, err := f.app.FindCollectionByNameOrId("channels")
	if err != nil {
		t.Fatal(err)
	}
	var channels []*core.Record
	for _, id := range []string{alphaID, betaID, gammaID, "unknown"} {
		channel := core.NewRecord(collection)
		channel.Set("platform", id)
		channels = append(channels, channel)
	}
	kept, err := f.svc.KeepEnabled(user.Id, channels)
	if err != nil || len(kept) != 1 || kept[0].GetString("platform") != alphaID {
		t.Fatalf("kept: %v %v", kept, err)
	}
}

func TestConnectionNeedsAnEnabledSourceAndSurvivesTurningItOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "connection-activation@example.test")
	ctx := context.Background()

	f.connect(t, user.Id, alphaID, "private-cookie-value")
	if source := catalogOf(t, f, user.Id)[alphaID]; !source.Connected || !source.Enabled {
		t.Fatalf("connected and enabled: %+v", source)
	}
	if err := f.svc.SetEnabled(user.Id, alphaID, false); err != nil {
		t.Fatal(err)
	}
	source := catalogOf(t, f, user.Id)[alphaID]
	if !source.Connected || source.Enabled || source.ConnectionID == "" {
		t.Fatalf("turning a source off keeps its connection: %+v", source)
	}
	validated, feeds, watched := f.alpha.calls()
	_, err := f.svc.Connect(ctx, user.Id, alphaID, fakeCookies("private-cookie-value"), "")
	assertCode(t, err, apierr.CodeDisabled)
	_, err = f.svc.Check(ctx, user.Id, alphaID)
	assertCode(t, err, apierr.CodeDisabled)
	_, err = f.svc.Feed(ctx, user.Id, alphaID, "home", 1, 10)
	assertCode(t, err, apierr.CodeDisabled)
	assertCode(t, f.svc.Watch(ctx, user.Id, alphaID, "vid-1", 30, 120), apierr.CodeDisabled)
	if v, fe, w := f.alpha.calls(); v != validated || fe != feeds || w != watched {
		t.Fatal("a disabled source was still reached")
	}

	if err := f.svc.SetEnabled(user.Id, alphaID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Check(ctx, user.Id, alphaID); err != nil {
		t.Fatalf("the connection stopped working after being turned back on: %v", err)
	}

	// The stored credentials can still be dropped while the source is off.
	if err := f.svc.SetEnabled(user.Id, alphaID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Disconnect(user.Id, alphaID); err != nil {
		t.Fatal(err)
	}
	if source = catalogOf(t, f, user.Id)[alphaID]; source.Connected || source.Enabled {
		t.Fatalf("disconnected and still off: %+v", source)
	}
}

func TestWriteActivationRunsInTheCallersTransaction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "transaction@example.test")
	err := f.app.RunInTransaction(func(tx core.App) error {
		if err := WriteActivation(tx, user.Id, alphaID, false); err != nil {
			return err
		}
		return WriteActivation(tx, user.Id, alphaID, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := f.app.FindRecordsByFilter(ActivationsCollection, "user = {:user}", "", 0, 0, map[string]any{"user": user.Id}); err != nil || len(rows) != 1 || rows[0].GetBool("enabled") {
		t.Fatalf("activation rows: %d %v", len(rows), err)
	}
}

func TestPlatformInUseStopsOnlyWhenEveryAccountTurnedItOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if !f.svc.PlatformInUse(betaID) {
		t.Fatal("a server with no account has nobody who turned anything off")
	}
	if f.svc.PlatformInUse(gammaID) {
		t.Fatal("an opt-in platform nobody chose is not in use")
	}
	if f.svc.PlatformInUse("unknown") {
		t.Fatal("an unknown platform cannot be in use")
	}
	first := newUser(t, f.app, "in-use-first@example.test")
	second := newUser(t, f.app, "in-use-second@example.test")
	if err := f.svc.SetEnabled(first.Id, betaID, false); err != nil {
		t.Fatal(err)
	}
	if !f.svc.PlatformInUse(betaID) {
		t.Fatal("one account still uses beta")
	}
	if err := f.svc.SetEnabled(second.Id, betaID, false); err != nil {
		t.Fatal(err)
	}
	if f.svc.PlatformInUse(betaID) {
		t.Fatal("every account turned beta off")
	}
	if !f.svc.PlatformInUse(alphaID) {
		t.Fatal("alpha is untouched")
	}
	if got := strings.Join(f.svc.PlatformsInUse(), ","); got != alphaID {
		t.Fatalf("platforms in use: %q", got)
	}

	if err := f.svc.SetEnabled(first.Id, gammaID, true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.svc.PlatformsInUse(), ","); got != alphaID+","+gammaID {
		t.Fatalf("an opt-in platform one account chose is in use: %q", got)
	}
}
