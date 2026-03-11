package accounts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// assertAPIError fails unless err is an *apierr.Error with the status and, when given, the code.
func assertAPIError(t *testing.T, err error, status int, code string) *apierr.Error {
	t.Helper()
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Status != status || (code != "" && safe.Code != code) {
		t.Fatalf("error = %v (%+v), want status %d code %q", err, safe, status, code)
	}
	return safe
}

func TestEveryAdminOperationRequiresAFreshAdministrator(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ordinary, _ := e.user(t, "ordinary@example.test", "user")
	stale, _ := e.user(t, "stale@example.test", "admin")
	stale.Set("role", "user")
	if err := e.app.Save(stale); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	operations := map[string]func(actor string) error{
		"list":   func(actor string) error { _, err := e.svc.AdminList(actor, "", ""); return err },
		"create": func(actor string) error { _, err := e.svc.AdminCreate(ctx, actor, UserInput{}); return err },
		"update": func(actor string) error { _, err := e.svc.AdminUpdate(ctx, actor, "missing", UserInput{}); return err },
		"delete": func(actor string) error { return e.svc.AdminDelete(actor, "missing") },
		"tx": func(actor string) error {
			return e.svc.AdminTx(actor, func(core.App, *core.Record) error { return nil })
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			assertAPIError(t, operation(""), http.StatusUnauthorized, "ADMIN_UNAUTHENTICATED")
			assertAPIError(t, operation("gone"), http.StatusUnauthorized, "ADMIN_UNAUTHENTICATED")
			for _, denied := range []string{ordinary.Id, stale.Id} {
				assertAPIError(t, operation(denied), http.StatusForbidden, "ADMIN_REQUIRED")
			}
		})
	}
}

