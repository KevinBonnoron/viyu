// Package ytdlp is the one place that starts yt-dlp. Every caller shares a global concurrency limit, a
// process-group kill on cancellation, bounded stderr, strict JSON/NDJSON decoding, typed errors and a
// shared backoff once the upstream answered 429, so no part of the server can exhaust the machine or keep
// hitting a service that asked it to slow down.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// DefaultConcurrency is the number of yt-dlp processes that may run at once.
	DefaultConcurrency = 6
	// ConcurrencyEnv overrides DefaultConcurrency.
	ConcurrencyEnv = "VIYU_YTDLP_CONCURRENCY"

	defaultBackoffBase = 10 * time.Second
	defaultBackoffMax  = 5 * time.Minute
	stderrLimit        = 64 << 10
	lineLimit          = 16 << 20
	// JSONLimit is the output cap of a strictly decoded document.
	JSONLimit = 16 << 20
)

// Kind classifies a failed run.
type Kind int

const (
	// KindFailed is any other failure of the process or of its output.
	KindFailed Kind = iota
	// KindRateLimited means the upstream answered HTTP 429 (or the shared backoff is active).
	KindRateLimited
	// KindAuthRequired means the upstream asked for a login: the session is missing or expired.
	KindAuthRequired
)

// Sentinels matched with errors.Is.
var (
	ErrRateLimited  = errors.New("yt-dlp: rate limited")
	ErrAuthRequired = errors.New("yt-dlp: authentication required")
	// ErrUnavailable matches a run that failed because what it asked for is gone or closed to it for good:
	// a video or channel that was removed, made private or never existed, or content reserved to members.
	// Asking again cannot succeed, unlike a rate limit or a network failure.
	ErrUnavailable = errors.New("yt-dlp: unavailable")
)

// Error is a failed yt-dlp run. Its message carries yt-dlp's stderr for logs: it is never meant for clients.
type Error struct {
	Kind   Kind
	Stderr string
	Err    error
	// RetryAfter is how long the shared backoff still lasts for a rate-limited error.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	switch {
	case e.Kind == KindRateLimited && e.Stderr == "":
		return fmt.Sprintf("yt-dlp: rate limited, retry in %s", e.RetryAfter.Round(time.Second))
	case e.Err != nil:
		return "yt-dlp: " + e.Err.Error() + ": " + e.Stderr
	}
	return "yt-dlp: " + e.Stderr
}

func (e *Error) Unwrap() error { return e.Err }

// Is makes errors.Is(err, ErrRateLimited), errors.Is(err, ErrAuthRequired) and errors.Is(err, ErrUnavailable)
// work on any wrapped *Error.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrRateLimited:
		return e.Kind == KindRateLimited
	case ErrAuthRequired:
		return e.Kind == KindAuthRequired
	case ErrUnavailable:
		// A private video asks to sign in, so this is judged from the text whatever the kind says; a run
		// refused by the backoff or by a 429 says nothing about the video.
		return e.Kind != KindRateLimited && Unavailable(e.Stderr)
	}
	return false
}

var (
	rateLimitPattern = regexp.MustCompile(`(?i)\bHTTP(?:/\d(?:\.\d)?)?(?:\s+error)?[: ]+\s*429\b`)
	authMarkers      = []string{
		"sign in", "log in", "not logged in", "login required", "login_required",
		"authentication required", "requires authentication", "cookies are no longer valid",
		"cookies have expired", "cookies expired", "session has expired", "session expired",
		"cookie is no longer valid", "account cookies are no longer valid",
	}
)

// unavailableMarkers are what yt-dlp prints (lower-cased) when the video or channel itself cannot be had.
var unavailableMarkers = []string{
	"video unavailable", "this video is unavailable", "this video is not available", "no longer available",
	"private video", "this video is private", "has been removed", "removed by the uploader", "has been deleted",
	"account associated with this video has been terminated", "channel does not exist", "does not exist",
	"members-only", "join this channel", "http error 404", "http error 410", "unsupported url",
}

