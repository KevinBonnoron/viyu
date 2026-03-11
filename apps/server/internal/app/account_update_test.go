package app

import (
	"net/http"
	"testing"
)

// The service rules of a self-service update are tested in accounts; this is the route: the body it accepts,
// the error it answers and the fresh session a new password issues.
func TestAccountUpdatesRequireOwnerProof(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	user := testUser(t, app, "update@example.test")
	token := tokenOf(t, user)

	if res := send(t, handler, http.MethodPatch, "/api/account", token, `{"name":"Ada"}`); res.Code != http.StatusOK {
		t.Fatalf("profile update: %d %s", res.Code, res.Body)
	}
	res := send(t, handler, http.MethodPatch, "/api/account", token, `{"password":"new-safe-password-123","passwordConfirm":"new-safe-password-123","oldPassword":"wrong"}`)
	acctExpect(t, res, http.StatusBadRequest, "INVALID_REQUEST")
	if fresh, err := app.FindRecordById("users", user.Id); err != nil || fresh.GetString("name") != "Ada" || !fresh.ValidatePassword("test-password-0123456789") {
		t.Fatalf("a rejected update changed the account: %v", err)
	}

	res = send(t, handler, http.MethodPatch, "/api/account", token, `{"oldPassword":"test-password-0123456789","password":"new-safe-password-123","passwordConfirm":"new-safe-password-123"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("credential update: %d %s", res.Code, res.Body)
	}
	var auth struct {
		Token  string `json:"token"`
		Record struct {
			Email string `json:"email"`
		} `json:"record"`
	}
	acctDecode(t, res, &auth)
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a fresh session is cacheable: %q", res.Header().Get("Cache-Control"))
	}
	fresh, err := app.FindRecordById("users", user.Id)
	if err != nil || !fresh.ValidatePassword("new-safe-password-123") || auth.Record.Email != "update@example.test" || auth.Token == "" {
		t.Fatalf("credential update did not persist: %v %s", err, res.Body)
	}
	if response := request(t, handler, "/api/account", auth.Token); response.Code != http.StatusOK {
		t.Fatalf("the session issued by the update was rejected: %d", response.Code)
	}
	if response := request(t, handler, "/api/account", token); response.Code != http.StatusUnauthorized {
		t.Fatalf("the old token survived the password update: %d", response.Code)
	}
}

func TestAccountCannotChangeEmailOrRoleThroughTheRoute(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	user := testUser(t, app, "original@example.test")
	token := tokenOf(t, user)

	res := send(t, handler, http.MethodPatch, "/api/account", token, `{"email":"new@example.test","oldPassword":"test-password-0123456789"}`)
	acctExpect(t, res, http.StatusBadRequest, "INVALID_REQUEST")
	if _, err := app.FindAuthRecordByEmail("users", "original@example.test"); err != nil {
		t.Fatalf("the original email was changed: %v", err)
	}
	if _, err := app.FindAuthRecordByEmail("users", "new@example.test"); err == nil {
		t.Fatal("the new email persisted after the refusal")
	}
	// A field the route does not know is refused, not silently ignored: the role in particular is never
	// reachable from here.
	res = send(t, handler, http.MethodPatch, "/api/account", token, `{"name":"Renamed","role":"admin"}`)
	acctExpect(t, res, http.StatusBadRequest, "INVALID_REQUEST")
	fresh, err := app.FindRecordById("users", user.Id)
	if err != nil || fresh.GetString("role") != "user" || fresh.GetString("name") == "Renamed" {
		t.Fatalf("a refused update elevated or renamed the account: %v", err)
	}
	if res := request(t, handler, "/api/account", token); res.Code != http.StatusOK || res.Body.String() != `{"isAdmin":false}` {
		t.Fatalf("account: %d %s", res.Code, res.Body)
	}
}
