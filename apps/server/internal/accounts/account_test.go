package accounts

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

func TestUpdateRequiresTheCurrentPasswordForANewOne(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "update@example.test", "")
	ctx := context.Background()

	if _, err := e.svc.Update(ctx, user.Id, AccountUpdate{Name: new("Ada")}); err != nil {
		t.Fatalf("profile update: %v", err)
	}
	wrong := AccountUpdate{Password: new("new-safe-password-123"), PasswordConfirm: new("new-safe-password-123"), OldPassword: "wrong"}
	_, err := e.svc.Update(ctx, user.Id, wrong)
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Status != http.StatusBadRequest || safe.Data == nil {
		t.Fatalf("password update accepted a wrong current password: %v", err)
	}
	fresh, err := e.app.FindRecordById("users", user.Id)
	if err != nil || fresh.GetString("name") != "Ada" || fresh.ValidatePassword("new-safe-password-123") {
		t.Fatalf("values changed after a rejected update: %v", err)
	}
	updated, err := e.svc.Update(ctx, user.Id, AccountUpdate{OldPassword: "account-test-password-123", Password: new("new-safe-password-123"), PasswordConfirm: new("new-safe-password-123")})
	if err != nil {
		t.Fatalf("credential update: %v", err)
	}
	fresh, err = e.app.FindRecordById("users", user.Id)
	if err != nil || !fresh.ValidatePassword("new-safe-password-123") || updated.Id != user.Id {
		t.Fatalf("credential update did not persist: %v", err)
	}
	// The password change invalidates the tokens of the account: the handler issues a fresh one.
	if _, err := fresh.NewAuthToken(); err != nil || updated.TokenKey() == user.TokenKey() {
		t.Fatalf("token key survived the password update: %v", err)
	}
}

func TestUpdateRefusesAMismatchedConfirmation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "confirm@example.test", "")
	for _, in := range []AccountUpdate{
		{OldPassword: "account-test-password-123", Password: new("one-password-123")},
		{OldPassword: "account-test-password-123", PasswordConfirm: new("one-password-123")},
		{OldPassword: "account-test-password-123", Password: new("one-password-123"), PasswordConfirm: new("other-password-123")},
		{OldPassword: "account-test-password-123", Password: new(""), PasswordConfirm: new("")},
	} {
		if _, err := e.svc.Update(context.Background(), user.Id, in); err == nil {
			t.Fatalf("update %+v accepted", in)
		}
	}
	fresh, err := e.app.FindRecordById("users", user.Id)
	if err != nil || !fresh.ValidatePassword("account-test-password-123") {
		t.Fatalf("a refused update changed the password: %v", err)
	}
}

func TestUpdateCannotChangeEmailEvenWithPasswordProof(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "original@example.test", "")
	_, err := e.svc.Update(context.Background(), user.Id, AccountUpdate{Email: new("new@example.test"), OldPassword: "account-test-password-123"})
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Status != http.StatusBadRequest {
		t.Fatalf("email change accepted: %v", err)
	}
	if _, err := e.app.FindAuthRecordByEmail("users", "original@example.test"); err != nil {
		t.Fatalf("original email was changed: %v", err)
	}
	if _, err := e.app.FindAuthRecordByEmail("users", "new@example.test"); err == nil {
		t.Fatal("new email persisted after rejection")
	}
	// Sending the current email back is not a change.
	if _, err := e.svc.Update(context.Background(), user.Id, AccountUpdate{Email: new(" original@example.test ")}); err != nil {
		t.Fatalf("unchanged email refused: %v", err)
	}
}

func TestUpdateOfAMissingAccountAsksToSignInAgain(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := e.svc.Update(context.Background(), "missing", AccountUpdate{Name: new("Ghost")})
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Status != http.StatusUnauthorized {
		t.Fatalf("missing account: %v", err)
	}
}

func TestIsAdminReadsTheRoleRightNow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "role@example.test", "")
	assertAdmin := func(want bool) {
		t.Helper()
		got, err := e.svc.IsAdmin(user.Id)
		if err != nil || got != want {
			t.Fatalf("IsAdmin=%v err=%v, want %v", got, err, want)
		}
	}
	assertAdmin(false)
	user.Set("role", "admin")
	if err := e.app.Save(user); err != nil {
		t.Fatal(err)
	}
	assertAdmin(true)
	user.Set("role", "user")
	if err := e.app.Save(user); err != nil {
		t.Fatal(err)
	}
	assertAdmin(false)
	if _, err := e.svc.IsAdmin("missing"); err == nil {
		t.Fatal("a missing account is not an administrator")
	}
}
