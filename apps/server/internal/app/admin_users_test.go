package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// admUser creates an account with the role the way trusted code does and returns it with a token.
func admUser(t *testing.T, app core.App, email, role string) (*core.Record, string) {
	t.Helper()
	record := testUser(t, app, email)
	if role != "user" {
		record.Set("role", role)
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	return record, tokenOf(t, record)
}

// admFields fails unless the JSON object has exactly these fields: an administration answer never carries a
// credential, a token key or anything else a record holds.
func admFields(t *testing.T, data []byte, fields ...string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.Strings(fields)
	if !reflect.DeepEqual(keys, fields) {
		t.Fatalf("fields=%v, want only %v: %s", keys, fields, data)
	}
}

func TestAdminEveryRouteRequiresAFreshAdministrator(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	_, ordinary := admUser(t, app, "ordinary@example.test", "user")
	stale, staleToken := admUser(t, app, "stale@example.test", "admin")
	stale.Set("role", "user")
	if err := app.Save(stale); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/users"},
		{http.MethodPost, "/api/admin/users"},
		{http.MethodPatch, "/api/admin/users/missing"},
		{http.MethodDelete, "/api/admin/users/missing"},
		{http.MethodGet, "/api/admin/registration"},
		{http.MethodPatch, "/api/admin/registration"},
		{http.MethodGet, "/api/admin/series-ocr"},
		{http.MethodPatch, "/api/admin/series-ocr"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			if res := send(t, handler, route.method, route.path, "", "{}"); res.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous: status=%d %s", res.Code, res.Body)
			}
			for _, denied := range []string{ordinary, staleToken} {
				acctExpect(t, send(t, handler, route.method, route.path, denied, "{}"), http.StatusForbidden, "ADMIN_REQUIRED")
			}
		})
	}
}

