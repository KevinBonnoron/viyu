package comments

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// fakePlatform serves comments from a function; every other method is unused here.
type fakePlatform struct {
	platform.Platform
	fetch func(ctx context.Context, videoID, cookieFile string) ([]platform.Comment, error)
}

func (fakePlatform) ID() string { return "fake" }
func (f fakePlatform) Comments(ctx context.Context, videoID, cookieFile string) ([]platform.Comment, error) {
	return f.fetch(ctx, videoID, cookieFile)
}

type jars struct {
	mu      sync.Mutex
	made    int
	cleaned int
}

func (j *jars) CookieFile(userID, platformID string) (string, func(), error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.made++
	return "/jar/" + userID, func() { j.mu.Lock(); j.cleaned++; j.mu.Unlock() }, nil
}

func newService(t *testing.T, fetch func(context.Context, string, string) ([]platform.Comment, error), cookies CookieProvider) *Service {
	t.Helper()
	group := jobs.NewGroup()
	t.Cleanup(group.Stop)
	return New(Options{Platforms: platform.NewRegistry(fakePlatform{fetch: fetch}), Cookies: cookies, Spawn: group.Go})
}

func TestPublicListsAreFetchedOnceAndReused(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	service := newService(t, func(context.Context, string, string) ([]platform.Comment, error) {
		calls.Add(1)
		return []platform.Comment{{ID: "one", Text: "hello"}}, nil
	}, nil)
	key := Key{Platform: "fake", VideoID: "v"}
	for range 3 {
		comments, err := service.List(context.Background(), key)
		if err != nil || len(comments) != 1 {
			t.Fatalf("comments %v: %v", comments, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("platform calls: %d", calls.Load())
	}
}

func TestAViewerLeavingDoesNotFailTheOthers(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	service := newService(t, func(context.Context, string, string) ([]platform.Comment, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return []platform.Comment{{ID: "one"}}, nil
	}, nil)
	key := Key{Platform: "fake", VideoID: "v"}
	first, leave := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, err := service.List(first, key); firstDone <- err }()
	<-started
	secondDone := make(chan error, 1)
	var got []platform.Comment
	go func() {
		var err error
		got, err = service.List(context.Background(), key)
		secondDone <- err
	}()
	// The second viewer has joined once the lookup counts two waiters: leaving earlier would abandon it.
	for service.lists.Waiting(key) < 2 {
		runtime.Gosched()
	}
	leave()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first viewer: %v", err)
	}
	close(release)
	if err := <-secondDone; err != nil || len(got) != 1 || calls.Load() != 1 {
		t.Fatalf("second viewer lost the shared lookup: %v (%d comments, %d calls)", err, len(got), calls.Load())
	}
}

func TestConnectedListsUseTheirOwnJarAndAreScopedToTheSession(t *testing.T) {
	t.Parallel()
	var files []string
	var mu sync.Mutex
	cookies := &jars{}
	service := newService(t, func(_ context.Context, _ string, cookieFile string) ([]platform.Comment, error) {
		mu.Lock()
		files = append(files, cookieFile)
		mu.Unlock()
		return []platform.Comment{{ID: "private"}}, nil
	}, cookies)
	a := Key{Platform: "fake", VideoID: "v", UserID: "alice", Revision: "r1"}
	for _, key := range []Key{a, a, {Platform: "fake", VideoID: "v", UserID: "alice", Revision: "r2"}, {Platform: "fake", VideoID: "v", UserID: "bob", Revision: "r1"}} {
		if _, err := service.List(context.Background(), key); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(files) != 3 || cookies.made != 3 || cookies.cleaned != 3 {
		t.Fatalf("lookups %v, jars made %d cleaned %d", files, cookies.made, cookies.cleaned)
	}
}

func TestInvalidateForgetsOnlyThatConnection(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	service := newService(t, func(context.Context, string, string) ([]platform.Comment, error) {
		calls.Add(1)
		return nil, nil
	}, &jars{})
	alice := Key{Platform: "fake", VideoID: "v", UserID: "alice", Revision: "r"}
	public := Key{Platform: "fake", VideoID: "v"}
	for _, key := range []Key{alice, public} {
		if _, err := service.List(context.Background(), key); err != nil {
			t.Fatal(err)
		}
	}
	service.Invalidate("alice", "fake")
	for _, key := range []Key{alice, public} {
		if _, err := service.List(context.Background(), key); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("lookups: %d, want the connected list fetched again and the public one reused", calls.Load())
	}
}

func TestFailuresAreNotCachedAndUnknownPlatformsAreRefused(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	boom := errors.New("boom")
	service := newService(t, func(context.Context, string, string) ([]platform.Comment, error) {
		if calls.Add(1) == 1 {
			return nil, boom
		}
		return []platform.Comment{{ID: "ok"}}, nil
	}, nil)
	key := Key{Platform: "fake", VideoID: "v"}
	if _, err := service.List(context.Background(), key); !errors.Is(err, boom) {
		t.Fatalf("first: %v", err)
	}
	if comments, err := service.List(context.Background(), key); err != nil || len(comments) != 1 {
		t.Fatalf("retry: %v %v", comments, err)
	}
	if _, err := service.List(context.Background(), Key{Platform: "nope", VideoID: "v"}); err == nil {
		t.Fatal("an unknown platform was served")
	}
}