func TestAdminUserCRUDValidationAndPublicShape(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	admin, _ := e.user(t, "admin@example.test", "admin")
	ctx := t.Context()

	created, err := e.svc.AdminCreate(ctx, admin.Id, UserInput{Name: new("  Ada  "), Email: new("ada@example.test"), Role: new("user"), Password: new("new-user-password-123"), PasswordConfirm: new("new-user-password-123")})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Ada" || created.Role != "user" || created.Verified || created.Created == "" || created.Email != "ada@example.test" {
		t.Fatalf("unexpected created user: %+v", created)
	}
	encoded, _ := json.Marshal(created)
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 6 {
		t.Fatalf("the public shape holds only id, name, email, role, verified and created: %s", encoded)
	}
	target, err := e.app.FindRecordById("users", created.ID)
	if err != nil || !target.ValidatePassword("new-user-password-123") || target.GetBool("emailVisibility") {
		t.Fatalf("unsafe or invalid created user: %v", err)
	}
	profile, err := e.app.FindFirstRecordByFilter("profiles", "user = {:user}", dbx.Params{"user": created.ID})
	if err != nil || profile.GetString("name") != "Ada" {
		t.Fatalf("default profile was not created by the users hook: %v", err)
	}

	for _, invalid := range []UserInput{
		{Role: new("superuser")}, {Email: new("not-an-email")}, {Email: new("admin@example.test")},
		{Password: new("only-one-field")}, {PasswordConfirm: new("only-one-field")},
		{Password: new("password-123"), PasswordConfirm: new("different-123")},
		{Password: new(""), PasswordConfirm: new("")}, {Password: new("x"), PasswordConfirm: new("x")},
	} {
		_, err := e.svc.AdminUpdate(ctx, admin.Id, created.ID, invalid)
		assertAPIError(t, err, http.StatusBadRequest, "ADMIN_VALIDATION")
	}
	_, err = e.svc.AdminUpdate(ctx, admin.Id, created.ID, UserInput{Email: new("not-an-email")})
	if safe := assertAPIError(t, err, http.StatusBadRequest, "ADMIN_VALIDATION"); safe.Data == nil {
		t.Fatal("a refused form carries its field messages")
	}
	for _, invalid := range []UserInput{
		{Email: new("missing-password@example.test"), Role: new("user")},
		{Email: new("missing-role@example.test"), Password: new("password-123"), PasswordConfirm: new("password-123")},
	} {
		_, err := e.svc.AdminCreate(ctx, admin.Id, invalid)
		assertAPIError(t, err, http.StatusBadRequest, "ADMIN_VALIDATION")
	}

	target.SetVerified(true)
	if err := e.app.Save(target); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AdminUpdate(ctx, admin.Id, created.ID, UserInput{Name: new("  Ada Updated "), Role: new("admin")}); err != nil {
		t.Fatal(err)
	}
	target, err = e.app.FindRecordById("users", created.ID)
	if err != nil || target.GetString("role") != "admin" || target.GetString("name") != "Ada Updated" || !target.Verified() || !target.ValidatePassword("new-user-password-123") {
		t.Fatalf("optional-password update failed: %v", err)
	}
	if _, err := e.svc.AdminUpdate(ctx, admin.Id, created.ID, UserInput{Email: new("ada-updated@example.test"), Password: new("updated-password-123"), PasswordConfirm: new("updated-password-123"), Role: new("user")}); err != nil {
		t.Fatal(err)
	}
	target, err = e.app.FindRecordById("users", created.ID)
	if err != nil || target.Verified() || target.Email() != "ada-updated@example.test" || !target.ValidatePassword("updated-password-123") || target.GetString("role") != "user" {
		t.Fatalf("credential update failed: %v", err)
	}

	page, err := e.svc.AdminList(admin.Id, "", "ada-updated")
	if err != nil || page.Page != 1 || page.TotalItems != 1 || page.TotalPages != 1 || len(page.Items) != 1 {
		t.Fatalf("filtered list: %+v (%v)", page, err)
	}
	for _, bad := range []string{"0", "-1", "oops", "999999999999999999999999"} {
		_, err := e.svc.AdminList(admin.Id, bad, "")
		assertAPIError(t, err, http.StatusBadRequest, "ADMIN_VALIDATION")
	}
	_, err = e.svc.AdminUpdate(ctx, admin.Id, "missing", UserInput{Name: new("Missing")})
	assertAPIError(t, err, http.StatusNotFound, "ADMIN_NOT_FOUND")
	assertAPIError(t, e.svc.AdminDelete(admin.Id, "missing"), http.StatusNotFound, "ADMIN_NOT_FOUND")

	video := e.video(t, "admin-cascade-video")
	owned := []*core.Record{
		profile,
		e.record(t, "video_reactions", map[string]any{"profile": profile.Id, "video": video.Id, "reaction": "like"}),
		e.record(t, "watch_history", map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z"}),
		e.record(t, "subscriptions", map[string]any{"profile": profile.Id, "channel": video.GetString("channel")}),
		e.record(t, "settings", map[string]any{"user": target.Id}),
	}
	if err := e.svc.AdminDelete(admin.Id, created.ID); err != nil {
		t.Fatal(err)
	}
	for _, record := range append(owned, target) {
		if _, err := e.app.FindRecordById(record.Collection().Name, record.Id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("owned record survived user deletion: %s/%s (%v)", record.Collection().Name, record.Id, err)
		}
	}
}