func TestAdminUserCRUDValidationAndPublicShape(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	_, token := admUser(t, app, "admin@example.test", "admin")

	// The body is what the route knows about a user, nothing more: a field that would reach the token key or
	// the verification flag is refused instead of being ignored.
	forged := acctJSON(t, map[string]any{"name": "Ada", "email": "forged@example.test", "role": "user", "password": "new-user-password-123", "passwordConfirm": "new-user-password-123", "verified": true, "tokenKey": "injected-secret"})
	acctExpect(t, send(t, handler, http.MethodPost, "/api/admin/users", token, forged), http.StatusBadRequest, "ADMIN_VALIDATION")
	if _, err := app.FindAuthRecordByEmail("users", "forged@example.test"); err == nil {
		t.Fatal("a refused creation persisted a user")
	}

	res := send(t, handler, http.MethodPost, "/api/admin/users", token, acctJSON(t, map[string]any{"name": "  Ada  ", "email": "ada@example.test", "role": "user", "password": "new-user-password-123", "passwordConfirm": "new-user-password-123"}))
	acctExpect(t, res, http.StatusCreated, "")
	admFields(t, res.Body.Bytes(), "id", "name", "email", "role", "verified", "created")
	var created accounts.AdminUser
	acctDecode(t, res, &created)
	if created.ID == "" || created.Name != "Ada" || created.Role != "user" || created.Verified || created.Created == "" {
		t.Fatalf("unexpected created user: %+v", created)
	}
	target, err := app.FindRecordById("users", created.ID)
	if err != nil || !target.ValidatePassword("new-user-password-123") {
		t.Fatalf("invalid created user: %v", err)
	}
	profile, err := app.FindFirstRecordByFilter("profiles", "user = {:user}", dbx.Params{"user": created.ID})
	if err != nil || profile.GetString("name") != "Ada" {
		t.Fatalf("the default profile was not created by the users hook: %v", err)
	}

	path := "/api/admin/users/" + created.ID
	for _, invalid := range []map[string]any{
		{"role": "superuser"}, {"email": "not-an-email"}, {"email": "admin@example.test"},
		{"password": "only-one-field"}, {"passwordConfirm": "only-one-field"},
		{"password": "password-123", "passwordConfirm": "different-123"},
		{"password": "", "passwordConfirm": ""}, {"password": "x", "passwordConfirm": "x"},
	} {
		acctExpect(t, send(t, handler, http.MethodPatch, path, token, acctJSON(t, invalid)), http.StatusBadRequest, "ADMIN_VALIDATION")
	}
	for _, invalid := range []map[string]any{
		{"email": "missing-password@example.test", "role": "user"},
		{"email": "missing-role@example.test", "password": "password-123", "passwordConfirm": "password-123"},
	} {
		acctExpect(t, send(t, handler, http.MethodPost, "/api/admin/users", token, acctJSON(t, invalid)), http.StatusBadRequest, "ADMIN_VALIDATION")
	}
	for _, body := range []string{`not-json`, `{"role":"user"} {"role":"admin"}`} {
		acctExpect(t, send(t, handler, http.MethodPatch, path, token, body), http.StatusBadRequest, "ADMIN_VALIDATION")
	}

	// Verification follows the email: an update that leaves the address alone keeps it, a new address starts
	// unverified.
	target.SetVerified(true)
	if err := app.Save(target); err != nil {
		t.Fatal(err)
	}
	res = send(t, handler, http.MethodPatch, path, token, `{"name":"  Ada Updated ","role":"admin"}`)
	acctExpect(t, res, http.StatusOK, "")
	admFields(t, res.Body.Bytes(), "id", "name", "email", "role", "verified", "created")
	target, err = app.FindRecordById("users", created.ID)
	if err != nil || target.GetString("role") != "admin" || target.GetString("name") != "Ada Updated" || !target.Verified() || !target.ValidatePassword("new-user-password-123") {
		t.Fatalf("optional-password PATCH failed: %v", err)
	}
	res = send(t, handler, http.MethodPatch, path, token, `{"email":"ada-updated@example.test","password":"updated-password-123","passwordConfirm":"updated-password-123","role":"user"}`)
	acctExpect(t, res, http.StatusOK, "")
	target, err = app.FindRecordById("users", created.ID)
	if err != nil || target.Verified() || target.Email() != "ada-updated@example.test" || !target.ValidatePassword("updated-password-123") || target.GetString("role") != "user" {
		t.Fatalf("credential update failed: %v", err)
	}

	res = request(t, handler, "/api/admin/users?q=ada-updated", token)
	acctExpect(t, res, http.StatusOK, "")
	var listed struct {
		Items      []json.RawMessage `json:"items"`
		Page       int               `json:"page"`
		TotalPages int               `json:"totalPages"`
		TotalItems int               `json:"totalItems"`
	}
	acctDecode(t, res, &listed)
	if listed.Page != 1 || listed.TotalItems != 1 || listed.TotalPages != 1 || len(listed.Items) != 1 {
		t.Fatalf("filtered list: %s", res.Body)
	}
	admFields(t, listed.Items[0], "id", "name", "email", "role", "verified", "created")
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the user list is cacheable")
	}
	for _, page := range []string{"0", "-1", "oops", "999999999999999999999999"} {
		acctExpect(t, request(t, handler, "/api/admin/users?page="+page, token), http.StatusBadRequest, "ADMIN_VALIDATION")
	}
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		acctExpect(t, send(t, handler, method, "/api/admin/users/missing", token, `{"name":"Missing"}`), http.StatusNotFound, "ADMIN_NOT_FOUND")
	}

	// Deleting an account takes what only it owned with it.
	videoID := storedVideo(t, app, platform.Video{ID: "admin-cascade", Title: "Cascade"})
	channelID := storedChannel(t, app, testPlatforms().Default().ID(), "UCadmincascadeadmincasc", "")
	owned := []*core.Record{
		profile,
		acctRecord(t, app, "video_reactions", map[string]any{"profile": profile.Id, "video": videoID, "reaction": "like"}),
		acctRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": videoID, "watched_at": "2026-01-01 00:00:00.000Z"}),
		acctRecord(t, app, "subscriptions", map[string]any{"profile": profile.Id, "channel": channelID}),
		acctRecord(t, app, "settings", map[string]any{"user": target.Id}),
	}
	if res := send(t, handler, http.MethodDelete, path, token, ""); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("delete: %d %s", res.Code, res.Body)
	}
	for _, record := range append(owned, target) {
		if _, err := app.FindRecordById(record.Collection().Name, record.Id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("an owned record survived the deletion of its user: %s/%s (%v)", record.Collection().Name, record.Id, err)
		}
	}
	if _, err := app.FindRecordById("videos", videoID); err != nil {
		t.Fatalf("a shared video went with the user: %v", err)
	}
}

