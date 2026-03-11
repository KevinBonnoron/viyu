// Package ocr reads the episode number off the thumbnail of a video. Everything that is plain Go (the
// address policy, the bounded download, the image validation and re-encoding, the interpretation of the
// words found) is built in every configuration and unit-tested without the engine. Only the native
// recognition engine, Tesseract, needs the `ocr` build tag (and CGO): without it New answers nil and
// Available false, so the server runs with recognition disabled.
package ocr

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// Options tune a reader. Every field is optional.
type Options struct {
	// HTTP is the client thumbnails are downloaded with. Nil builds the shared hardened client: public
	// addresses only (checked at connect time), port 443, short deadlines, no connection reuse. Its
	// redirect policy is replaced either way: a redirect must stay on an allowed host.
	HTTP *http.Client
	// Hosts are the image hosts a thumbnail may come from, matched exactly. Empty means the public image
	// service the platform thumbnails are served from.
	Hosts []string
	// Path is the shape of the path of an image on those hosts. Nil means the one of that service.
	Path *regexp.Regexp
	// QueryKeys are the only query parameters an address may carry. Empty means the ones that service
	// signs its addresses with.
	QueryKeys []string
}

// engine recognizes the words of a normalized image.
type engine interface {
	// recognize reads a grayscale PNG of at most engineWidth x engineHeight pixels. The native call is
	// synchronous: cancellation is checked before and after it, not by abandoning it.
	recognize(ctx context.Context, normalized []byte) ([]Word, error)
}

type reader struct {
	fetch  *fetcher
	engine engine
}

var _ series.EpisodeReader = (*reader)(nil)

func newReader(opts Options, e engine) *reader {
	return &reader{fetch: newFetcher(opts), engine: e}
}

// Episode reads the thumbnails of one video and answers the episode number they agree on, 0 when none
// shows one (or two of them disagree). Addresses the policy does not accept are not fetched and not
// errors: a thumbnail from elsewhere simply cannot be read here. When every acceptable address failed the
// error is ErrImage or ErrRecognition (wrapping the context's error when it ended).
func (r *reader) Episode(ctx context.Context, thumbnailURLs []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrRecognition, err)
	}
	var accepted []string
	for _, raw := range thumbnailURLs {
		if raw != "" && r.fetch.valid(raw) {
			accepted = append(accepted, raw)
		}
	}
	episode, succeeded := 0, false
	var failure error
	for _, raw := range accepted {
		found, err := r.read(ctx, raw)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return 0, fmt.Errorf("%w: %w", ErrRecognition, ctxErr)
			}
			failure = err
			continue
		}
		succeeded = true
		if found == 0 {
			continue
		}
		if episode != 0 && episode != found {
			return 0, nil
		}
		episode = found
	}
	if !succeeded && failure != nil {
		return 0, failure
	}
	return episode, nil
}

// read runs one thumbnail through download, validation and recognition.
func (r *reader) read(ctx context.Context, raw string) (int, error) {
	data, err := r.fetch.download(ctx, raw)
	if err != nil {
		return 0, err
	}
	return r.recognize(ctx, data)
}

// recognize reads the episode number off the bytes of an image.
func (r *reader) recognize(ctx context.Context, data []byte) (int, error) {
	normalized, width, height, err := normalize(ctx, data)
	if err != nil {
		return 0, err
	}
	words, err := r.engine.recognize(ctx, normalized)
	if err != nil || ctx.Err() != nil {
		return 0, ErrRecognition
	}
	return parseEpisode(words, width, height), nil
}
