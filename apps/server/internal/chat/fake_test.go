package chat

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

var (
	errUnreachable = errors.New("chat unreachable")
	errDropped     = errors.New("session dropped")
)

var (
	fakeChannel = regexp.MustCompile(`(?i)^[a-z0-9_]{1,25}$`)
	fakeVideoID = regexp.MustCompile(`^[1-9][0-9]{0,21}$`)
)

// fakeChat is a scripted platform.Chat: Follow registers a session the test drives, Replay runs a function.
type fakeChat struct {
	sessions    chan *fakeSession
	attempts    atomic.Int32
	reachable   atomic.Bool
	replay      func(ctx context.Context, videoID string, offset int, cursor string) (platform.ChatPage, error)
	replayCalls atomic.Int32
}

func newFakeChat() *fakeChat {
	f := &fakeChat{sessions: make(chan *fakeSession, 64)}
	f.reachable.Store(true)
	return f
}

// fakeSession is one Follow call in progress.
type fakeSession struct {
	channel string
	message func(platform.ChatMessage)
	drop    chan struct{}
	closed  chan struct{} // closed once Follow returned
}

func (s *fakeSession) send(m platform.ChatMessage) { s.message(m) }

func (f *fakeChat) ValidChannel(channel string) bool { return fakeChannel.MatchString(channel) }

func (f *fakeChat) ValidReplayID(videoID string) bool { return fakeVideoID.MatchString(videoID) }

func (f *fakeChat) Follow(ctx context.Context, channel string, ready func(), message func(platform.ChatMessage)) (bool, error) {
	f.attempts.Add(1)
	if !f.reachable.Load() {
		return false, errUnreachable
	}
	session := &fakeSession{channel: channel, message: message, drop: make(chan struct{}), closed: make(chan struct{})}
	defer close(session.closed)
	f.sessions <- session
	ready()
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	case <-session.drop:
		return true, errDropped
	}
}

func (f *fakeChat) Replay(ctx context.Context, videoID string, offset int, cursor string) (platform.ChatPage, error) {
	f.replayCalls.Add(1)
	return f.replay(ctx, videoID, offset, cursor)
}

func (f *fakeChat) nextSession(t *testing.T) *fakeSession {
	t.Helper()
	select {
	case s := <-f.sessions:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no upstream session")
		return nil
	}
}

// group is the Spawner of the tests: it owns the goroutines and waits for them.
type group struct {
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

func newGroup(t *testing.T) *group {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	g := &group{ctx: ctx, cancel: cancel}
	t.Cleanup(g.stop)
	return g
}

func (g *group) Go(work func(ctx context.Context)) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return false
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		work(g.ctx)
	}()
	return true
}

func (g *group) stop() {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()
	g.cancel()
	g.wg.Wait()
}

// newService builds a Service with short timings over a fresh group; tweak adjusts the options.
func newService(t *testing.T, tweak func(*Options)) (*Service, *group) {
	t.Helper()
	g := newGroup(t)
	opts := Options{
		RoomIdle:      20 * time.Millisecond,
		PingInterval:  10 * time.Millisecond,
		ReconnectBase: time.Millisecond,
		ReconnectMax:  4 * time.Millisecond,
		Spawn:         g.Go,
	}
	if tweak != nil {
		tweak(&opts)
	}
	return New(opts), g
}

func waitSignal(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: %s", what)
	}
}

func waitMessage(t *testing.T, sub *subscriber) platform.ChatMessage {
	t.Helper()
	select {
	case m := <-sub.messages:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no chat message")
		return platform.ChatMessage{}
	}
}