func TestAdminPaginationHasStableCreatedAndIDOrdering(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	admin, _ := e.user(t, "admin@example.test", "admin")
	for i := range 26 {
		e.user(t, fmt.Sprintf("paged-%02d@example.test", i), "user")
	}
	expected, err := e.app.FindRecordsByFilter("users", "", "created,id", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for page := 1; page <= 3; page++ {
		got, err := e.svc.AdminList(admin.Id, fmt.Sprint(page), "")
		if err != nil || got.Page != page || got.TotalPages != 2 || got.TotalItems != 27 {
			t.Fatalf("pagination metadata: %+v (%v)", got, err)
		}
		want := map[int]int{1: 25, 2: 2, 3: 0}[page]
		if len(got.Items) != want {
			t.Fatalf("page %d contains %d records, want %d", page, len(got.Items), want)
		}
		for _, item := range got.Items {
			ids = append(ids, item.ID)
		}
	}
	for i, record := range expected {
		if ids[i] != record.Id {
			t.Fatalf("unstable ordering at %d: %s != %s", i, ids[i], record.Id)
		}
	}
	none, err := e.svc.AdminList(admin.Id, "", "nomatch")
	if err != nil || none.Items == nil || len(none.Items) != 0 {
		t.Fatalf("an empty list must be an empty array: %+v %v", none, err)
	}
	encoded, _ := json.Marshal(none)
	if string(encoded) != `{"items":[],"page":1,"totalPages":0,"totalItems":0}` {
		t.Fatalf("empty page JSON: %s", encoded)
	}
}

func TestAdminSelfAndLastAdministratorGuards(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	first, _ := e.user(t, "first-admin@example.test", "admin")
	demote := UserInput{Role: new("user")}

	_, err := e.svc.AdminUpdate(ctx, first.Id, first.Id, demote)
	assertAPIError(t, err, http.StatusConflict, "ADMIN_LAST_ADMIN")
	assertAPIError(t, e.svc.AdminDelete(first.Id, first.Id), http.StatusConflict, "ADMIN_LAST_ADMIN")

	second, _ := e.user(t, "second-admin@example.test", "admin")
	_, err = e.svc.AdminUpdate(ctx, first.Id, first.Id, demote)
	assertAPIError(t, err, http.StatusConflict, "ADMIN_SELF_PROTECTION")
	assertAPIError(t, e.svc.AdminDelete(first.Id, first.Id), http.StatusConflict, "ADMIN_SELF_PROTECTION")
	if _, err := e.svc.AdminUpdate(ctx, first.Id, first.Id, UserInput{Name: new("Still Admin"), Role: new("admin")}); err != nil {
		t.Fatalf("renaming oneself: %v", err)
	}
	if _, err := e.svc.AdminUpdate(ctx, first.Id, second.Id, demote); err != nil {
		t.Fatalf("demoting another administrator: %v", err)
	}
	assertAPIError(t, e.svc.RequireAdmin(second.Id), http.StatusForbidden, "ADMIN_REQUIRED")
	_, err = e.svc.AdminUpdate(ctx, first.Id, first.Id, demote)
	assertAPIError(t, err, http.StatusConflict, "ADMIN_LAST_ADMIN")
}

func TestAdminTransactionsSerializeCompetingRoleChanges(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	first, _ := e.user(t, "race-first@example.test", "admin")
	second, _ := e.user(t, "race-second@example.test", "admin")
	ctx := t.Context()
	results := make([]error, 2)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i, pair := range [][2]string{{first.Id, second.Id}, {second.Id, first.Id}} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, results[i] = e.svc.AdminUpdate(ctx, pair[0], pair[1], UserInput{Role: new("user")})
		}()
	}
	close(start)
	wait.Wait()
	var statuses []int
	for _, err := range results {
		if err == nil {
			statuses = append(statuses, http.StatusOK)
			continue
		}
		statuses = append(statuses, assertAPIError(t, err, http.StatusForbidden, "ADMIN_REQUIRED").Status)
	}
	sort.Ints(statuses)
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusForbidden {
		t.Fatalf("competing role updates: %v", results)
	}
	count, err := e.app.CountRecords("users", dbx.HashExp{"role": "admin"})
	if err != nil || count != 1 {
		t.Fatalf("last administrator lost: %d (%v)", count, err)
	}
}

func TestAdminTxRunsItsWorkInTheTransactionAndRollsBack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	admin, _ := e.user(t, "tx-admin@example.test", "admin")
	boom := errors.New("boom")
	err := e.svc.AdminTx(admin.Id, func(tx core.App, actor *core.Record) error {
		if actor.Id != admin.Id {
			t.Errorf("actor = %s", actor.Id)
		}
		if _, err := tx.DB().NewQuery("UPDATE {{app_settings}} SET [[registration_enabled]] = 0 WHERE [[id]] = 1").Execute(); err != nil {
			return err
		}
		return boom
	})
	if safe := assertAPIError(t, err, http.StatusInternalServerError, "INTERNAL_ERROR"); !errors.Is(safe, boom) {
		t.Fatalf("the cause is kept for the log: %v", safe)
	}
	if enabled, err := e.svc.RegistrationEnabled(); err != nil || !enabled {
		t.Fatalf("a failed administration action was not rolled back: %v %v", enabled, err)
	}
	refused := AdminInvalid("nope", nil)
	if err := e.svc.AdminTx(admin.Id, func(core.App, *core.Record) error { return refused }); err != refused {
		t.Fatalf("an API error passes through unchanged: %v", err)
	}
}
