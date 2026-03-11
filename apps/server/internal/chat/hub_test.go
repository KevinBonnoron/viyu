package chat

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestHubSharesOneSessionAndCountsSubscribers(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat := newFakeChat()

	first, leaveFirst, err := svc.join("p", chat, "Zerator")
	if err != nil {
		t.Fatal(err)
	}
	session := chat.nextSession(t)
	if session.channel != "zerator" {
		t.Fatalf("the upstream channel is lowercased: %q", session.channel)
	}
	waitSignal(t, "first subscriber ready", first.ready)
	second, leaveSecond, err := svc.join("p", chat, "zerator")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, "late subscriber is ready at once", second.ready)
	if svc.Rooms() != 1 || chat.attempts.Load() != 1 {
		t.Fatalf("one room and one session expected: rooms=%d sessions=%d", svc.Rooms(), chat.attempts.Load())
	}

	session.send(platform.ChatMessage{ID: "1", User: "A"})
	for _, sub := range []*subscriber{first, second} {
		if m := waitMessage(t, sub); m.ID != "1" || m.User != "A" {
			t.Fatalf("shared message: %+v", m)
		}
	}

	// The room outlives its first subscriber, then closes RoomIdle after the last one left.
	leaveFirst()
	leaveFirst() // leaving twice must not corrupt the count
	select {
	case <-session.closed:
		t.Fatal("room closed while a subscriber was left")
	case <-time.After(5 * svc.opts.RoomIdle):
	}
	if svc.Rooms() != 1 {
		t.Fatal("room forgotten while a subscriber was left")
	}
	leaveSecond()
	waitSignal(t, "session closed with the idle room", session.closed)
	if svc.Rooms() != 0 {
		t.Fatalf("idle room must be forgotten, rooms=%d", svc.Rooms())
	}
}

func TestHubRejoinCancelsIdleClose(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.RoomIdle = 150 * time.Millisecond })
	chat := newFakeChat()
	sub, leave, err := svc.join("p", chat, "keeper")
	if err != nil {
		t.Fatal(err)
	}
	session := chat.nextSession(t)
	waitSignal(t, "ready", sub.ready)
	leave()
	time.Sleep(svc.opts.RoomIdle / 3)
	sub, leave, err = svc.join("p", chat, "keeper")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	select {
	case <-session.closed:
		t.Fatal("a rejoined room must stay open")
	case <-time.After(2 * svc.opts.RoomIdle):
	}
	if svc.Rooms() != 1 || chat.attempts.Load() != 1 {
		t.Fatalf("rooms=%d sessions=%d", svc.Rooms(), chat.attempts.Load())
	}
	waitSignal(t, "rejoined subscriber ready", sub.ready)
}

func TestHubLimitsRooms(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.MaxRooms = 2 })
	chat := newFakeChat()
	_, leaveA, err := svc.join("p", chat, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.join("p", chat, "b"); err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.join("p", chat, "c")
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable || apiErr.Code != apierr.CodeUnavailable || apiErr.Message != "Chat is unavailable right now." {
		t.Fatalf("third room: %v", err)
	}
	if _, _, err := svc.join("p", chat, "A"); err != nil {
		t.Fatalf("an existing room is always joinable: %v", err)
	}
	leaveA()
}

func TestHubRoomsAreKeyedPerPlatform(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	one, other := newFakeChat(), newFakeChat()
	subOne, _, err := svc.join("one", one, "same")
	if err != nil {
		t.Fatal(err)
	}
	subOther, _, err := svc.join("other", other, "same")
	if err != nil {
		t.Fatal(err)
	}
	sessionOne, sessionOther := one.nextSession(t), other.nextSession(t)
	waitSignal(t, "ready", subOne.ready)
	waitSignal(t, "ready", subOther.ready)
	if svc.Rooms() != 2 {
		t.Fatalf("one room per platform: %d", svc.Rooms())
	}
	sessionOne.send(platform.ChatMessage{ID: "from-one"})
	if m := waitMessage(t, subOne); m.ID != "from-one" {
		t.Fatalf("message: %+v", m)
	}
	select {
	case m := <-subOther.messages:
		t.Fatalf("a platform's chat leaked into another's room: %+v", m)
	default:
	}
	sessionOther.send(platform.ChatMessage{ID: "from-other"})
	if m := waitMessage(t, subOther); m.ID != "from-other" {
		t.Fatalf("message: %+v", m)
	}
}

func TestHubSlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.SubscriberBuffer = 3 })
	chat := newFakeChat()
	slow, _, err := svc.join("p", chat, "busy")
	if err != nil {
		t.Fatal(err)
	}
	fast, _, err := svc.join("p", chat, "busy")
	if err != nil {
		t.Fatal(err)
	}
	session := chat.nextSession(t)
	waitSignal(t, "ready", slow.ready)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := range 50 {
			session.send(platform.ChatMessage{ID: string(rune('A' + i%26)), User: "u"})
			if len(fast.messages) > 0 {
				<-fast.messages
			}
		}
	}()
	waitSignal(t, "broadcast never blocks on a slow subscriber", finished)
	if len(slow.messages) != 3 {
		t.Fatalf("slow subscriber buffer must be full and bounded: %d", len(slow.messages))
	}
}

func TestHubReconnectsWhileSubscribed(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat := newFakeChat()
	sub, leave, err := svc.join("p", chat, "flaky")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	first := chat.nextSession(t)
	waitSignal(t, "ready", sub.ready)
	close(first.drop)
	second := chat.nextSession(t)
	second.send(platform.ChatMessage{ID: "again", User: "B"})
	if m := waitMessage(t, sub); m.ID != "again" {
		t.Fatalf("message after reconnect: %+v", m)
	}
	if svc.Rooms() != 1 {
		t.Fatal("the room must survive a reconnect")
	}
}

func TestHubUnreachableChatIsUnavailableAndForgotten(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.MaxAttempts = 3 })
	chat := newFakeChat()
	chat.reachable.Store(false)
	sub, _, err := svc.join("p", chat, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, "unavailable after repeated failures", sub.unavailable)
	select {
	case <-sub.ready:
		t.Fatal("never ready")
	default:
	}
	if svc.Rooms() != 0 {
		t.Fatalf("a failed room is forgotten before its viewers are told: rooms=%d", svc.Rooms())
	}
	if got := chat.attempts.Load(); got != 3 {
		t.Fatalf("attempts: %d", got)
	}
	// A new viewer starts a fresh room.
	chat.reachable.Store(true)
	again, leave, err := svc.join("p", chat, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	waitSignal(t, "fresh room becomes ready", again.ready)
}

func TestHubKeepsRetryingAfterASessionThatWasReady(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.MaxAttempts = 2 })
	chat := newFakeChat()
	sub, leave, err := svc.join("p", chat, "steady")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	// Sessions that became ready reset the failure count: dropping many of them never gives up.
	for range 5 {
		close(chat.nextSession(t).drop)
	}
	last := chat.nextSession(t)
	waitSignal(t, "ready", sub.ready)
	select {
	case <-sub.unavailable:
		t.Fatal("a channel whose sessions became ready is never unavailable")
	default:
	}
	last.send(platform.ChatMessage{ID: "alive"})
	if m := waitMessage(t, sub); m.ID != "alive" {
		t.Fatalf("message: %+v", m)
	}
}

func TestHubStopsWhenItsOwnerStops(t *testing.T) {
	t.Parallel()
	svc, group := newService(t, nil)
	chat := newFakeChat()
	sub, _, err := svc.join("p", chat, "shutdown")
	if err != nil {
		t.Fatal(err)
	}
	session := chat.nextSession(t)
	waitSignal(t, "ready", sub.ready)
	stopped := make(chan struct{})
	go func() { group.stop(); close(stopped) }()
	waitSignal(t, "the owner's stop returns while a room is open", stopped)
	waitSignal(t, "session closed", session.closed)
	waitSignal(t, "viewers are released", sub.ended)
	select {
	case <-sub.unavailable:
		t.Fatal("a shutdown is not an unavailable channel")
	default:
	}
	_, _, err = svc.join("p", chat, "late")
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("join after stop: %v", err)
	}
	if svc.Rooms() != 0 {
		t.Fatal("rooms must be gone after stop")
	}
}

func TestUserLimiterCapsOneAccountOnly(t *testing.T) {
	t.Parallel()
	var limiter userLimiter
	const limit = 4
	var releases []func()
	for range limit {
		release, ok := limiter.acquire("viewer", limit)
		if !ok {
			t.Fatal("a connection below the cap was refused")
		}
		releases = append(releases, release)
	}
	if _, ok := limiter.acquire("viewer", limit); ok {
		t.Fatal("the account exceeded its connection cap")
	}
	other, ok := limiter.acquire("someone-else", limit)
	if !ok {
		t.Fatal("one account's connections used up another account's")
	}
	other()

	releases[0]()
	releases[0]() // releasing twice frees one slot only
	if _, ok := limiter.acquire("viewer", limit); !ok {
		t.Fatal("a released connection did not free its slot")
	}
	if _, ok := limiter.acquire("viewer", limit); ok {
		t.Fatal("a double release freed two slots")
	}
	for _, release := range releases[1:] {
		release()
	}
	if len(limiter.active) != 1 {
		t.Fatalf("released accounts must not stay in the map: %v", limiter.active)
	}
}
