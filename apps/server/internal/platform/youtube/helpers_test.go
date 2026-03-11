package youtube

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// fakeYTDLP is a platform whose yt-dlp is a shell script: body runs after the arguments of the call were
// appended to the returned log (one line per run).
func fakeYTDLP(t *testing.T, body string, mutate ...func(*Options)) (*Platform, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls")
	script := testutil.Script(t, "yt-dlp", "[ \"$1\" = --warm-up ] && exit 0\nprintf '%s\\n' \"$*\" >> '"+log+"'\n"+body)
	ready(t, script)
	opts := Options{Runner: ytdlp.New(ytdlp.Options{Binary: script, Concurrency: 4, CookieDir: t.TempDir()})}
	for _, change := range mutate {
		change(&opts)
	}
	return New(opts), log
}

// ready runs a freshly written script once. Executing a file while a process forked by a parallel test
// still holds it open for writing fails with ETXTBSY; once one run succeeds no such process is left, and
// no later fork can inherit a descriptor that is closed.
func ready(t *testing.T, script string) {
	t.Helper()
	for range 100000 {
		err := exec.Command(script, "--warm-up").Run()
		if !errors.Is(err, syscall.ETXTBSY) {
			if err != nil {
				t.Fatal(err)
			}
			return
		}
		runtime.Gosched()
	}
	t.Fatal("the fake yt-dlp stayed busy")
}

// calls returns the arguments of every run so far, one line each.
func calls(t *testing.T, log string) string {
	t.Helper()
	data, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// assertCode fails unless err is an *apierr.Error with the code, and leaks nothing private in its message.
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatalf("want error %s, got %v", code, err)
	}
	for _, secret := range []string{"private-cookie-value", "viyu-source", "/tmp/", "secret-cookie"} {
		if strings.Contains(safe.Message, secret) {
			t.Fatalf("the message of %s leaks %q: %s", code, secret, safe.Message)
		}
	}
}
