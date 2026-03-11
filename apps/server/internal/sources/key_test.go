package sources

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

func TestMasterKeyIsAPrivateFileNextToTheDatabaseByDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	path := filepath.Join(f.app.DataDir(), "source_connections.key")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Fatalf("master key is not a private 32-byte file: %v %v", info, err)
	}
	entries, err := os.ReadDir(f.app.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".source-key-") {
			t.Fatalf("a key tempfile was left behind: %s", entry.Name())
		}
	}
}

func TestMasterKeyIsNeverReplacedByConcurrentInitializers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const initializers = 16
	keys := make([][]byte, initializers)
	errs := make([]error, initializers)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(initializers)
	done.Add(initializers)
	for i := range initializers {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			keys[i], errs[i] = masterKey(dir, "")
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	for i := range initializers {
		if errs[i] != nil || len(keys[i]) != 32 || !bytes.Equal(keys[i], keys[0]) {
			t.Fatalf("initializer %d got a different or partial key: %v", i, errs[i])
		}
	}
	if again, err := masterKey(dir, ""); err != nil || !bytes.Equal(again, keys[0]) {
		t.Fatalf("the key changed between starts: %v", err)
	}
}

func TestMasterKeyRefusesAKeyThatIsNotPrivateRegularAndComplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "source_connections.key")
	key := bytes.Repeat([]byte{7}, 32)

	if err := os.WriteFile(path, key, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := masterKey(dir, ""); err == nil {
		t.Fatal("a world-readable key was accepted")
	}
	if err := os.WriteFile(path+".short", key[:16], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := masterKey(dir, path+".short"); err == nil {
		t.Fatal("a truncated key was accepted")
	}
	if err := os.WriteFile(path+".long", append(key, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := masterKey(dir, path+".long"); err == nil {
		t.Fatal("an oversized key was accepted")
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, key, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := masterKey(dir, link); err == nil {
		t.Fatal("a symlinked key was accepted")
	}
	// An existing complete private key is used as it is, never overwritten.
	if err := os.WriteFile(path+".ok", key, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := masterKey(dir, path+".ok"); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("an existing key was not used: %v", err)
	}
}

func TestMasterKeyCanLiveOutsideTheDataDirectory(t *testing.T) {
	t.Parallel()
	dataDir, keyDir := t.TempDir(), t.TempDir()
	keyPath := filepath.Join(keyDir, "keys", "source.key")
	key, err := masterKey(dataDir, keyPath)
	if err != nil || len(key) != 32 {
		t.Fatalf("key: %v", err)
	}
	assertPrivate(t, keyPath, 0o600)
	assertGone(t, filepath.Join(dataDir, "source_connections.key"))
	again, err := masterKey(dataDir, keyPath)
	if err != nil || !bytes.Equal(key, again) {
		t.Fatalf("the key changed between starts: %v", err)
	}
	if _, err := masterKey(dataDir, "relative/source.key"); err == nil {
		t.Fatal("a relative key path was accepted")
	}
}

func TestServiceKeyFileOptionAndEnvironment(t *testing.T) {
	app := testutil.NewApp(t)
	registry := platform.NewRegistry(&fakePlatform{id: alphaID, enabled: true, connector: &fakeConnector{}})
	runner := ytdlp.New(ytdlp.Options{CookieDir: filepath.Join(app.DataDir(), "source-cookies")})

	optionKey := filepath.Join(t.TempDir(), "option.key")
	if _, err := New(app, registry, runner, Options{KeyFile: optionKey}); err != nil {
		t.Fatal(err)
	}
	assertPrivate(t, optionKey, 0o600)

	// Without the option the environment names the key; the option wins over it.
	envKey := filepath.Join(t.TempDir(), "env.key")
	t.Setenv("VIYU_SOURCE_KEY_FILE", envKey)
	if _, err := New(app, registry, runner, Options{}); err != nil {
		t.Fatal(err)
	}
	assertPrivate(t, envKey, 0o600)
	assertGone(t, filepath.Join(app.DataDir(), "source_connections.key"))
	otherKey := filepath.Join(t.TempDir(), "other.key")
	if _, err := New(app, registry, runner, Options{KeyFile: otherKey}); err != nil {
		t.Fatal(err)
	}
	assertPrivate(t, otherKey, 0o600)

	t.Setenv("VIYU_SOURCE_KEY_FILE", "relative/source.key")
	if _, err := New(app, registry, runner, Options{}); err == nil {
		t.Fatal("a relative key path was accepted")
	}
}

// A service started with another key cannot read what the first one sealed: the key is what protects the
// stored sessions from a copy of the data directory.
func TestSessionsDoNotDecryptWithAnotherKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	user := newUser(t, f.app, "key@example.test")
	f.connect(t, user.Id, alphaID, "private-cookie-value")
	elsewhere, err := New(f.app, f.registry, f.runner, Options{KeyFile: filepath.Join(t.TempDir(), "other.key")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = elsewhere.StoredCookies(user.Id, alphaID)
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
	if _, err := elsewhere.Connect(context.Background(), user.Id, alphaID, fakeCookies("new"), ""); err != nil {
		t.Fatal(err)
	}
}
