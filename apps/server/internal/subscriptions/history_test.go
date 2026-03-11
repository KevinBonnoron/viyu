package subscriptions

import (
	"net/http"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

func (f *fixture) watched(t testing.TB, profile *core.Record, id string) {
	t.Helper()
	video := f.record(t, "videos", map[string]any{"platform": alpha, "platform_id": id, "expires_at": "2030-01-01 00:00:00.000Z"})
	f.record(t, historyCollection, map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z", "duration_watched": 60})
}

func TestClearHistoryDeletesOnlyTheOwnedProfile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, other := f.user(t, "history-owner@example.test"), f.user(t, "history-other@example.test")
	profile, sibling, foreign := f.profile(t, owner, "Mine"), f.profile(t, owner, "Sibling"), f.profile(t, other, "Foreign")
	f.watched(t, profile, "first")
	f.watched(t, profile, "second")
	f.watched(t, sibling, "sibling")
	f.watched(t, foreign, "foreign")

	for name, test := range map[string]struct{ user, profile string }{
		"unknown profile": {owner.Id, "not-a-profile"},
		"foreign profile": {owner.Id, foreign.Id},
		"empty profile":   {owner.Id, ""},
	} {
		if err := f.service.ClearHistory(test.user, test.profile); err == nil {
			t.Errorf("%s: cleared", name)
		} else {
			requireCode(t, err, http.StatusNotFound, apierr.CodeNotFound)
		}
	}
	count := func(profileID string) int {
		return f.count(t, historyCollection, dbx.HashExp{"profile": profileID})
	}
	if count(profile.Id) != 2 || count(foreign.Id) != 1 {
		t.Fatal("a refused request deleted history")
	}

	// Each entry is deleted as a record, so realtime subscribers hear about every one.
	var deleted atomic.Int32
	f.app.OnRecordAfterDeleteSuccess(historyCollection).BindFunc(func(e *core.RecordEvent) error {
		deleted.Add(1)
		return e.Next()
	})
	if err := f.service.ClearHistory(owner.Id, profile.Id); err != nil {
		t.Fatal(err)
	}
	if got := count(profile.Id); got != 0 {
		t.Fatalf("cleared profile still has %d entries", got)
	}
	if got := deleted.Load(); got != 2 {
		t.Fatalf("%d delete events, want one per entry", got)
	}
	if count(sibling.Id) != 1 || count(foreign.Id) != 1 {
		t.Fatal("another profile lost its history")
	}
	// Clearing again is a no-op, not an error.
	if err := f.service.ClearHistory(owner.Id, profile.Id); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}
