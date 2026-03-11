package catalog

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

func canView(t *testing.T, app core.App, collection string, record, auth *core.Record) bool {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := app.CanAccessRecord(record, &core.RequestInfo{Auth: auth}, c.ViewRule)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestDeletingAnAccountDeletesWhatOnlyItOwnedAndKeepsSharedRecordsPrivate(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	a := testUser(t, app, "owner-a@example.test")
	b := testUser(t, app, "owner-b@example.test")
	c := testUser(t, app, "owner-c@example.test")
	mustStore(t, s, listing(alpha, "solo"), a.Id)
	mustStore(t, s, listing(alpha, "shared"), a.Id)
	mustStore(t, s, listing(alpha, "shared"), b.Id)
	mustStore(t, s, listing(alpha, "public"), "")
	// A subscription of the account's own profile must not stop its private channel from being removed.
	profile := testProfile(t, app, a, "Mine")
	soloChannel := mustFindChannel(t, s, alpha, "UCsolo")
	soloVideo := mustFindVideo(t, s, alpha, "solo")
	testRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": soloChannel.Id})
	testRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": soloVideo.Id, "watched_at": "2026-01-01 00:00:00.000Z"})

	if err := app.Delete(a); err != nil {
		t.Fatalf("deleting the account: %v", err)
	}

	for _, id := range []string{"solo"} {
		if _, err := s.FindVideo(alpha, id); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("video %s outlived its only owner: %v", id, err)
		}
		if _, err := s.FindChannel(alpha, "UC"+id); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("channel UC%s outlived its only owner: %v", id, err)
		}
	}
	for _, id := range []string{"shared", "public"} {
		if _, err := s.FindVideo(alpha, id); err != nil {
			t.Errorf("video %s was deleted: %v", id, err)
		}
		if _, err := s.FindChannel(alpha, "UC"+id); err != nil {
			t.Errorf("channel UC%s was deleted: %v", id, err)
		}
	}

	shared := map[string]*core.Record{"videos": mustFindVideo(t, s, alpha, "shared"), "channels": mustFindChannel(t, s, alpha, "UCshared")}
	for collection, record := range shared {
		if owners := record.GetStringSlice("owners"); !slices.Equal(owners, []string{b.Id}) {
			t.Errorf("shared %s owners = %v, want only %s", collection, owners, b.Id)
		}
		for label, tc := range map[string]struct {
			auth *core.Record
			want bool
		}{"remaining owner": {b, true}, "another account": {c, false}, "anonymous": {nil, false}} {
			if got := canView(t, app, collection, record, tc.auth); got != tc.want {
				t.Errorf("shared %s visible to %s = %v, want %v", collection, label, got, tc.want)
			}
		}
	}
}

// A private channel that another account's profile subscribes to cannot be deleted: it stays, private to
// the account that still has a claim on it.
func TestDeletingAnAccountKeepsAPrivateChannelAnotherAccountSubscribesTo(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	a := testUser(t, app, "keeper-a@example.test")
	b := testUser(t, app, "keeper-b@example.test")
	mustStore(t, s, listing(alpha, "contested"), a.Id)
	channel := mustFindChannel(t, s, alpha, "UCcontested")
	testRecord(t, app, "subscriptions", map[string]any{"profile": testProfile(t, app, b, "Theirs").Id, "channel": channel.Id})

	if err := app.Delete(a); err != nil {
		t.Fatalf("deleting the account: %v", err)
	}
	if _, err := s.FindChannel(alpha, "UCcontested"); err != nil {
		t.Fatalf("a channel another account subscribes to was deleted: %v", err)
	}
}
