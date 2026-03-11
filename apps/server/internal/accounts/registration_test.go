package accounts

import (
	"errors"
	"net/http"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

func TestRegistrationSwitchControlsPublicSignup(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	handler := e.handler(t)
	admin, _ := e.user(t, "registration-admin@example.test", "admin")
	ordinary, _ := e.user(t, "registration-user@example.test", "user")
	read := func() bool {
		t.Helper()
		enabled, err := e.svc.RegistrationEnabled()
		if err != nil {
			t.Fatal(err)
		}
		return enabled
	}
	if !read() {
		t.Fatal("new installation should retain open registration")
	}
	for _, actor := range []struct{ id, code string }{{"", "ADMIN_UNAUTHENTICATED"}, {ordinary.Id, "ADMIN_REQUIRED"}} {
		err := e.svc.AdminSetRegistration(actor.id, new(false))
		var safe *apierr.Error
		if !errors.As(err, &safe) || safe.Code != actor.code {
			t.Fatalf("setting by %q: %v", actor.id, err)
		}
	}
	if err := e.svc.AdminSetRegistration(admin.Id, nil); err == nil {
		t.Fatal("no choice was accepted")
	}
	if err := e.svc.AdminSetRegistration(admin.Id, new(false)); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if read() {
		t.Fatal("public setting did not change")
	}
	if enabled, err := e.svc.AdminRegistration(admin.Id); err != nil || enabled {
		t.Fatalf("admin read: %v %v", enabled, err)
	}
	if _, err := e.svc.AdminRegistration(ordinary.Id); err == nil {
		t.Fatal("an ordinary account read the administration setting")
	}

	signup := func(email string) int {
		return send(t, handler, http.MethodPost, "/api/collections/users/records", "", map[string]string{"email": email, "password": "password-123", "passwordConfirm": "password-123"}).Code
	}
	if code := signup("blocked@example.test"); code != http.StatusForbidden {
		t.Fatalf("disabled public signup: %d", code)
	}
	if _, err := e.app.FindAuthRecordByEmail("users", "blocked@example.test"); err == nil {
		t.Fatal("blocked user persisted")
	}
	if _, err := e.svc.AdminCreate(t.Context(), admin.Id, UserInput{Email: new("created-by-admin@example.test"), Password: new("password-123"), PasswordConfirm: new("password-123"), Role: new("user")}); err != nil {
		t.Fatalf("admin create should remain allowed: %v", err)
	}
	if err := e.svc.AdminSetRegistration(admin.Id, new(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if code := signup("allowed@example.test"); code != http.StatusOK {
		t.Fatalf("enabled public signup: %d", code)
	}
}