func TestAdminPaginationHasStableCreatedAndIDOrdering(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	_, token := admUser(t, app, "admin@example.test", "admin")
	for i := range 26 {
		admUser(t, app, fmt.Sprintf("paged-%02d@example.test", i), "user")
	}
	expected, err := app.FindRecordsByFilter("users", "", "created,id", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for page := 1; page <= 3; page++ {
		res := request(t, handler, fmt.Sprintf("/api/admin/users?page=%d", page), token)
		acctExpect(t, res, http.StatusOK, "")
		var body accounts.UserPage
		acctDecode(t, res, &body)
		if body.Page != page || body.TotalPages != 2 || body.TotalItems != 27 {
			t.Fatalf("pagination metadata: %s", res.Body)
		}
		want := []int{25, 2, 0}[page-1]
		if len(body.Items) != want {
			t.Fatalf("page %d contains %d records, want %d", page, len(body.Items), want)
		}
		for _, item := range body.Items {
			ids = append(ids, item.ID)
		}
	}
	for i, record := range expected {
		if ids[i] != record.Id {
			t.Fatalf("unstable ordering at %d: %s != %s", i, ids[i], record.Id)
		}
	}
	res := request(t, handler, "/api/admin/users?q=nomatch", token)
	acctExpect(t, res, http.StatusOK, "")
	if !strings.Contains(res.Body.String(), `"items":[]`) {
		t.Fatalf("an empty list must be an array: %s", res.Body)
	}
}

func TestAdminSelfAndLastAdministratorGuards(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	first, token := admUser(t, app, "first-admin@example.test", "admin")
	path := "/api/admin/users/" + first.Id
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		acctExpect(t, send(t, handler, method, path, token, `{"role":"user"}`), http.StatusConflict, "ADMIN_LAST_ADMIN")
	}
	second, secondToken := admUser(t, app, "second-admin@example.test", "admin")
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		acctExpect(t, send(t, handler, method, path, token, `{"role":"user"}`), http.StatusConflict, "ADMIN_SELF_PROTECTION")
	}
	acctExpect(t, send(t, handler, http.MethodPatch, path, token, `{"name":"Still Admin","role":"admin"}`), http.StatusOK, "")
	acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/users/"+second.Id, token, `{"role":"user"}`), http.StatusOK, "")
	acctExpect(t, request(t, handler, "/api/admin/users", secondToken), http.StatusForbidden, "ADMIN_REQUIRED")
	acctExpect(t, send(t, handler, http.MethodPatch, path, token, `{"role":"user"}`), http.StatusConflict, "ADMIN_LAST_ADMIN")
}

// Two administrators demoting each other at once: the writes are serialized and the second one finds it is
// no longer an administrator, so one always remains.
func TestAdminCompetingRoleChangesLeaveOneAdministrator(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	first, firstToken := admUser(t, app, "race-first@example.test", "admin")
	second, secondToken := admUser(t, app, "race-second@example.test", "admin")
	var requests []*http.Request
	for _, actor := range []struct{ target, token string }{{second.Id, firstToken}, {first.Id, secondToken}} {
		req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+actor.target, strings.NewReader(`{"role":"user"}`))
		req.Header.Set("Authorization", actor.token)
		req.Header.Set("Content-Type", "application/json")
		requests = append(requests, req)
	}
	responses := make([]*httptest.ResponseRecorder, len(requests))
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i, req := range requests {
		responses[i] = httptest.NewRecorder()
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			handler.ServeHTTP(responses[i], req)
		}()
	}
	close(start)
	wait.Wait()
	statuses := []int{responses[0].Code, responses[1].Code}
	sort.Ints(statuses)
	if !reflect.DeepEqual(statuses, []int{http.StatusOK, http.StatusForbidden}) {
		t.Fatalf("competing role updates: %v (%s / %s)", statuses, responses[0].Body, responses[1].Body)
	}
	if count, err := app.CountRecords("users", dbx.HashExp{"role": "admin"}); err != nil || count != 1 {
		t.Fatalf("the last administrator was lost: %d (%v)", count, err)
	}
}
