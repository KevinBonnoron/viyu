package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// catalogServer answers the catalog query like Twitch does: pages of at most `first` videos, the cursor of a
// page being the same on every edge, and the playback access token of the VODs listed in restricted.
type catalogServer struct {
	total      int
	restricted map[string]bool
	pages      atomic.Int32
	access     atomic.Int32
}

func (c *catalogServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string `json:"query"`
			Variables struct {
				Login string `json:"login"`
				First int    `json:"first"`
				After string `json:"after"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(readBody(t, r), &body); err != nil {
			t.Errorf("bad request: %v", err)
		}
		if strings.Contains(body.Query, "ViyuAccess") {
			c.access.Add(1)
			data := map[string]any{}
			for index := 0; ; index++ {
				marker := fmt.Sprintf(`v%d: videoPlaybackAccessToken(id: "`, index)
				at := strings.Index(body.Query, marker)
				if at < 0 {
					break
				}
				id := body.Query[at+len(marker):]
				id = id[:strings.Index(id, `"`)]
				bitrates := "[]"
				if c.restricted[id] {
					bitrates = `["chunked"]`
				}
				data[fmt.Sprintf("v%d", index)] = map[string]string{"value": `{"chansub":{"restricted_bitrates":` + bitrates + `}}`}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		c.pages.Add(1)
		if body.Variables.Login != "streamer" {
			_, _ = io.WriteString(w, `{"data":{"user":null}}`)
			return
		}
		start := 0
		if body.Variables.After != "" {
			start, _ = strconv.Atoi(strings.TrimPrefix(body.Variables.After, "after-"))
		}
		end := min(start+body.Variables.First, c.total)
		edges := []map[string]any{}
		for index := start; index < end; index++ {
			// Newest first: the first video has the highest id.
			id := strconv.Itoa(1000 + c.total - index)
			thumbnail := "https://static-cdn.example/" + id + ".jpg"
			if id == "1002" {
				thumbnail = "https://vod-secure.twitch.tv/_404/404_processing_640x360.png"
			}
			edges = append(edges, map[string]any{"cursor": "after-" + strconv.Itoa(end), "node": map[string]any{
				"id": id, "title": "Episode " + strconv.Itoa(c.total-index), "createdAt": "2026-11-06T12:30:00Z",
				"lengthSeconds": 3600, "viewCount": index, "previewThumbnailURL": thumbnail,
			}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"user": map[string]any{"videos": map[string]any{
			"edges": edges, "pageInfo": map[string]any{"hasNextPage": end < c.total},
		}}}})
	}
}

func catalogOf(t *testing.T, p *Platform, login string) ([]platform.Video, error) {
	t.Helper()
	var got []platform.Video
	err := p.CatalogSync().Catalog(context.Background(), platform.Channel{ID: login, Name: "The Streamer", Thumbnail: "https://static-cdn.example/avatar.png"}, "", func(v platform.Video) error {
		got = append(got, v)
		return nil
	})
	return got, err
}

// The whole video list is read page after page, every kind of video included, each with what a series needs:
// its title, its exact date, duration and artwork, and whether only subscribers can watch it.
func TestCatalogPagesThroughEveryVideoOfAChannel(t *testing.T) {
	t.Parallel()
	server := &catalogServer{total: 250, restricted: map[string]bool{"1250": true}}
	p := graphQLPlatform(t, server.handler(t), Options{})

	videos, err := catalogOf(t, p, "streamer")
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 250 || server.pages.Load() != 3 {
		t.Fatalf("%d videos in %d pages, want 250 in 3", len(videos), server.pages.Load())
	}
	seen := map[string]bool{}
	for _, video := range videos {
		if seen[video.ID] {
			t.Fatalf("video %s listed twice", video.ID)
		}
		seen[video.ID] = true
	}
	first := videos[0]
	if first.ID != "1250" || first.Platform != ID || first.Title != "Episode 250" || first.OriginalTitle != "Episode 250" || !first.OriginalTitleVerified ||
		first.ChannelID != "streamer" || first.ChannelName != "The Streamer" || first.ChannelThumbnail != "https://static-cdn.example/avatar.png" ||
		first.Published != "2026-11-06T12:30:00Z" || first.Duration != 3600 || first.Thumbnail != "https://static-cdn.example/1250.jpg" ||
		len(first.Thumbnails) != 1 || !first.SubscriberOnly || !first.SubscriberChecked {
		t.Fatalf("first video: %+v", first)
	}
	if public := videos[1]; public.SubscriberOnly || !public.SubscriberChecked {
		t.Fatalf("a public VOD: %+v", public)
	}
	for _, video := range videos {
		if video.ID == "1002" && (video.Thumbnail != "" || len(video.Thumbnails) != 0) {
			t.Fatalf("the placeholder of a VOD still processing was kept: %+v", video)
		}
	}
	// 15 VODs per lookup, asked page by page: 100, 100 and 50 videos.
	if want := int32(7 + 7 + 4); server.access.Load() != want {
		t.Fatalf("%d access lookups, want %d", server.access.Load(), want)
	}
}

func TestCatalogOfAChannelWithoutVideosIsEmpty(t *testing.T) {
	t.Parallel()
	p := graphQLPlatform(t, (&catalogServer{}).handler(t), Options{})
	videos, err := catalogOf(t, p, "streamer")
	if err != nil || len(videos) != 0 {
		t.Fatalf("videos=%v err=%v", videos, err)
	}
}

// An error of visit is the caller's: it stops the listing and comes back as it is.
func TestCatalogStopsWhenTheCallerDoes(t *testing.T) {
	t.Parallel()
	server := &catalogServer{total: 250}
	p := graphQLPlatform(t, server.handler(t), Options{})
	stop := errors.New("stop")
	count := 0
	err := p.CatalogSync().Catalog(context.Background(), platform.Channel{ID: "streamer"}, "", func(platform.Video) error {
		count++
		return stop
	})
	if !errors.Is(err, stop) || count != 1 || server.pages.Load() != 1 {
		t.Fatalf("err=%v count=%d pages=%d", err, count, server.pages.Load())
	}
}

// A listing that cannot be followed to its end fails: the caller drops what a finished listing did not show.
func TestCatalogFailsInsteadOfEndingEarly(t *testing.T) {
	t.Parallel()
	node := `{"cursor":"c","node":{"id":"123","title":"t","createdAt":"2026-11-06T12:30:00Z"}}`
	for name, body := range map[string]string{
		"unknown channel":      `{"data":{"user":null}}`,
		"graphql errors":       `{"errors":[{"message":"service error"}]}`,
		"next page, no cursor": `{"data":{"user":{"videos":{"edges":[],"pageInfo":{"hasNextPage":true}}}}}`,
		"same cursor again":    `{"data":{"user":{"videos":{"edges":[` + node + `],"pageInfo":{"hasNextPage":true}}}}}`,
		"bad video id":         `{"data":{"user":{"videos":{"edges":[{"cursor":"c","node":{"id":"not-a-vod"}}],"pageInfo":{"hasNextPage":false}}}}}`,
		"null video":           `{"data":{"user":{"videos":{"edges":[{"cursor":"c","node":null}],"pageInfo":{"hasNextPage":false}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(string(readBody(t, r)), "ViyuAccess") {
					_, _ = io.WriteString(w, `{"data":{}}`)
					return
				}
				_, _ = io.WriteString(w, body)
			}, Options{})
			_, err := catalogOf(t, p, "streamer")
			assertCode(t, err, apierr.CodeRequestFailed)
		})
	}
	assertCode(t, errOf(catalogOf(t, newPlatform(t, Options{}), "not a login")), apierr.CodeInvalidSourceArg)
}

func errOf(_ []platform.Video, err error) error { return err }

// Twitch answering 429 is what yt-dlp hitting a 429 is: the error says so, and the runner's backoff, shared
// with every other download, holds the next request back without reaching Twitch.
func TestCatalogRateLimitStartsTheSharedBackoff(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	runner := ytdlp.New(ytdlp.Options{Binary: "unused", BackoffBase: time.Minute})
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}, Options{Runner: runner})

	_, err := catalogOf(t, p, "streamer")
	assertCode(t, err, apierr.CodeUpstreamRateLimit)
	if !errors.Is(err, ytdlp.ErrRateLimited) || runner.RetryAfter() <= 0 {
		t.Fatalf("the limit was not shared: %v (backoff %s)", err, runner.RetryAfter())
	}
	_, err = catalogOf(t, p, "streamer")
	var limited *ytdlp.Error
	assertCode(t, err, apierr.CodeUpstreamRateLimit)
	if !errors.As(err, &limited) || limited.RetryAfter <= 0 || requests.Load() != 1 {
		t.Fatalf("second attempt: %v after %d requests", err, requests.Load())
	}
}

// A request takes a slot of the runner, like a yt-dlp process: with every slot taken the listing waits.
func TestCatalogWaitsForARunnerSlot(t *testing.T) {
	t.Parallel()
	runner := ytdlp.New(ytdlp.Options{Binary: "unused", Concurrency: 1})
	server := &catalogServer{total: 1}
	p := graphQLPlatform(t, server.handler(t), Options{Runner: runner})
	slot, err := runner.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := catalogOf(t, p, "streamer")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the listing ran without a free slot: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	slot.Release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.CatalogSync().Catalog(ctx, platform.Channel{ID: "streamer"}, "", func(platform.Video) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

// A limit met while flagging subscriber-only VODs stops the listing too: the shared backoff has to start.
func TestCatalogRateLimitedFlagLookupStopsTheListing(t *testing.T) {
	t.Parallel()
	runner := ytdlp.New(ytdlp.Options{Binary: "unused"})
	server := &catalogServer{total: 3}
	inner := server.handler(t)
	p := graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		if strings.Contains(string(payload), "ViyuAccess") {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(payload)))
		inner(w, r)
	}, Options{Runner: runner})
	_, err := catalogOf(t, p, "streamer")
	assertCode(t, err, apierr.CodeUpstreamRateLimit)
	if runner.RetryAfter() <= 0 {
		t.Fatal("no backoff after the limit")
	}
}
