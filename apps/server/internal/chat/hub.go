package chat

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// userLimiter caps the live chat streams one account holds open at once, next to the global room cap:
// a single account cannot occupy every connection slot of the server.
type userLimiter struct {
	mu     sync.Mutex
	active map[string]int
}

// acquire takes one slot of user, or reports false when the account is at limit. The returned function
// gives the slot back; calling it twice frees one slot only.
func (l *userLimiter) acquire(user string, limit int) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[user] >= limit {
		return nil, false
	}
	if l.active == nil {
		l.active = make(map[string]int)
	}
	l.active[user]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.active[user]--; l.active[user] <= 0 {
				delete(l.active, user)
			}
		})
	}, true
}

// roomKey identifies a room: a channel of one platform, never the same channel name of another.
type roomKey struct{ platform, channel string }

// hub is the set of rooms. One mutex guards the map and every room and subscriber it holds.
type hub struct {
	mu    sync.Mutex
	rooms map[roomKey]*room
}

// room is one upstream session shared by the viewers of a channel.
type room struct {
	svc     *Service
	key     roomKey
	source  platform.Chat
	cancel  context.CancelFunc // set by run
	stopped bool               // stop was asked before run set cancel
	subs    map[*subscriber]struct{}
	ready   bool
	idle    *time.Timer
	idleGen int
}

// subscriber is one viewer of a room. messages is bounded: a slow viewer loses messages, it never blocks
// the room.
type subscriber struct {
	messages    chan platform.ChatMessage
	ready       chan struct{}
	unavailable chan struct{} // closed when the channel's chat cannot be reached
	ended       chan struct{} // closed when the room stopped because its owner did
	readySent   bool
}

func errBusy() error {
	return apierr.Unavailable("Chat is unavailable right now.")
}

// join subscribes to a channel's room, creating it (and its upstream session) when it is the first viewer.
// The returned function leaves the room; the room closes Options.RoomIdle after the last viewer left.
func (s *Service) join(key string, source platform.Chat, channel string) (*subscriber, func(), error) {
	rk := roomKey{platform: key, channel: strings.ToLower(channel)}
	h := &s.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[rk]
	if r == nil {
		if len(h.rooms) >= s.opts.MaxRooms {
			return nil, nil, errBusy()
		}
		r = &room{svc: s, key: rk, source: source, subs: make(map[*subscriber]struct{})}
		if !s.opts.Spawn(r.run) {
			return nil, nil, errBusy()
		}
		if h.rooms == nil {
			h.rooms = make(map[roomKey]*room)
		}
		h.rooms[rk] = r
	}
	r.idleGen++
	if r.idle != nil {
		r.idle.Stop()
		r.idle = nil
	}
	sub := &subscriber{
		messages:    make(chan platform.ChatMessage, s.opts.SubscriberBuffer),
		ready:       make(chan struct{}),
		unavailable: make(chan struct{}),
		ended:       make(chan struct{}),
	}
	if r.ready {
		sub.readySent = true
		close(sub.ready)
	}
	r.subs[sub] = struct{}{}
	var once sync.Once
	return sub, func() { once.Do(func() { s.leave(r, sub) }) }, nil
}

func (s *Service) leave(r *room, sub *subscriber) {
	h := &s.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(r.subs, sub)
	if len(r.subs) != 0 || h.rooms[r.key] != r {
		return
	}
	r.idleGen++
	generation := r.idleGen
	r.idle = time.AfterFunc(s.opts.RoomIdle, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.idleGen == generation && len(r.subs) == 0 && h.rooms[r.key] == r {
			delete(h.rooms, r.key)
			r.stopLocked()
		}
	})
}

// Rooms reports how many rooms (upstream sessions) exist.
func (s *Service) Rooms() int {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	return len(s.hub.rooms)
}

// stopLocked cancels the room's session; the caller holds the hub lock.
func (r *room) stopLocked() {
	r.stopped = true
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *room) markReady() {
	h := &r.svc.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	r.ready = true
	for sub := range r.subs {
		if !sub.readySent {
			sub.readySent = true
			close(sub.ready)
		}
	}
}

func (r *room) broadcast(message platform.ChatMessage) {
	h := &r.svc.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range r.subs {
		select {
		case sub.messages <- message:
		default:
		}
	}
}

// forgetLocked removes the room from the hub and stops its idle timer.
func (r *room) forgetLocked() {
	h := &r.svc.hub
	if h.rooms[r.key] == r {
		delete(h.rooms, r.key)
	}
	if r.idle != nil {
		r.idle.Stop()
		r.idle = nil
	}
}

// fail ends the room because the channel's chat cannot be reached: every viewer is told, and the room is
// forgotten.
func (r *room) fail() {
	h := &r.svc.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	r.forgetLocked()
	for sub := range r.subs {
		close(sub.unavailable)
	}
	r.subs = make(map[*subscriber]struct{})
	r.stopLocked()
}

// end runs when the room's goroutine stops. A room that stops because its owner does must not stay in
// the map, and its viewers must not wait on it.
func (r *room) end() {
	h := &r.svc.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	r.forgetLocked()
	for sub := range r.subs {
		close(sub.ended)
	}
	r.subs = make(map[*subscriber]struct{})
}

// run keeps the room's upstream session alive, reconnecting with backoff, until the room is cancelled.
func (r *room) run(owner context.Context) {
	ctx, cancel := context.WithCancel(owner)
	defer cancel()
	h := &r.svc.hub
	h.mu.Lock()
	r.cancel = cancel
	if r.stopped {
		cancel()
	}
	h.mu.Unlock()
	defer r.end()

	opts := r.svc.opts
	failures := 0
	for {
		ready, _ := r.source.Follow(ctx, r.key.channel, r.markReady, r.broadcast)
		if ctx.Err() != nil {
			return
		}
		h.mu.Lock()
		r.ready = false
		h.mu.Unlock()
		wait := opts.ReconnectBase
		if ready {
			failures = 0
		} else {
			failures++
			if failures >= opts.MaxAttempts {
				r.fail()
				return
			}
			wait = min(opts.ReconnectBase<<failures, opts.ReconnectMax)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
