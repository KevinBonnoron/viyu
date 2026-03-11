// Package media is the HLS proxy: it fetches a video's manifest from the platform, serves the playlists with
// every address rewritten to point back at the server, and fetches the signed resources they name on behalf
// of the viewer. Private manifests and their signed resources never enter the public database, and the
// browser never talks to the upstream hosts.
//
// The package works on plain net/http. Authorization of a request (account, activation, connection
// revision) happens in the HTTP layer before it calls in, and reaches the proxy as a Key: resources are
// addressed per Key, so one account can never reach another's manifest.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/cache"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/safehttp"
)

const (
	defaultMaxEntries   = 128
	defaultMaxResources = 8192
	// lookupTimeout bounds the platform lookup of a manifest, which belongs to no single request.
	lookupTimeout = 90 * time.Second
	// maxPlaylist bounds a playlist read into memory.
	maxPlaylist = 8 << 20
)

var playlistURI = regexp.MustCompile(`URI="([^"]+)"`)

// Key names one authorized manifest: the viewer's account and connection revision, and the video. VideoID
// is the platform's native id (for the lookups); RecordID the id clients know (for the rewritten URLs).
type Key struct{ VideoID, RecordID, Platform, UserID, Revision string }

// CookieProvider hands out a private cookie jar of a connected source for the duration of one lookup.
type CookieProvider interface {
	CookieFile(userID, platformID string) (path string, cleanup func(), err error)
}

// Options configures a Proxy.
type Options struct {
	// Platforms resolves the platform of a Key (required).
	Platforms *platform.Registry
	// DefaultPlatform is the platform id left out of rewritten resource URLs; the registry's default when empty.
	DefaultPlatform string
	// Cookies provides the session of a connected source; needed only when a request names a source.
	Cookies CookieProvider
	// HTTP fetches the upstream; nil means safehttp.Media().
	HTTP *http.Client
	// Spawn owns the detached manifest lookups (required).
	Spawn cache.Spawner
	// MaxEntries bounds the manifests kept at once, MaxResources the resource addresses remembered per
	// manifest (least recently used first out); defaults 128 / 8192.
	MaxEntries, MaxResources int
	// Now replaces the clock (tests).
	Now func() time.Time
}

// Proxy serves HLS manifests and their resources. It is safe for concurrent use.
type Proxy struct {
	platforms       *platform.Registry
	defaultPlatform string
	cookies         CookieProvider
	client          *http.Client
	maxResources    int
	now             func() time.Time

	manifests *cache.TTL[Key, *entry]
	lookups   *cache.Flight[Key, *entry]
}

