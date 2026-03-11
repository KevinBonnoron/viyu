package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshedCookieJarIsStoredWithoutEndingTheSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "refresh@example.test")
	f.connect(t, user.Id, alphaID, "original")
	version, err := f.svc.SessionVersion(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		t.Helper()
		record, err := f.svc.connection(user.Id, alphaID)
		if err != nil {
			t.Fatal(err)
		}
		return record.GetString("encrypted_cookies")
	}
	original := stored()

	path, cleanup, err := f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(path)
	if directory != f.cookieDir || filepath.Dir(directory) != f.app.DataDir() {
		t.Fatalf("cookie files must live in the runner's directory inside the data directory: %s", directory)
	}
	assertPrivate(t, directory, 0o700)
	// The downloader leaves the same cookies in another order: nothing to store.
	rows := strings.Split(strings.TrimSpace(readFile(t, path)), "\n")
	rows[1], rows[2] = rows[2], rows[1]
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	assertGone(t, path)
	if stored() != original {
		t.Fatal("a reordered jar was stored again")
	}

	// A jar that no longer holds a valid session (the downloader dropped the login cookie) is never stored.
	path, cleanup, err = f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n.fake.test\tTRUE\t/\tTRUE\t0\tPREF\tx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if stored() != original {
		t.Fatal("an invalid jar replaced the stored session")
	}
	// Nor an emptied one, nor a missing one.
	path, cleanup, err = f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	path, cleanup, err = f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if stored() != original {
		t.Fatal("an empty or missing jar replaced the stored session")
	}

	// The platform rotated a cookie: the new jar is stored, encrypted, and playback sessions bound to the
	// connection stay valid.
	path, cleanup, err = f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fakeCookies("rotated")), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if stored() == original || strings.Contains(stored(), "rotated") {
		t.Fatal("the rotated jar was not stored encrypted")
	}
	if cookies, err := f.svc.StoredCookies(user.Id, alphaID); err != nil || !strings.Contains(cookies, "rotated") || strings.Contains(cookies, "original") {
		t.Fatalf("the stored session is not the rotated one: %v", err)
	}
	if next, err := f.svc.SessionVersion(user.Id, alphaID); err != nil || next != version {
		t.Fatalf("a refresh ended the playback sessions of the connection: %v", err)
	}

	// A refresh never overwrites a connection that was replaced while the downloader ran...
	path, cleanup, err = f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	f.connect(t, user.Id, alphaID, "replacement")
	if err := os.WriteFile(path, []byte(fakeCookies("late")), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if cookies, err := f.svc.StoredCookies(user.Id, alphaID); err != nil || !strings.Contains(cookies, "replacement") || strings.Contains(cookies, "late") {
		t.Fatalf("a stale refresh overwrote the new connection: %v", err)
	}

	// ... or removed.
	path, cleanup, err = f.svc.CookieFile(user.Id, alphaID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Disconnect(user.Id, alphaID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fakeCookies("ghost")), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := f.svc.StoredCookies(user.Id, alphaID); err == nil {
		t.Fatal("a refresh brought a removed connection back")
	}
	assertGone(t, path)
}

func TestCookieDirectoryIsPurgedAtStartup(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	leftover := filepath.Join(f.cookieDir, "viyu-source-leftover.txt")
	if err := os.WriteFile(leftover, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(f.app, f.registry, f.runner, Options{}); err != nil {
		t.Fatal(err)
	}
	assertGone(t, leftover)
	assertPrivate(t, f.cookieDir, 0o700)
}
