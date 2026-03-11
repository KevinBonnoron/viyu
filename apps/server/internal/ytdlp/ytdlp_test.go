package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake writes an executable shell script standing for yt-dlp and returns its path.
func fake(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSlotsBoundConcurrentProcessesAndWaitWithTheContext(t *testing.T) {
	r := New(Options{Concurrency: 2})
	if r.Slots() != 2 {
		t.Fatalf("slots: %d", r.Slots())
	}
	acquire, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	var held []*Slot
	for range r.Slots() {
		slot, err := r.Acquire(acquire)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, slot)
	}
	waiting, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := r.Acquire(waiting); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a process started beyond the cap: %v", err)
	}
	var started sync.WaitGroup
	started.Add(1)
	go func() {
		defer started.Done()
		slot, err := r.Acquire(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		slot.Release()
	}()
	held[0].Release()
	held[0].Release() // a second release must not free a second slot
	started.Wait()
	for _, slot := range held[1:] {
		slot.Release()
	}
}

func TestConcurrencyComesFromTheEnvironment(t *testing.T) {
	t.Setenv(ConcurrencyEnv, "3")
	if got := New(Options{}).Slots(); got != 3 {
		t.Fatalf("slots: %d", got)
	}
	t.Setenv(ConcurrencyEnv, "bogus")
	if got := New(Options{}).Slots(); got != DefaultConcurrency {
		t.Fatalf("slots: %d", got)
	}
}

func TestEachStreamsEntriesAndAllCollectsThem(t *testing.T) {
	r := New(Options{Binary: fake(t, `printf '{"id":"a"}\n{"id":"b"}\n'`)})
	type entry struct {
		ID string `json:"id"`
	}
	entries, err := All[entry](context.Background(), r, Spec{})
	if err != nil || len(entries) != 2 || entries[1].ID != "b" {
		t.Fatalf("entries %v: %v", entries, err)
	}
}

func TestMalformedOutputKillsTheProcessAndFails(t *testing.T) {
	r := New(Options{Binary: fake(t, `echo 'not json'; sleep 30`)})
	done := make(chan error, 1)
	go func() {
		_, err := All[map[string]any](context.Background(), r, Spec{})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("malformed output was accepted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the process was not killed")
	}
}