// Unavailable tells whether a stderr text says that what was asked for is gone or closed for good (see
// ErrUnavailable).
func Unavailable(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, marker := range unavailableMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// Classify names what a stderr text says about the run.
func Classify(stderr string) Kind {
	if rateLimitPattern.MatchString(stderr) {
		return KindRateLimited
	}
	lower := strings.ToLower(stderr)
	for _, marker := range authMarkers {
		if strings.Contains(lower, marker) {
			return KindAuthRequired
		}
	}
	return KindFailed
}

// Options configures a Runner. The zero value is production behaviour.
type Options struct {
	// Concurrency bounds the processes running at once; zero reads VIYU_YTDLP_CONCURRENCY, then DefaultConcurrency.
	Concurrency int
	// Binary is the executable, "yt-dlp" (resolved through PATH when a run starts) when empty.
	Binary string
	// CookieDir is where CookieFile keeps its private jars; empty uses the system temporary directory.
	CookieDir string
	// BackoffBase and BackoffMax bound the pause that follows a 429; the pause doubles on each strike.
	BackoffBase, BackoffMax time.Duration
	// Now replaces the clock (tests).
	Now func() time.Time
}

// Runner starts yt-dlp processes under one shared concurrency limit and one shared backoff.
type Runner struct {
	binary    string
	cookieDir string
	slots     chan struct{}
	base, max time.Duration
	now       func() time.Time

	mu      sync.Mutex
	until   time.Time
	strikes int
}

// New builds a Runner.
func New(opts Options) *Runner {
	limit := opts.Concurrency
	if limit <= 0 {
		limit = DefaultConcurrency
		if configured, err := strconv.Atoi(os.Getenv(ConcurrencyEnv)); err == nil && configured > 0 {
			limit = configured
		}
	}
	r := &Runner{binary: opts.Binary, cookieDir: opts.CookieDir, slots: make(chan struct{}, limit), base: opts.BackoffBase, max: opts.BackoffMax, now: opts.Now}
	if r.binary == "" {
		r.binary = "yt-dlp"
	}
	if r.base <= 0 {
		r.base = defaultBackoffBase
	}
	if r.max <= 0 {
		r.max = defaultBackoffMax
	}
	if r.now == nil {
		r.now = time.Now
	}
	return r
}

// Slots is the number of processes that may run at once.
func (r *Runner) Slots() int { return cap(r.slots) }

// RetryAfter is how long the shared backoff lasts still: zero when yt-dlp may be started.
func (r *Runner) RetryAfter() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return max(0, r.until.Sub(r.now()))
}

func (r *Runner) strike() {
	r.mu.Lock()
	defer r.mu.Unlock()
	pause := r.base
	for range r.strikes {
		if pause *= 2; pause >= r.max {
			pause = r.max
			break
		}
	}
	r.strikes++
	if until := r.now().Add(pause); until.After(r.until) {
		r.until = until
	}
}

func (r *Runner) recover() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.now().Before(r.until) {
		r.strikes = 0
	}
}

// Spec describes one run.
type Spec struct {
	Args []string
	// MaxOutput refuses an output longer than this many bytes; zero is unlimited (streamed NDJSON).
	MaxOutput int64
	// Stderr inspects what the process wrote to stderr once it exited, whether it failed or not. A non-nil
	// result becomes the outcome of the run, so a caller can classify a failure its own way.
	Stderr func(stderr string) error
}

// Slot is a process slot taken for a command that Run does not manage (tests).
type Slot struct{ release func() }

// Release frees the slot; it is safe to call more than once.
func (s *Slot) Release() { s.release() }

// Acquire waits for a free process slot, or for ctx.
func (r *Runner) Acquire(ctx context.Context) (*Slot, error) {
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	return &Slot{release: func() { once.Do(func() { <-r.slots }) }}, nil
}

// Guard runs fn, a request to an upstream that is not a yt-dlp process (Twitch's GraphQL API), under the
// limits every yt-dlp run shares: it takes a process slot for the duration of fn, refuses to start while the
// shared backoff lasts (a rate-limited *Error that says how long) and, when fn fails with ErrRateLimited,
// starts the backoff for every other caller. fn's error is returned as it is.
func (r *Runner) Guard(ctx context.Context, fn func() error) error {
	if wait := r.RetryAfter(); wait > 0 {
		return &Error{Kind: KindRateLimited, RetryAfter: wait}
	}
	slot, err := r.Acquire(ctx)
	if err != nil {
		return err
	}
	defer slot.Release()
	err = fn()
	switch {
	case err == nil:
		r.recover()
	case errors.Is(err, ErrRateLimited):
		r.strike()
	}
	return err
}

