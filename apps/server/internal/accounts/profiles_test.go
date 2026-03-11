package accounts

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewAccountGetsADefaultProfileNamedAfterIt(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user(t, "profile-owner@example.test", "")
	profile := e.firstProfile(t, owner.Id)
	if profile.GetString("user") != owner.Id || profile.GetString("name") != "profile-owner@example.test" {
		t.Fatalf("new account has no default profile named after it: %+v", profile)
	}
	again, err := ensureDefaultProfile(e.app, owner)
	if err != nil || again.Id != profile.Id {
		t.Fatalf("ensureDefaultProfile created a second profile: %v", err)
	}
	if count, err := e.app.CountRecords("profiles"); err != nil || count != 1 {
		t.Fatalf("profiles: %d %v", count, err)
	}
}

func TestDefaultProfileNameIsCutToTheProfileLimitWithoutSplittingACharacter(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "long-name@example.test", "")
	user.Set("name", strings.Repeat("é", 100))
	if err := e.app.Save(user); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Delete(e.firstProfile(t, user.Id)); err != nil {
		t.Fatal(err)
	}
	profile, err := ensureDefaultProfile(e.app, user)
	if err != nil {
		t.Fatalf("a long account name must not prevent the default profile: %v", err)
	}
	if got := profile.GetString("name"); got != strings.Repeat("é", maxProfileNameLength) {
		t.Fatalf("default profile name = %q", got)
	}
}

func TestADefaultProfileHasANameWhenTheAccountHasNone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	user, _ := e.user(t, "nameless@example.test", "")
	user.Set("name", "  ")
	if err := e.app.Save(user); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Delete(e.firstProfile(t, user.Id)); err != nil {
		t.Fatal(err)
	}
	profile, err := ensureDefaultProfile(e.app, user)
	if err != nil || profile.GetString("name") != "Profile 1" {
		t.Fatalf("default profile: %v %v", profile, err)
	}
}

func TestTheLastProfileOfAnAccountCannotBeDeleted(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	handler := e.handler(t)
	owner, token := e.user(t, "last-profile@example.test", "")
	only := e.firstProfile(t, owner.Id)
	path := "/api/collections/profiles/records/"
	response := send(t, handler, http.MethodDelete, path+only.Id, token, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "last profile") {
		t.Fatalf("deleting the only profile: %d %s", response.Code, response.Body)
	}
	second := e.profile(t, owner, "Second")
	if response := send(t, handler, http.MethodDelete, path+second.Id, token, nil); response.Code != http.StatusNoContent {
		t.Fatalf("deleting one of two profiles: %d %s", response.Code, response.Body)
	}
	if _, err := e.app.FindRecordById("profiles", only.Id); err != nil {
		t.Fatalf("the remaining profile vanished: %v", err)
	}
}
