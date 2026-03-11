package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// recorder remembers the calls of a scripted Replay.
type recorder struct {
	mu    sync.Mutex
	calls []replayCall
}

type replayCall struct {
	id     string
	offset int
	cursor string
}

func (r *recorder) last() replayCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

func replayChat() (*fakeChat, *recorder) {
	chat := newFakeChat()
	rec := &recorder{}
	chat.replay = func(_ context.Context, id string, offset int, cursor string) (platform.ChatPage, error) {
		rec.mu.Lock()
		rec.calls = append(rec.calls, replayCall{id, offset, cursor})
		rec.mu.Unlock()
		if id == "500" {
			return platform.ChatPage{}, errors.New("secret upstream detail")
		}
		return platform.ChatPage{Messages: []platform.ChatMessage{{ID: "m-" + id}}, Next: "next"}, nil
	}
	return chat, rec
}

func TestReplayPassesTheOffsetAndTheCursor(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat, rec := replayChat()
	ctx := context.Background()

	page, err := svc.Replay(ctx, "p", chat, "123456", 600, "")
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "m-123456" || page.Next != "next" {
		t.Fatalf("page: %+v %v", page, err)
	}
	if got := rec.last(); got != (replayCall{"123456", 600, ""}) {
		t.Fatalf("upstream call: %+v", got)
	}
	// A cursor wins over an offset.
	if _, err := svc.Replay(ctx, "p", chat, "123456", 9, "cur5"); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(); got != (replayCall{"123456", 0, "cur5"}) {
		t.Fatalf("cursor call: %+v", got)
	}
}

func TestReplayRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat, _ := replayChat()
	for _, c := range []struct {
		id     string
		offset int
		cursor string
	}{
		{"abc", 0, ""},
		{"0123", 0, ""},
		{"12345678901234567890123", 0, ""},
		{"123", -1, ""},
		{"123", 10_000_001, ""},
		{"123", 0, "a b"},
		{"123", 0, `"}`},
		{"123", 0, strings.Repeat("a", 1001)},
	} {
		_, err := svc.Replay(context.Background(), "p", chat, c.id, c.offset, c.cursor)
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Code != apierr.CodeInvalidRequest {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if chat.replayCalls.Load() != 0 {
		t.Fatalf("invalid input must not reach the platform (%d calls)", chat.replayCalls.Load())
	}
	// The limits themselves are valid.
	if _, err := svc.Replay(context.Background(), "p", chat, "123", 10_000_000, strings.Repeat("a", 1000)); err != nil {
		t.Fatalf("boundary values: %v", err)
	}
}

func TestReplayFailuresAreBadGatewaysAndNotCached(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat, _ := replayChat()
	for range 2 {
		_, err := svc.Replay(context.Background(), "p", chat, "500", 1, "")
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway || apiErr.Code != apierr.CodeRequestFailed || apiErr.Message != "The chat replay is unavailable." {
			t.Fatalf("failure: %v", err)
		}
		if strings.Contains(apiErr.Message, "secret") || apiErr.Cause == nil || !strings.Contains(apiErr.Cause.Error(), "secret") {
			t.Fatalf("the cause is kept for the log and never sent: %+v", apiErr)
		}
	}
	if chat.replayCalls.Load() != 2 {
		t.Fatalf("a failure must not be cached: %d calls", chat.replayCalls.Load())
	}
}

func TestReplayCachesIdenticalRequestsBriefly(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat, _ := replayChat()
	ctx := context.Background()
	for range 3 {
		if _, err := svc.Replay(ctx, "p", chat, "123456", 600, ""); err != nil {
			t.Fatal(err)
		}
	}
	if chat.replayCalls.Load() != 1 {
		t.Fatalf("identical requests must be cached: %d calls", chat.replayCalls.Load())
	}
	for _, c := range []struct {
		key, id string
		offset  int
		cursor  string
	}{{"p", "123456", 601, ""}, {"p", "123456", 0, "cur5"}, {"p", "654321", 600, ""}, {"q", "123456", 600, ""}} {
		if _, err := svc.Replay(ctx, c.key, chat, c.id, c.offset, c.cursor); err != nil {
			t.Fatal(err)
		}
	}
	if chat.replayCalls.Load() != 5 {
		t.Fatalf("another offset, cursor, video or platform is another entry: %d calls", chat.replayCalls.Load())
	}
	// With a cursor the offset does not matter, so it does not split the entry.
	if _, err := svc.Replay(ctx, "p", chat, "123456", 77, "cur5"); err != nil {
		t.Fatal(err)
	}
	if chat.replayCalls.Load() != 5 {
		t.Fatalf("the offset is ignored next to a cursor: %d calls", chat.replayCalls.Load())
	}
}

func TestReplayCacheExpires(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.VODCacheTTL = 30 * time.Millisecond })
	chat, _ := replayChat()
	ctx := context.Background()
	if _, err := svc.Replay(ctx, "p", chat, "1", 0, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := svc.Replay(ctx, "p", chat, "1", 0, ""); err != nil {
		t.Fatal(err)
	}
	if chat.replayCalls.Load() != 2 {
		t.Fatalf("an expired page must be fetched again: %d calls", chat.replayCalls.Load())
	}
}

func TestReplayCacheIsBounded(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, func(o *Options) { o.VODCacheEntries = 4 })
	chat, _ := replayChat()
	for offset := range 20 {
		if _, err := svc.Replay(context.Background(), "p", chat, "1", offset, ""); err != nil {
			t.Fatal(err)
		}
		if n := svc.vod.Len(); n > 4 {
			t.Fatalf("cache must stay bounded: %d", n)
		}
	}
}

func TestReplayConcurrentRequestsShareOneFetch(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	chat := newFakeChat()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	chat.replay = func(ctx context.Context, id string, _ int, _ string) (platform.ChatPage, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
		}
		return platform.ChatPage{Next: "x"}, nil
	}
	var wg, entered sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		entered.Add(1)
		go func() {
			defer wg.Done()
			entered.Done()
			_, err := svc.Replay(context.Background(), "p", chat, "42", 0, "")
			results <- err
		}()
	}
	entered.Wait()
	waitSignal(t, "fetch started", started)
	// Let every caller reach the shared fetch before it answers.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if chat.replayCalls.Load() != 1 {
		t.Fatalf("concurrent identical requests fetch once: %d", chat.replayCalls.Load())
	}
}

func TestReplayAfterTheOwnerStoppedFails(t *testing.T) {
	t.Parallel()
	svc, group := newService(t, nil)
	chat, _ := replayChat()
	group.stop()
	_, err := svc.Replay(context.Background(), "p", chat, "42", 0, "")
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway {
		t.Fatalf("a replay after the owner stopped: %v", err)
	}
}