// Run starts yt-dlp once a slot is free and hands its stdout to consume. A failing consume (malformed
// output) kills the process group; the output is then abandoned. The process group is killed, including
// downloader/ffmpeg children, both on cancellation of ctx and when the caller's application shuts down.
func (r *Runner) Run(ctx context.Context, spec Spec, consume func(stdout io.Reader) error) error {
	if wait := r.RetryAfter(); wait > 0 {
		return &Error{Kind: KindRateLimited, RetryAfter: wait}
	}
	slot, err := r.Acquire(ctx)
	if err != nil {
		return err
	}
	defer slot.Release()
	cmd := exec.CommandContext(ctx, r.binary, append([]string{"--ignore-config", "--no-mark-watched"}, spec.Args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	var stderr boundedLog
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	var reader io.Reader = stdout
	var limited *io.LimitedReader
	if spec.MaxOutput > 0 {
		limited = &io.LimitedReader{R: stdout, N: spec.MaxOutput + 1}
		reader = limited
	}
	readErr := consume(reader)
	if readErr == nil && limited != nil && limited.N <= 0 {
		readErr = errors.New("output limit exceeded")
	}
	if readErr != nil {
		_ = cmd.Cancel()
	}
	waitErr := cmd.Wait()
	text := stderr.String()
	if spec.Stderr != nil {
		if err := spec.Stderr(text); err != nil {
			return err
		}
	}
	if readErr == nil && waitErr == nil {
		r.recover()
		return ctx.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	kind := Classify(text)
	if kind == KindRateLimited {
		r.strike()
	}
	if kind != KindFailed {
		return &Error{Kind: kind, Stderr: text, Err: firstError(waitErr, readErr), RetryAfter: r.RetryAfter()}
	}
	// A document cut short by the process dying is the process' failure: its stderr says why.
	if readErr != nil && (waitErr == nil || !(errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF))) {
		return readErr
	}
	return &Error{Stderr: text, Err: waitErr}
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// boundedLog drains stderr without allowing an external process to exhaust memory.
type boundedLog struct{ bytes.Buffer }

func (b *boundedLog) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < stderrLimit {
		_, _ = b.Buffer.Write(p[:min(n, stderrLimit-b.Len())])
	}
	return n, nil
}

// Each decodes the NDJSON lines of one run, handing each to visit as it arrives. Lazy extractions make
// every entry available before the upstream finishes paginating.
func Each[T any](ctx context.Context, r *Runner, spec Spec, visit func(T) error) error {
	return r.Run(ctx, spec, func(stdout io.Reader) error {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64<<10), lineLimit)
		for scanner.Scan() {
			var entry T
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				return err
			}
			if err := visit(entry); err != nil {
				return err
			}
		}
		return scanner.Err()
	})
}

// All collects every NDJSON entry (or the single document of --dump-single-json) of one run.
func All[T any](ctx context.Context, r *Runner, spec Spec) ([]T, error) {
	results := []T{}
	err := Each(ctx, r, spec, func(entry T) error {
		results = append(results, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// Decode strictly decodes one JSON document: consume reads it from the decoder, and anything after it,
// truncated or oversized output, and an empty output are errors. MaxOutput defaults to JSONLimit.
func Decode(ctx context.Context, r *Runner, spec Spec, consume func(*json.Decoder) error) error {
	if spec.MaxOutput <= 0 {
		spec.MaxOutput = JSONLimit
	}
	return r.Run(ctx, spec, func(stdout io.Reader) error {
		decoder := json.NewDecoder(stdout)
		if err := consume(decoder); err != nil {
			return err
		}
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			return errors.New("unexpected downloader output")
		}
		return nil
	})
}

// WithCookies appends the cookie jar argument when there is one.
func WithCookies(args []string, cookieFile string) []string {
	if cookieFile != "" {
		return append(args, "--cookies", cookieFile)
	}
	return args
}

// CookieFile writes cookies to a new private temporary file for one process. The cleanup removes it.
func (r *Runner) CookieFile(cookies string) (string, func(), error) {
	if r.cookieDir != "" {
		if err := os.MkdirAll(r.cookieDir, 0o700); err != nil {
			return "", nil, err
		}
	}
	f, err := os.CreateTemp(r.cookieDir, "viyu-source-*.txt")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err = f.WriteString(cookies); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, err
	}
	if err = f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}

// ResetCookieDir empties the private cookie directory, so jars left by a crashed process never linger.
func (r *Runner) ResetCookieDir() error {
	if r.cookieDir == "" {
		return nil
	}
	if err := os.RemoveAll(r.cookieDir); err != nil {
		return err
	}
	return os.MkdirAll(r.cookieDir, 0o700)
}
