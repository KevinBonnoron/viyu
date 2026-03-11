package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
)

// csyncCookies is a cookie export the YouTube connector accepts, valid for an hour.
func csyncCookies(secret string) string {
	expiry := time.Now().Add(time.Hour).Unix()
	return fmt.Sprintf("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t%d\tSAPISID\t%s\n#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t%d\tLOGIN_INFO\tlogin-%s\n", expiry, secret, expiry, secret)
}

// csyncConnect connects the account's YouTube session through the route.
func csyncConnect(t *testing.T, handler http.Handler, token, secret string) {
	t.Helper()
	res := send(t, handler, http.MethodPost, "/api/sources/"+testPlatforms().Default().ID()+"/connect", token, acctJSON(t, map[string]string{"cookies": csyncCookies(secret)}))
	if res.Code != http.StatusOK {
		t.Fatalf("connecting the source: %d %s", res.Code, res.Body)
	}
}

// csyncJars lists the cookie jars the script was given.
func csyncJars(t *testing.T, state string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "cookie-paths"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func csyncGone(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

// A connected catalog is what one account's session lists: it runs with a jar of its own for as long as it
// lives, belongs to that connection alone and never becomes a public catalog.
func TestConnectedChannelCatalogOwnsItsCookiesAndEndsWithItsConnection(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	app, handler := newApp(t, csyncOptions(t, state)...)
	yt := testPlatforms().Default().ID()
	owner := testUser(t, app, "catalog-owner@example.test")
	other := testUser(t, app, "catalog-other@example.test")
	token, otherToken := tokenOf(t, owner), tokenOf(t, other)
	native := serChannel("catalog")
	channelID := storedChannel(t, app, yt, native, "Catalog channel")
	path := "/api/channel/" + channelID + "?sync=1&source=" + yt

	csyncConnect(t, handler, token, "first-secret")
	if err := os.WriteFile(filepath.Join(state, "connected-prefix"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	acctExpect(t, anonymousRequest(t, handler, path), http.StatusUnauthorized, "")
	res := request(t, handler, path, otherToken)
	acctExpect(t, res, http.StatusConflict, "SOURCE_NOT_CONNECTED")
	if strings.Contains(res.Body.String(), "first-") {
		t.Fatalf("another account was answered with a connected snapshot: %s", res.Body)
	}

	csyncWait(t, handler, path, token, func(a jobs.Catalog) bool { return a.Sync.Loaded == 5 })
	jars := csyncJars(t, state)
	// The jar is made for the job: the first page and the catalog stream are listed with the same one, which
	// is private to the process owner while it lives.
	if len(jars) != 2 || jars[0] != jars[1] {
		t.Fatalf("the job did not keep its own jar across both extractions: %q", jars)
	}
	if info, err := os.Stat(jars[0]); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the cookie file of an active job is missing or not private: %v %v", info, err)
	}
	// What the connection lists is stored like any listing, so it is addressed by record id, but it never
	// becomes a public catalog.
	if count, err := app.CountRecords("channel_catalogs"); err != nil || count != 0 {
		t.Fatalf("a connected catalog persisted as public: %d %v", count, err)
	}

	// Reconnecting invalidates everything bound to the previous authorization.
	csyncConnect(t, handler, token, "second-secret")
	if err := os.WriteFile(filepath.Join(state, "connected-prefix"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := csyncGet(t, handler, path, token)
	for _, video := range fresh.Videos {
		if native := csyncNative(t, app, video.ID); !strings.HasPrefix(native, "second-") {
			t.Fatalf("the previous connection's results were served again: %s", native)
		}
	}
	second := csyncWait(t, handler, path, token, func(a jobs.Catalog) bool { return a.Sync.Loaded == 5 })
	for _, video := range second.Videos {
		if video.Source != yt || !strings.HasPrefix(csyncNative(t, app, video.ID), "second-") {
			t.Fatalf("the previous connection leaked into the reconnected results: %+v", video)
		}
	}
	waitFor(t, "the first job's cookie file to be removed", func() bool { return csyncGone(jars[0]) })

	// Disconnecting ends the job and the jar with it, even though nobody asks again.
	if res := send(t, handler, http.MethodDelete, "/api/sources/"+yt, token, ""); res.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", res.Code, res.Body)
	}
	res = request(t, handler, path, token)
	acctExpect(t, res, http.StatusConflict, "SOURCE_NOT_CONNECTED")
	if strings.Contains(res.Body.String(), "second-") {
		t.Fatalf("a disconnected source still exposed its job: %s", res.Body)
	}
	waitFor(t, "every cookie file to be removed", func() bool {
		for _, jar := range csyncJars(t, state) {
			if !csyncGone(jar) {
				return false
			}
		}
		return true
	})
	if count, err := app.CountRecords("channel_catalogs"); err != nil || count != 0 {
		t.Fatalf("a connected catalog persisted as public: %d %v", count, err)
	}

	// The public catalog is a synchronization of its own: it inherits nothing of the connected ones.
	csyncMarker(t, state, "release")
	public := csyncWait(t, handler, "/api/channel/"+channelID+"?sync=1", "", func(a jobs.Catalog) bool { return a.Sync.Status == jobs.Complete })
	for _, video := range public.Videos {
		if video.Source != "" || !strings.HasPrefix(csyncNative(t, app, video.ID), "public-") {
			t.Fatalf("the public job inherited connected results: %+v", video)
		}
	}
}
