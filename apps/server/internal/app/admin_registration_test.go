package app

import (
	"net/http"
	"testing"
)

// The registration switch end to end: the public status the sign-in screen reads, the administration routes
// that change it, and the users collection rule that enforces it on a public signup.
func TestRegistrationConfigurationControlsPublicSignup(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	_, admin := admUser(t, app, "registration-admin@example.test", "admin")
	_, ordinary := admUser(t, app, "registration-user@example.test", "user")
	read := func() bool {
		t.Helper()
		res := anonymousRequest(t, handler, "/api/registration")
		var setting struct {
			Enabled bool `json:"enabled"`
		}
		if res.Code != http.StatusOK || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("public settings: %d %s", res.Code, res.Body)
		}
		acctDecode(t, res, &setting)
		return setting.Enabled
	}
	signup := func(email string) int {
		return send(t, handler, http.MethodPost, "/api/collections/users/records", "", `{"email":"`+email+`","password":"password-123","passwordConfirm":"password-123"}`).Code
	}
	if !read() {
		t.Fatal("a new installation should retain open registration")
	}

	for _, actor := range []struct {
		token string
		code  int
		kind  string
	}{{"", http.StatusUnauthorized, ""}, {ordinary, http.StatusForbidden, "ADMIN_REQUIRED"}} {
		res := send(t, handler, http.MethodPatch, "/api/admin/registration", actor.token, `{"enabled":false}`)
		acctExpect(t, res, actor.code, actor.kind)
	}
	if !read() {
		t.Fatal("a refused request changed the setting")
	}
	acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/registration", admin, `{}`), http.StatusBadRequest, "ADMIN_VALIDATION")
	acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/registration", admin, `{"enabled":"yes"}`), http.StatusBadRequest, "ADMIN_VALIDATION")

	if res := send(t, handler, http.MethodPatch, "/api/admin/registration", admin, `{"enabled":false}`); res.Code != http.StatusOK || res.Body.String() != `{"enabled":false}` {
		t.Fatalf("disable: %d %s", res.Code, res.Body)
	}
	if read() {
		t.Fatal("the public setting did not change")
	}
	if res := request(t, handler, "/api/admin/registration", admin); res.Code != http.StatusOK || res.Body.String() != `{"enabled":false}` {
		t.Fatalf("administration read: %d %s", res.Code, res.Body)
	}
	if code := signup("blocked@example.test"); code != http.StatusForbidden {
		t.Fatalf("disabled public signup: %d", code)
	}
	if _, err := app.FindAuthRecordByEmail("users", "blocked@example.test"); err == nil {
		t.Fatal("a blocked user persisted")
	}
	res := send(t, handler, http.MethodPost, "/api/admin/users", admin, `{"email":"created-by-admin@example.test","password":"password-123","passwordConfirm":"password-123","role":"user"}`)
	acctExpect(t, res, http.StatusCreated, "")

	if res := send(t, handler, http.MethodPatch, "/api/admin/registration", admin, `{"enabled":true}`); res.Code != http.StatusOK || res.Body.String() != `{"enabled":true}` {
		t.Fatalf("enable: %d %s", res.Code, res.Body)
	}
	if !read() {
		t.Fatal("the public setting did not change back")
	}
	if code := signup("allowed@example.test"); code != http.StatusOK {
		t.Fatalf("enabled public signup: %d", code)
	}
}