func TestDecodeIsStrict(t *testing.T) {
	for name, script := range map[string]string{
		"trailing data": `echo '{"a":1}'; echo '{"a":2}'`,
		"empty":         `true`,
		"truncated":     `printf '{"a":'`,
	} {
		r := New(Options{Binary: fake(t, script)})
		err := Decode(context.Background(), r, Spec{}, func(d *json.Decoder) error {
			var v map[string]any
			return d.Decode(&v)
		})
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	r := New(Options{Binary: fake(t, `echo '{"a":1}'`)})
	if err := Decode(context.Background(), r, Spec{}, func(d *json.Decoder) error {
		var v map[string]any
		return d.Decode(&v)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRefusesOversizedOutput(t *testing.T) {
	r := New(Options{Binary: fake(t, `printf '{"a":"%s"}' "$(head -c 200 /dev/zero | tr '\0' x)"`)})
	err := Decode(context.Background(), r, Spec{MaxOutput: 50}, func(d *json.Decoder) error {
		var v map[string]any
		return d.Decode(&v)
	})
	if err == nil {
		t.Fatal("oversized output accepted")
	}
}

func TestStderrHookClassifiesEvenASuccessfulRun(t *testing.T) {
	r := New(Options{Binary: fake(t, `echo 'WARNING: unable to mark watched' >&2; echo '{}'`)})
	sentinel := errors.New("classified")
	err := Decode(context.Background(), r, Spec{Stderr: func(text string) error {
		if strings.Contains(text, "mark watched") {
			return sentinel
		}
		return nil
	}}, func(d *json.Decoder) error {
		var v map[string]any
		return d.Decode(&v)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
}

func TestFailuresAreClassified(t *testing.T) {
	for script, want := range map[string]Kind{
		`echo 'ERROR: HTTP Error 429: Too Many Requests' >&2; exit 1`:    KindRateLimited,
		`echo 'ERROR: Sign in to confirm you are not a bot' >&2; exit 1`: KindAuthRequired,
		`echo 'ERROR: boom' >&2; exit 1`:                                 KindFailed,
	} {
		r := New(Options{Binary: fake(t, script)})
		err := Decode(context.Background(), r, Spec{}, func(d *json.Decoder) error { var v any; return d.Decode(&v) })
		var failure *Error
		if !errors.As(err, &failure) || failure.Kind != want {
			t.Fatalf("%q: got %v", script, err)
		}
		if want == KindRateLimited && !errors.Is(err, ErrRateLimited) || want == KindAuthRequired && !errors.Is(err, ErrAuthRequired) {
			t.Fatalf("%q: sentinel not matched by %v", script, err)
		}
		if !strings.Contains(err.Error(), strings.SplitN(script, "'", 3)[1][7:]) {
			t.Fatalf("%q: stderr missing from %q", script, err)
		}
	}
}

func TestUnavailableIsJudgedFromTheTextWhateverTheKind(t *testing.T) {
	for stderr, want := range map[string]bool{
		"ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader":     true,
		"ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video": true,
		"ERROR: [youtube:tab] This channel does not exist.":                                        true,
		"ERROR: [youtube] abc: Join this channel to get access to members-only content":            true,
		"ERROR: Sign in to confirm you are not a bot":                                              false,
		"ERROR: Unable to download webpage: <urlopen error timed out>":                             false,
		"ERROR: boom": false,
	} {
		err := &Error{Kind: Classify(stderr), Stderr: stderr}
		if got := errors.Is(err, ErrUnavailable); got != want {
			t.Errorf("%q: unavailable=%v, want %v", stderr, got, want)
		}
	}
	if errors.Is(&Error{Kind: KindRateLimited, Stderr: "HTTP Error 429 video unavailable"}, ErrUnavailable) {
		t.Error("a rate limit says nothing of the video")
	}
}

func TestRateLimitPausesEveryCallerUntilTheBackoffEnds(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	now := time.Now()
	var clock sync.Mutex
	advance := func(d time.Duration) { clock.Lock(); now = now.Add(d); clock.Unlock() }
	r := New(Options{
		Binary:      fake(t, `echo call >> `+log+`; echo 'ERROR: HTTP Error 429: Too Many Requests' >&2; exit 1`),
		BackoffBase: time.Minute, BackoffMax: 4 * time.Minute,
		Now: func() time.Time { clock.Lock(); defer clock.Unlock(); return now },
	})
	run := func() error {
		return Decode(context.Background(), r, Spec{}, func(d *json.Decoder) error { var v any; return d.Decode(&v) })
	}
	if err := run(); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("first: %v", err)
	}
	if r.RetryAfter() <= 0 {
		t.Fatal("no backoff after a 429")
	}
	err := run()
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != KindRateLimited || failure.RetryAfter <= 0 {
		t.Fatalf("second: %v", err)
	}
	data, _ := os.ReadFile(log)
	if calls := strings.Count(string(data), "call"); calls != 1 {
		t.Fatalf("yt-dlp started %d times during the backoff", calls)
	}
	advance(90 * time.Second)
	if r.RetryAfter() != 0 {
		t.Fatalf("backoff still %s", r.RetryAfter())
	}
	if err := run(); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third: %v", err)
	}
	// The second strike doubles the pause.
	if got := r.RetryAfter(); got <= time.Minute || got > 2*time.Minute {
		t.Fatalf("second pause %s", got)
	}
}

func TestGuardSharesSlotsAndBackoffWithProcesses(t *testing.T) {
	r := New(Options{Concurrency: 1, BackoffBase: time.Minute})
	// fn runs on the slot, and a slot taken elsewhere makes the guarded call wait for its context.
	slot, err := r.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waiting, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ran := false
	if err := r.Guard(waiting, func() error { ran = true; return nil }); !errors.Is(err, context.DeadlineExceeded) || ran {
		t.Fatalf("a guarded call ran without a slot: %v (ran=%v)", err, ran)
	}
	slot.Release()

	boom := errors.New("boom")
	if err := r.Guard(context.Background(), func() error { return boom }); !errors.Is(err, boom) || r.RetryAfter() != 0 {
		t.Fatalf("an ordinary failure: %v (backoff %s)", err, r.RetryAfter())
	}
	// The slot is back, and a rate limit starts the backoff every other caller sees.
	if err := r.Guard(context.Background(), func() error { return fmt.Errorf("twitch: %w", ErrRateLimited) }); !errors.Is(err, ErrRateLimited) || r.RetryAfter() <= 0 {
		t.Fatalf("a rate limit: %v (backoff %s)", err, r.RetryAfter())
	}
	ran = false
	err = r.Guard(context.Background(), func() error { ran = true; return nil })
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != KindRateLimited || failure.RetryAfter <= 0 || ran {
		t.Fatalf("during the backoff: %v (ran=%v)", err, ran)
	}
	if err := Decode(context.Background(), r, Spec{}, func(d *json.Decoder) error { return nil }); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("a process during the guard's backoff: %v", err)
	}
}

func TestCookieFilesArePrivateAndRemovedByTheirCleanup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jars")
	r := New(Options{CookieDir: dir})
	path, cleanup, err := r.CookieFile("# Netscape HTTP Cookie File\n")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o077 != 0 || filepath.Dir(path) != dir {
		t.Fatalf("jar %v %v", info, err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the jar outlived its cleanup")
	}
	if err := os.WriteFile(filepath.Join(dir, "stale"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.ResetCookieDir(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("stale jars left: %v", entries)
	}
}
