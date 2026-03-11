package ocr

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeEngine answers the words a thumbnail would hold, by the size of the image it is given.
type fakeEngine struct {
	words []Word
	err   error
	calls atomic.Int32
}

func (e *fakeEngine) recognize(context.Context, []byte) ([]Word, error) {
	e.calls.Add(1)
	return e.words, e.err
}

// servingReader answers every thumbnail with the same image, counting what was fetched.
func servingReader(t *testing.T, image []byte, e engine) (*reader, *atomic.Int32) {
	t.Helper()
	var fetched atomic.Int32
	return newReader(Options{HTTP: clientFor(func(request *http.Request) (*http.Response, error) {
		fetched.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(image)), Header: make(http.Header), Request: request}, nil
	})}, e), &fetched
}

func TestEpisodeReadsTheNumberTheEngineFound(t *testing.T) {
	t.Parallel()
	engine := &fakeEngine{words: []Word{testWord("EP07", 97, 1, 100, 60, 64)}}
	r, fetched := servingReader(t, textPNG(t, "EP07"), engine)
	episode, err := r.Episode(context.Background(), []string{goodURL})
	if err != nil || episode != 7 || fetched.Load() != 1 || engine.calls.Load() != 1 {
		t.Fatalf("episode=%d err=%v fetched=%d calls=%d", episode, err, fetched.Load(), engine.calls.Load())
	}
	blank, _ := servingReader(t, textPNG(t, ""), &fakeEngine{})
	if episode, err := blank.Episode(context.Background(), []string{goodURL}); err != nil || episode != 0 {
		t.Fatalf("a blank image manufactured an episode: %d %v", episode, err)
	}
}

func TestEpisodeRequiresThumbnailsToAgree(t *testing.T) {
	t.Parallel()
	var call atomic.Int32
	engine := &sequenceEngine{answers: [][]Word{
		{testWord("EP07", 97, 1, 100, 60, 64)},
		{testWord("EP08", 97, 1, 100, 60, 64)},
		{testWord("EP07", 97, 1, 100, 60, 64)},
	}, call: &call}
	r, _ := servingReader(t, textPNG(t, "EP07"), engine)
	urls := []string{goodURL, "https://i.ytimg.com/vi/id/mq1.jpg"}
	if episode, err := r.Episode(context.Background(), urls); err != nil || episode != 0 {
		t.Fatalf("two thumbnails that disagree gave an episode: %d %v", episode, err)
	}
	agreeing := &sequenceEngine{answers: [][]Word{{testWord("EP07", 97, 1, 100, 60, 64)}, {}, {testWord("EP07", 97, 1, 100, 60, 64)}}, call: &atomic.Int32{}}
	r, _ = servingReader(t, textPNG(t, "EP07"), agreeing)
	if episode, err := r.Episode(context.Background(), append(urls, "https://i.ytimg.com/vi/id/mq2.jpg")); err != nil || episode != 7 {
		t.Fatalf("episode=%d err=%v", episode, err)
	}
}

type sequenceEngine struct {
	answers [][]Word
	call    *atomic.Int32
}

func (e *sequenceEngine) recognize(context.Context, []byte) ([]Word, error) {
	index := int(e.call.Add(1)) - 1
	return e.answers[min(index, len(e.answers)-1)], nil
}

func TestEpisodeSkipsAddressesTheCDNPolicyRefuses(t *testing.T) {
	t.Parallel()
	engine := &fakeEngine{words: []Word{testWord("EP07", 97, 1, 100, 60, 64)}}
	r, fetched := servingReader(t, textPNG(t, "EP07"), engine)
	foreign := []string{"", "https://example.test/vi/id/hqdefault.jpg", "http://i.ytimg.com/vi/id/hqdefault.jpg", "https://127.0.0.1/vi/id/x.jpg"}
	if episode, err := r.Episode(context.Background(), foreign); err != nil || episode != 0 || fetched.Load() != 0 || engine.calls.Load() != 0 {
		t.Fatalf("episode=%d err=%v fetched=%d", episode, err, fetched.Load())
	}
	if episode, err := r.Episode(context.Background(), nil); err != nil || episode != 0 {
		t.Fatalf("no thumbnails: %d %v", episode, err)
	}
	// An acceptable address among them is still read.
	if episode, err := r.Episode(context.Background(), append(foreign, goodURL)); err != nil || episode != 7 || fetched.Load() != 1 {
		t.Fatalf("episode=%d err=%v fetched=%d", episode, err, fetched.Load())
	}
}

func TestEpisodeFailuresAreFixedAndOnlyWhenNothingWasRead(t *testing.T) {
	t.Parallel()
	down := newReader(Options{HTTP: clientFor(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("remote secret")
	})}, &fakeEngine{})
	if _, err := down.Episode(context.Background(), []string{goodURL}); !errors.Is(err, ErrImage) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("download failure=%v", err)
	}
	broken, _ := servingReader(t, textPNG(t, "EP07"), &fakeEngine{err: errors.New("engine secret")})
	if _, err := broken.Episode(context.Background(), []string{goodURL}); !errors.Is(err, ErrRecognition) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("engine failure=%v", err)
	}
	notImage, _ := servingReader(t, []byte("not an image"), &fakeEngine{})
	if _, err := notImage.Episode(context.Background(), []string{goodURL}); !errors.Is(err, ErrRecognition) {
		t.Fatalf("bad image=%v", err)
	}
	// One thumbnail that fails does not hide the one that reads.
	var served atomic.Int32
	image := textPNG(t, "EP07")
	mixed := newReader(Options{HTTP: clientFor(func(request *http.Request) (*http.Response, error) {
		if served.Add(1) == 1 {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(image)), Header: make(http.Header), Request: request}, nil
	})}, &fakeEngine{words: []Word{testWord("EP07", 97, 1, 100, 60, 64)}})
	if episode, err := mixed.Episode(context.Background(), []string{goodURL, "https://i.ytimg.com/vi/id/mq1.jpg"}); err != nil || episode != 7 {
		t.Fatalf("episode=%d err=%v", episode, err)
	}
}

func TestEpisodeStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := &fakeEngine{}
	r, fetched := servingReader(t, textPNG(t, "EP07"), engine)
	_, err := r.Episode(ctx, []string{goodURL})
	if !errors.Is(err, ErrRecognition) || !errors.Is(err, context.Canceled) || fetched.Load() != 0 || engine.calls.Load() != 0 {
		t.Fatalf("cancelled read: err=%v fetched=%d", err, fetched.Load())
	}
	// Ending while the download runs is reported as the context's, not as a bad image.
	ctx, cancel = context.WithCancel(context.Background())
	during := newReader(Options{HTTP: clientFor(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, ctx.Err()
	})}, engine)
	if _, err := during.Episode(ctx, []string{goodURL}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
