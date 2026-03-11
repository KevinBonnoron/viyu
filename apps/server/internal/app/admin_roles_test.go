package app

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const usersRecordsPath = "/api/collections/users/records"

func admExpectAdminFlag(t *testing.T, handler http.Handler, token string, want bool) {
	t.Helper()
	res := request(t, handler, "/api/account", token)
	var data map[string]bool
	acctDecode(t, res, &data)
	if res.Code != http.StatusOK || len(data) != 1 {
		t.Fatalf("account: %d %s", res.Code, res.Body)
	}
	if got, present := data["isAdmin"]; !present || got != want {
		t.Fatalf("account isAdmin=%v (present=%v), want %v", got, present, want)
	}
}

// The role rules are tested in accounts; here the composed application proves they are bound: a role never
// comes from a public signup or a self-service write, the account route reads it fresh, and a superuser may
// promote.
func TestComposedApplicationGuardsTheRolesOfTheUsersCollection(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	injections := []string{`"role":"admin"`, `"role+":"admin"`, `"+role":"admin"`, `"role":["admin"]`}
	for i, injection := range injections {
		email := "injected" + string(rune('a'+i)) + "@example.test"
		res := send(t, handler, http.MethodPost, usersRecordsPath, "", `{"email":"`+email+`","password":"public-signup-password","passwordConfirm":"public-signup-password",`+injection+`}`)
		if res.Code == http.StatusOK {
			t.Fatalf("signup accepted the role injection %s: %s", injection, res.Body)
		}
		if _, err := app.FindAuthRecordByEmail("users", email); err == nil {
			t.Fatal("a rejected signup persisted a user")
		}
	}
	res := send(t, handler, http.MethodPost, usersRecordsPath, "", `{"email":"ordinary@example.test","name":"Ordinary","password":"public-signup-password","passwordConfirm":"public-signup-password"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("ordinary signup: %d %s", res.Code, res.Body)
	}
	user, err := app.FindAuthRecordByEmail("users", "ordinary@example.test")
	if err != nil || user.GetString("role") != "user" {
		t.Fatalf("a signup did not stay ordinary: %v", err)
	}
	if _, err := app.FindFirstRecordByFilter("profiles", "user = {:user}", map[string]any{"user": user.Id}); err != nil {
		t.Fatalf("a signup did not get its default profile: %v", err)
	}
	token := tokenOf(t, user)
	for _, injection := range injections {
		if res := send(t, handler, http.MethodPatch, usersRecordsPath+"/"+user.Id, token, "{"+injection+"}"); res.Code == http.StatusOK {
			t.Fatalf("self-service accepted the role injection %s: %s", injection, res.Body)
		}
		if fresh, err := app.FindRecordById("users", user.Id); err != nil || fresh.GetString("role") != "user" {
			t.Fatalf("self-service elevated the user: %v", err)
		}
	}
	if res := send(t, handler, http.MethodPatch, usersRecordsPath+"/"+user.Id, token, `{"name":"Renamed"}`); res.Code != http.StatusOK {
		t.Fatalf("the original self-update rule is broken: %d %s", res.Code, res.Body)
	}
	admExpectAdminFlag(t, handler, token, false)

	superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	superuser := core.NewRecord(superusers)
	superuser.SetEmail("superuser@example.test")
	superuser.SetPassword("superuser-test-password")
	if err := app.Save(superuser); err != nil {
		t.Fatal(err)
	}
	path := usersRecordsPath + "/" + user.Id
	if res := send(t, handler, http.MethodPatch, path, "", `{"role":"admin"}`); res.Code == http.StatusOK {
		t.Fatal("an anonymous promotion was accepted")
	}
	if res := send(t, handler, http.MethodPatch, path, token, `{"role":"admin"}`); res.Code == http.StatusOK {
		t.Fatal("a self-promotion was accepted")
	}
	if res := send(t, handler, http.MethodPatch, path, tokenOf(t, superuser), `{"role":"admin"}`); res.Code != http.StatusOK {
		t.Fatalf("superuser promotion: %d %s", res.Code, res.Body)
	}
	// The same token reads the role right now, not the one it was issued with.
	admExpectAdminFlag(t, handler, token, true)
	// An administrator cannot delete their own last administrator account through the records API either.
	if res := send(t, handler, http.MethodDelete, path, token, ""); res.Code == http.StatusNoContent {
		t.Fatal("self-service deleted the last administrator")
	}
	if _, err := app.FindRecordById("users", user.Id); err != nil {
		t.Fatalf("the last administrator disappeared after a blocked deletion: %v", err)
	}
	if res := send(t, handler, http.MethodPatch, path, tokenOf(t, superuser), `{"role":"user"}`); res.Code != http.StatusBadRequest {
		t.Fatalf("superuser demotion: %d %s (administrators are demoted from the administration)", res.Code, res.Body)
	}
}

// `viyu admin promote EMAIL` is how the first administrator is appointed. The command is part of what New
// builds, drives the real root command, and PocketBase reads --dir from os.Args when the application is built,
// so the test cannot run in parallel.
func TestAdminPromotionCommandOfTheApplicationHonorsDir(t *testing.T) {
	dir := t.TempDir()
	first := New(Options{DataDir: dir})
	if err := first.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := first.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	user := testUser(t, first, "existing@example.test")
	other := testUser(t, first, "other@example.test")
	if err := first.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}

	previousArgs := os.Args
	os.Args = []string{"viyu", "admin", "promote", user.Email(), "--dir", dir}
	t.Cleanup(func() { os.Args = previousArgs })
	decoy := filepath.Join(t.TempDir(), "unused")
	app := New(Options{DataDir: decoy})
	t.Cleanup(func() { Shutdown(app) })
	app.RootCmd.SetArgs(os.Args[1:])
	var output bytes.Buffer
	app.RootCmd.SetOut(&output)
	app.RootCmd.SetErr(&output)
	if err := app.RootCmd.Execute(); err != nil {
		t.Fatalf("promote command: %v", err)
	}
	promoted, err := app.FindRecordById("users", user.Id)
	if err != nil || promoted.GetString("role") != "admin" || !promoted.ValidatePassword("test-password-0123456789") {
		t.Fatalf("the existing user was not promoted intact: %v", err)
	}
	if ordinary, err := app.FindRecordById("users", other.Id); err != nil || ordinary.GetString("role") != "user" {
		t.Fatalf("the command elevated an unrelated user: %v", err)
	}
	if _, err := os.Stat(decoy); !os.IsNotExist(err) {
		t.Fatalf("the command ignored --dir: %v", err)
	}
	if strings.Contains(output.String(), "password") || strings.Contains(output.String(), promoted.TokenKey()) {
		t.Fatal("the command printed credentials")
	}
	app.RootCmd.SetArgs([]string{"admin", "promote", "missing@example.test"})
	if err := app.RootCmd.Execute(); err == nil {
		t.Fatal("the promotion created or promoted a missing account")
	}
	if _, err := app.FindAuthRecordByEmail("users", "missing@example.test"); err == nil {
		t.Fatal("a missing promotion target was persisted")
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	if preserved, err := app.FindRecordById("users", promoted.Id); err != nil || preserved.GetString("role") != "admin" {
		t.Fatalf("re-running the migrations reset an explicit promotion: %v", err)
	}
}