// New builds a Proxy.
func New(opts Options) *Proxy {
	if opts.Platforms == nil {
		panic("media: Options.Platforms is required")
	}
	if opts.Spawn == nil {
		panic("media: Options.Spawn is required")
	}
	p := &Proxy{
		platforms:       opts.Platforms,
		defaultPlatform: opts.DefaultPlatform,
		cookies:         opts.Cookies,
		client:          opts.HTTP,
		maxResources:    opts.MaxResources,
		now:             opts.Now,
	}
	if p.defaultPlatform == "" {
		p.defaultPlatform = opts.Platforms.Default().ID()
	}
	if p.client == nil {
		p.client = safehttp.Media()
	}
	if p.maxResources <= 0 {
		p.maxResources = defaultMaxResources
	}
	if p.now == nil {
		p.now = time.Now
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	p.manifests = cache.New[Key, *entry](cache.Options{Max: maxEntries, Now: p.now})
	p.lookups = cache.NewFlight[Key, *entry](opts.Spawn, lookupTimeout)
	return p
}

// Remember records a manifest learned with the video metadata, so the first playback does not ask the
// platform again.
func (p *Proxy) Remember(key Key, m platform.Manifest) { p.remember(key, m) }

func (p *Proxy) remember(key Key, m platform.Manifest) *entry {
	if current, ok := p.manifests.Get(key); ok && current.manifest.URL == m.URL {
		p.manifests.Touch(key, current.lifetime(p.now()))
		return current
	}
	fresh := newEntry(m, p.maxResources)
	p.manifests.PutFor(key, fresh, fresh.lifetime(p.now()))
	return fresh
}

// Purge drops the manifests that expired; run it periodically to bound the memory of entries nobody asks
// for any more (lookups already ignore expired ones).
func (p *Proxy) Purge() { p.manifests.Purge() }

// live returns the manifest in memory and renews its lifetime.
func (p *Proxy) live(key Key) *entry {
	current, ok := p.manifests.Get(key)
	if !ok {
		return nil
	}
	if _, ok := p.manifests.Touch(key, current.lifetime(p.now())); !ok {
		return nil
	}
	return current
}

// forget drops the manifest the upstream just refused, unless a newer one replaced it meanwhile.
func (p *Proxy) forget(key Key, stale *entry) {
	if current, ok := p.manifests.Get(key); ok && current == stale {
		p.manifests.Delete(key)
	}
}

// manifest answers from the manifests in memory or asks the platform once for all concurrent viewers of
// the same video. The lookup does not belong to the request that started it: it is bounded by its own
// deadline and is cancelled only when every waiting request has gone.
func (p *Proxy) manifest(ctx context.Context, key Key, source string) (*entry, error) {
	if live := p.live(key); live != nil {
		return live, nil
	}
	found, err := p.lookups.Do(ctx, key, func(ctx context.Context) (*entry, error) {
		plat, ok := p.platforms.Get(key.Platform)
		if !ok {
			return nil, apierr.StreamUnavailable()
		}
		cookies := ""
		if source != "" {
			if p.cookies == nil {
				return nil, errors.New("no cookie provider")
			}
			path, cleanup, err := p.cookies.CookieFile(key.UserID, source)
			if err != nil {
				return nil, err
			}
			if cleanup != nil {
				defer cleanup()
			}
			cookies = path
		}
		manifest, err := plat.PlaybackManifest(ctx, key.VideoID, cookies)
		if err != nil {
			return nil, err
		}
		return p.remember(key, manifest), nil
	})
	if err != nil {
		return nil, lookupFailure(err, source)
	}
	return found, nil
}

// lookupFailure keeps what the platform classified (a subscriber-only VOD, an expired session) and hides
// everything else behind a generic failure.
func lookupFailure(err error, source string) *apierr.Error {
	var safe *apierr.Error
	switch {
	case errors.As(err, &safe):
		return safe
	case errors.Is(err, cache.ErrRefused):
		return apierr.New(http.StatusServiceUnavailable, apierr.CodeShuttingDown, "The server is shutting down.")
	case source != "":
		return apierr.RequestFailed().WithCause(err)
	}
	return apierr.Internal("Stream failed", err)
}

// ServeManifest answers the master playlist of the video, rewritten to point back at the proxy. A returned
// error has not written anything beyond the hardening headers.
func (p *Proxy) ServeManifest(w http.ResponseWriter, r *http.Request, key Key, source string) error {
	harden(w)
	found, err := p.manifest(r.Context(), key, source)
	if err != nil {
		return err
	}
	return p.proxy(w, r, key, found, found.manifest.URL, true, source)
}

// ServeResource answers a variant playlist, segment or other resource a rewritten playlist pointed at.
func (p *Proxy) ServeResource(w http.ResponseWriter, r *http.Request, key Key, resourceID, source string) error {
	harden(w)
	var remote string
	found := p.live(key)
	if found != nil {
		remote = found.resources.lookup(resourceID)
	}
	if remote == "" {
		return apierr.New(http.StatusNotFound, apierr.CodeResourceExpired, "Media resource expired. Start playback again.")
	}
	return p.proxy(w, r, key, found, remote, false, source)
}

func (p *Proxy) proxy(w http.ResponseWriter, r *http.Request, key Key, found *entry, remote string, playlist bool, source string) error {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, remote, nil)
	if err != nil {
		return apierr.Internal("Stream failed", err)
	}
	plat, _ := p.platforms.Get(key.Platform)
	trusted := func(host string) bool { return plat != nil && plat.TrustedMediaHost(host) }
	for name, value := range requestHeaders(trusted, found.manifest, request.URL) {
		request.Header.Set(name, value)
	}
	request.Header.Set("Accept-Encoding", "identity")
	if byteRange := r.Header.Get("Range"); byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response, err := p.client.Do(request)
	if err != nil {
		if source != "" {
			return apierr.RequestFailed().WithCause(err)
		}
		return apierr.Internal("Stream failed", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusGone {
			p.forget(key, found)
		}
		// Do not expose signed media URLs or connected-account downloader details.
		return apierr.New(http.StatusBadGateway, apierr.CodeStreamUpstream, "The upstream media resource is unavailable")
	}
	upstreamType := response.Header.Get("Content-Type")
	if playlist || strings.Contains(upstreamType, "mpegurl") || strings.HasSuffix(response.Request.URL.Path, ".m3u8") {
		data, err := io.ReadAll(io.LimitReader(response.Body, maxPlaylist+1))
		if err != nil {
			return apierr.Internal("Stream failed", err)
		}
		if len(data) > maxPlaylist || !bytes.HasPrefix(bytes.TrimSpace(data), []byte("#EXTM3U")) {
			return apierr.Internal("Stream failed", errors.New("invalid HLS playlist"))
		}
		rewritten, err := p.rewrite(key, found, response.Request.URL, data, source)
		if err != nil {
			return apierr.Internal("Stream failed", err)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, rewritten); err != nil {
			abort()
		}
		return nil
	}
	safeType, isMedia := contentType(upstreamType)
	w.Header().Set("Content-Type", safeType)
	if !isMedia {
		w.Header().Set("Content-Disposition", "attachment")
	}
	for _, name := range []string{"Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		abort()
	}
	return nil
}

// abort ends a response that failed after its status line was sent. There is no error to report any more,
// and returning normally would let the server finish the body cleanly (a chunked response would look
// complete), so the player would take a truncated segment for a whole one. net/http treats this panic as
// "close the connection without logging", which is the one signal the client reliably sees.
func abort() { panic(http.ErrAbortHandler) }

// rewrite points every address of a playlist at the proxy and remembers what each stands for.
func (p *Proxy) rewrite(key Key, found *entry, base *url.URL, data []byte, source string) (string, error) {
	var result strings.Builder
	result.Grow(len(data))
	var resourceErr error
	address := func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			resourceErr = err
			return ""
		}
		resolved := base.ResolveReference(parsed)
		if resolved.Scheme != "https" && resolved.Scheme != "http" {
			resourceErr = fmt.Errorf("unsupported HLS resource scheme")
			return ""
		}
		remote := resolved.String()
		digest := sha256.Sum256([]byte(remote))
		id := hex.EncodeToString(digest[:16])
		found.resources.remember(id, remote)
		// Resource URLs are client-facing: they address the video by its record id, like every other
		// API path (Key.VideoID stays native for the platform calls).
		path := "/api/stream/" + url.PathEscape(key.RecordID) + "/resource/" + id
		query := url.Values{}
		if source != "" {
			query.Set("source", source)
		}
		if key.Platform != p.defaultPlatform {
			query.Set("platform", key.Platform)
		}
		if len(query) != 0 {
			path += "?" + query.Encode()
		}
		return path
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "#") {
			line = playlistURI.ReplaceAllStringFunc(line, func(attribute string) string { return `URI="` + address(attribute[5:len(attribute)-1]) + `"` })
		} else if strings.TrimSpace(line) != "" {
			line = address(strings.TrimSpace(line))
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	if resourceErr != nil {
		return "", resourceErr
	}
	return result.String(), nil
}
