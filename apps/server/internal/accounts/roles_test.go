package accounts

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/forms"
)

const usersRecords = "/api/collections/users/records"

func TestPublicUsersCannotGrantRoles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	handler := e.handler(t)
	injections := []map[string]any{{"role": "admin"}, {"role+": "admin"}, {"+role": "admin"}, {"role": []string{"admin"}}}
	for i, injection := range injections {
		input := map[string]any{"email": "injected" + string(rune('a'+i)) + "@example.test", "password": "public-signup-password", "passwordConfirm": "public-signup-password"}
		for key, value := range injection {
			input[key] = value
		}
		if res := send(t, handler, http.MethodPost, usersRecords, "", input); res.Code == http.StatusOK {
			t.Fatalf("signup accepted role injection %v: %s", injection, res.Body)
		}
		if _, err := e.app.FindAuthRecordByEmail("users", input["email"].(string)); err == nil {
			t.Fatal("rejected signup persisted a user")
		}
	}
	res := send(t, handler, http.MethodPost, usersRecords, "", map[string]string{
		"email": "ordinary@example.test", "name": "Ordinary", "password": "public-signup-password", "passwordConfirm": "public-signup-password",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("ordinary signup: %d %s", res.Code, res.Body)
	}
	user, err := e.app.FindAuthRecordByEmail("users", "ordinary@example.test")
	if err != nil || user.GetString("role") != "user" {
		t.Fatalf("first signup did not remain ordinary: %v", err)
	}
	if count, err := e.app.CountRecords("profiles"); err != nil || count != 1 {
		t.Fatalf("the signup did not get its default profile: %d %v", count, err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	path := usersRecords + "/" + user.Id
	for _, injection := range injections {
		if res := send(t, handler, http.MethodPatch, path, token, injection); res.Code == http.StatusOK {
			t.Fatalf("self-service accepted role injection %v: %s", injection, res.Body)
		}
		fresh, err := e.app.FindRecordById("users", user.Id)
		if err != nil || fresh.GetString("role") != "user" {
			t.Fatalf("self-service elevated user: %v", err)
		}
	}
	if res := send(t, handler, http.MethodPatch, path, token, map[string]string{"name": "Renamed"}); res.Code != http.StatusOK {
		t.Fatalf("original self-update rule broken: %d %s", res.Code, res.Body)
	}
	if res := send(t, handler, http.MethodDelete, path, token, nil); res.Code == http.StatusNoContent {
		t.Fatal("self-service deleted an ordinary account")
	}
	if _, err := e.app.FindRecordById("users", user.Id); err != nil {
		t.Fatalf("ordinary account disappeared after blocked deletion: %v", err)
	}
	user, err = e.app.FindRecordById("users", user.Id)
	if err != nil {
		t.Fatal(err)
	}
	user.Set("role", "admin")
	if err := e.app.Save(user); err != nil {
		t.Fatal(err)
	}
	if res := send(t, handler, http.MethodDelete, path, token, nil); res.Code == http.StatusNoContent {
		t.Fatal("self-service deleted the last administrator")
	}
	if _, err := e.app.FindRecordById("users", user.Id); err != nil {
		t.Fatalf("last administrator disappeared after blocked deletion: %v", err)
	}
}

func TestTrustedRoleWritesAndFreshAccountRole(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	handler := e.handler(t)
	user, token := e.user(t, "trusted@example.test", "")
	assertAdmin := func(want bool) {
		t.Helper()
		got, err := e.svc.IsAdmin(user.Id)
		if err != nil || got != want {
			t.Fatalf("IsAdmin=%v err=%v, want %v", got, err, want)
		}
	}
	if user.GetString("role") != "user" {
		t.Fatalf("trusted ordinary creation: role=%q", user.GetString("role"))
	}
	assertAdmin(false)
	form := forms.NewRecordUpsert(e.app, user)
	form.GrantManagerAccess()
	form.Load(map[string]any{"role": "admin"})
	if err := form.Submit(); err != nil {
		t.Fatalf("trusted native admin form rejected: %v", err)
	}
	assertAdmin(true)
	if res := send(t, handler, http.MethodPatch, usersRecords+"/"+user.Id, token, map[string]string{"role": "user"}); res.Code == http.StatusOK {
		t.Fatal("public self-service changed admin role")
	}
	assertAdmin(true)
	user.Set("role", "user")
	if err := e.app.Save(user); err != nil {
		t.Fatal(err)
	}
	assertAdmin(false)
}

func TestPocketBaseSuperuserCanPromoteExistingViyuUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	handler := e.handler(t)
	user, userToken := e.user(t, "promote@example.test", "user")
	collection, err := e.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	superuser := core.NewRecord(collection)
	superuser.SetEmail("superuser@example.test")
	superuser.SetPassword("superuser-test-password")
	if err := e.app.Save(superuser); err != nil {
		t.Fatal(err)
	}
	token, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	path := usersRecords + "/" + user.Id
	promote := map[string]string{"role": "admin"}
	if res := send(t, handler, http.MethodPatch, path, "", promote); res.Code == http.StatusOK {
		t.Fatal("anonymous promotion accepted")
	}
	if res := send(t, handler, http.MethodPatch, path, userToken, promote); res.Code == http.StatusOK {
		t.Fatal("self-promotion accepted")
	}
	if res := send(t, handler, http.MethodPatch, path, token, promote); res.Code != http.StatusOK {
		t.Fatalf("superuser promotion: %d %s", res.Code, res.Body)
	}
	if admin, err := e.svc.IsAdmin(user.Id); err != nil || !admin {
		t.Fatalf("promoted user is not an administrator: %v %v", admin, err)
	}
	// A superuser promotes; demotion belongs to the administration, which keeps one administrator.
	if res := send(t, handler, http.MethodPatch, path, token, map[string]string{"role": "user"}); res.Code != http.StatusBadRequest {
		t.Fatalf("superuser demotion: %d %s", res.Code, res.Body)
	}
}

// The command is driven through the root command of a real PocketBase application, which parses --dir
// from os.Args when it is built; it cannot run in parallel.
func TestAdminPromotionCommandHonorsDir(t *testing.T) {
	dir := t.TempDir()
	legacy := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	registerRoleGuards(legacy)
	if err := legacy.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := legacy.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	collection, err := legacy.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	user := core.NewRecord(collection)
	user.SetEmail("existing@example.test")
	user.SetPassword("existing-promotion-password")
	if err := legacy.Save(user); err != nil {
		t.Fatal(err)
	}
	other := core.NewRecord(collection)
	other.SetEmail("other@example.test")
	other.SetPassword("other-promotion-password")
	if err := legacy.Save(other); err != nil {
		t.Fatal(err)
	}
	if err := legacy.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}

	previousArgs := os.Args
	os.Args = []string{"viyu", "admin", "promote", user.Email(), "--dir", dir}
	t.Cleanup(func() { os.Args = previousArgs })
	decoy := filepath.Join(t.TempDir(), "unused")
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: decoy})
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	RegisterAdminRoles(app)
	app.RootCmd.SetArgs(os.Args[1:])
	var output bytes.Buffer
	app.RootCmd.SetOut(&output)
	app.RootCmd.SetErr(&output)
	if err := app.RootCmd.Execute(); err != nil {
		t.Fatalf("promote command: %v", err)
	}
	promoted, err := app.FindRecordById("users", user.Id)
	if err != nil || promoted.GetString("role") != "admin" || !promoted.ValidatePassword("existing-promotion-password") {
		t.Fatalf("existing user was not promoted intact: %v", err)
	}
	ordinary, err := app.FindRecordById("users", other.Id)
	if err != nil || ordinary.GetString("role") != "user" {
		t.Fatalf("command elevated an unrelated existing user: %v", err)
	}
	if _, err := app.FindCollectionByNameOrId("profiles"); err != nil {
		t.Fatalf("command did not apply app migrations: %v", err)
	}
	if _, err := os.Stat(decoy); !os.IsNotExist(err) {
		t.Fatalf("command ignored --dir: %v", err)
	}
	if strings.Contains(output.String(), "password") || strings.Contains(output.String(), promoted.TokenKey()) {
		t.Fatal("command printed credentials")
	}
	app.RootCmd.SetArgs([]string{"admin", "promote", "missing@example.test"})
	if err := app.RootCmd.Execute(); err == nil {
		t.Fatal("promotion created a missing account")
	}
	if _, err := app.FindAuthRecordByEmail("users", "missing@example.test"); err == nil {
		t.Fatal("missing promotion target was persisted")
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	preserved, err := app.FindRecordById("users", promoted.Id)
	if err != nil || preserved.GetString("role") != "admin" {
		t.Fatalf("re-running migrations reset explicit promotion: %v", err)
	}
}

func TestPromoteAdminTouchesOnlyTheNamedUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	app := e.app
	named, _ := e.user(t, "named@example.test", "")
	other, _ := e.user(t, "unrelated@example.test", "")
	if err := PromoteAdmin(app, "named@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := PromoteAdmin(app, "missing@example.test"); err == nil {
		t.Fatal("promotion created or promoted a missing account")
	}
	for id, want := range map[string]string{named.Id: "admin", other.Id: "user"} {
		fresh, err := app.FindRecordById("users", id)
		if err != nil || fresh.GetString("role") != want {
			t.Fatalf("role of %s = %q, want %q (%v)", id, fresh.GetString("role"), want, err)
		}
	}
}
