// Package media proxies HLS without exposing upstream URLs.
// The HTTP layer authorizes requests; Key isolates accounts and connection revisions.
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
	"strconv"
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
	maxPlaylist   = 8 << 20
)

var playlistURI = regexp.MustCompile(`URI="([^"]+)"`)

// Key names one authorized manifest: the viewer's account and connection revision, and the video. VideoID
// is the platform's native id (for the lookups); RecordID the id clients know (for the rewritten URLs).
type Key struct{ VideoID, RecordID, Platform, UserID, Revision string }

// CookieProvider hands out a private cookie jar of a connected source for the duration of one lookup.
type CookieProvider interface {
	CookieFile(userID, platformID string) (path string, cleanup func(), err error)
}

type Options struct {
	Platforms *platform.Registry
	// DefaultPlatform is the platform id left out of rewritten resource URLs; the registry's default when empty.
	DefaultPlatform string
	// Cookies provides the session of a connected source; needed only when a request names a source.
	Cookies CookieProvider
	// HTTP fetches the upstream; nil means safehttp.Media().
	HTTP *http.Client
	// Spawn owns the detached manifest lookups (required).
	Spawn cache.Spawner
	// Defaults: 128 manifests and 8192 resources per manifest.
	MaxEntries, MaxResources int
	Now                      func() time.Time
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
	current, ok := p.manifests.Get(key)
	if ok && current.manifest.URL == m.URL {
		p.manifests.Touch(key, manifestLife)
		return current
	}
	fresh := newEntry(m, p.maxResources)
	if ok {
		fresh.resources = current.resources
	}
	p.manifests.PutFor(key, fresh, manifestLife)
	return fresh
}

func (p *Proxy) Purge() { p.manifests.Purge() }

func (p *Proxy) live(key Key) *entry {
	current, ok := p.manifests.Get(key)
	if !ok {
		return nil
	}
	if _, ok := p.manifests.Touch(key, manifestLife); !ok {
		return nil
	}
	return current
}

// manifest answers from the manifests in memory or asks the platform once for all concurrent viewers of
// the same video. The lookup does not belong to the request that started it: it is bounded by its own
// deadline and is cancelled only when every waiting request has gone.
func (p *Proxy) manifest(ctx context.Context, key Key, source string) (*entry, error) {
	live := p.live(key)
	if live != nil && !live.expired(p.now()) {
		return live, nil
	}
	return p.refresh(ctx, key, source, live)
}

func (p *Proxy) refresh(ctx context.Context, key Key, source string, stale *entry) (*entry, error) {
	found, err := p.lookups.Do(ctx, key, func(ctx context.Context) (*entry, error) {
		if current := p.live(key); current != nil && current != stale {
			return current, nil
		}
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
		fresh := newEntry(manifest, p.maxResources)
		if stale != nil {
			fresh.resources = stale.resources
		}
		p.manifests.PutFor(key, fresh, manifestLife)
		return fresh, nil
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
	return p.proxy(w, r, key, found, found.manifest.URL, "", true, source)
}

func (p *Proxy) ServeResource(w http.ResponseWriter, r *http.Request, key Key, resourceID, source string) error {
	harden(w)
	found := p.live(key)
	var item resource
	var ok bool
	if found != nil {
		item, ok = found.resource(resourceID)
	}
	if !ok {
		return apierr.New(http.StatusNotFound, apierr.CodeResourceExpired, "Media resource expired.")
	}
	remote := item.url
	if found.expired(p.now()) || (item.generation != nil && item.generation != found.generation) {
		var err error
		found, remote, err = p.recoverResource(r.Context(), key, source, found, resourceID, remote)
		if err != nil {
			return err
		}
	}
	return p.proxy(w, r, key, found, remote, resourceID, false, source)
}

func (p *Proxy) proxy(w http.ResponseWriter, r *http.Request, key Key, found *entry, remote, resourceID string, playlist bool, source string) error {
	return p.proxyAttempt(w, r, key, found, remote, resourceID, playlist, source, true)
}

func (p *Proxy) proxyAttempt(w http.ResponseWriter, r *http.Request, key Key, found *entry, remote, resourceID string, playlist bool, source string, recover bool) error {
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
		if recover && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusGone) {
			response.Body.Close()
			var err error
			if playlist {
				found, err = p.refresh(r.Context(), key, source, found)
				if err == nil {
					remote = found.manifest.URL
				}
			} else {
				found, remote, err = p.recoverResource(r.Context(), key, source, found, resourceID, remote)
			}
			if err != nil {
				return err
			}
			return p.proxyAttempt(w, r, key, found, remote, resourceID, playlist, source, false)
		}
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
		rewritten, err := p.rewriteAt(key, found, response.Request.URL, data, source, resourceID)
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

// Abort incomplete responses so the player cannot mistake a truncated segment for a complete one.
func abort() { panic(http.ErrAbortHandler) }

// resourceIdentity removes only signing fields; all other URL components remain part of the identity.
// The URL is never synthesized from this identity: it must be found in a fresh upstream playlist.
func resourceIdentity(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	parts := strings.Split(u.EscapedPath(), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "expire", "signature", "sig", "lsig":
			parts[i+1] = ""
			i++
		}
	}
	escaped := strings.Join(parts, "/")
	u.Path, err = url.PathUnescape(escaped)
	if err != nil {
		return ""
	}
	u.RawPath = escaped
	for _, name := range []string{"expire", "expires", "sig", "signature", "lsig", "hdnts", "hdnea", "Policy", "Signature", "Key-Pair-Id"} {
		q.Del(name)
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String()
}

// playlistSemantics supplies an independent identity for opaque signed paths.
// Variant attributes must match exactly; live segments additionally require a sequence and
// program date/time, so a changed broadcast or a slid window cannot select a neighbour.
type playlistSemantics struct {
	sequence                          int64
	haveSequence                      bool
	stream, duration, date, byteRange string
}

func (s *playlistSemantics) line(line string) string {
	switch {
	case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
		n, err := strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
		s.sequence, s.haveSequence = n, err == nil && n >= 0
	case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
		s.stream = line
	case strings.HasPrefix(line, "#EXTINF:"):
		s.duration = line
	case strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:"):
		s.date = line
	case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
		s.byteRange = line
	case line != "" && !strings.HasPrefix(line, "#"):
		semantic := ""
		if s.stream != "" {
			semantic = "variant:" + s.stream
		} else if s.haveSequence && s.date != "" && s.duration != "" {
			semantic = fmt.Sprintf("segment:%d:%s:%s:%s", s.sequence, s.date, s.duration, s.byteRange)
		}
		s.stream, s.date, s.duration, s.byteRange = "", "", "", ""
		if s.haveSequence {
			s.sequence++
		}
		return semantic
	}
	return ""
}
func normalizedPlaylistLines(data []byte, base *url.URL) []string {
	lines := strings.Split(string(data), "\n")
	pending := -1
	previous := ""
	var end int64
	validPrevious := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
			pending = i
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			validPrevious, pending = false, -1
			continue
		}
		current := base.ResolveReference(u).String()
		if pending < 0 {
			validPrevious = false
			continue
		}
		value := strings.TrimPrefix(strings.TrimSpace(lines[pending]), "#EXT-X-BYTERANGE:")
		lengthText, offsetText, explicit := strings.Cut(value, "@")
		length, err := strconv.ParseInt(lengthText, 10, 64)
		offset := end
		if explicit && err == nil {
			offset, err = strconv.ParseInt(offsetText, 10, 64)
		} else if !validPrevious || previous != current {
			err = errors.New("invalid implicit byte range")
		}
		if err != nil || length <= 0 || offset < 0 || offset > int64(^uint64(0)>>1)-length {
			validPrevious, pending = false, -1
			continue
		}
		lines[pending] = fmt.Sprintf("#EXT-X-BYTERANGE:%d@%d", length, offset)
		previous, end, validPrevious, pending = current, offset+length, true, -1
	}
	return lines
}
func semanticLine(s *playlistSemantics, line string, base *url.URL) string {
	byteRange := s.byteRange
	semantic := s.line(line)
	if semantic == "" && byteRange != "" && line != "" && !strings.HasPrefix(line, "#") {
		if u, err := url.Parse(line); err == nil {
			return "range:" + resourceIdentity(base.ResolveReference(u).String()) + ":" + byteRange
		}
	}
	return semantic
}

func upstreamUnavailable() error {
	return apierr.New(http.StatusBadGateway, apierr.CodeStreamUpstream, "The upstream media resource is unavailable")
}

// recoverResource walks the remembered playlist ancestry, finding the same resource in freshly fetched
// playlists. An absent or ambiguous identity is never replaced by a neighbour.
func (p *Proxy) recoverResource(ctx context.Context, key Key, source string, stale *entry, id, attempted string) (*entry, string, error) {
	var chain []resource
	seen := make(map[string]bool)
	for current := id; current != ""; {
		if seen[current] || len(chain) >= 16 {
			return nil, "", upstreamUnavailable()
		}
		seen[current] = true
		item, ok := stale.resource(current)
		if !ok {
			return nil, "", upstreamUnavailable()
		}
		chain = append(chain, item)
		current = item.parent
	}
	fresh := p.live(key)
	if fresh == nil {
		return nil, "", upstreamUnavailable()
	}
	if chain[0].url != attempted && chain[0].generation == fresh.generation && !fresh.expired(p.now()) {
		return fresh, chain[0].url, nil
	}
	var err error
	refreshedRoot := false
	if fresh == stale && (chain[0].generation == fresh.generation || fresh.expired(p.now())) {
		refreshedRoot = true
		fresh, err = p.refresh(ctx, key, source, stale)
		if err != nil {
			return nil, "", err
		}
	}
	for {
		remote := fresh.manifest.URL
		restart := false
		for i := len(chain) - 1; i >= 0; i-- {
			remote, err = p.findResource(ctx, key, fresh, remote, chain[i])
			if err != nil {
				if errors.Is(err, errPlaylistRefused) && !refreshedRoot {
					refreshedRoot = true
					fresh, err = p.refresh(ctx, key, source, fresh)
					if err != nil {
						return nil, "", err
					}
					restart = true
					break
				}
				return nil, "", upstreamUnavailable()
			}
			fresh.resources.rememberChild(chain[i].id, remote, chain[i].parent, chain[i].semantic, fresh.generation, chain[i].retained)
			if current := p.live(key); current != nil && current.generation.id > fresh.generation.id {
				fresh = current
				restart = true
				break
			}
			if recorded, ok := fresh.resources.get(chain[i].id); ok && recorded.generation != nil && recorded.generation.id > fresh.generation.id {
				return nil, "", upstreamUnavailable()
			}
		}
		if !restart {
			return fresh, remote, nil
		}
	}
}

var errPlaylistRefused = errors.New("playlist refused")

type playlistIndex struct {
	semantic map[string]string
	identity map[string]string
}

func addCandidate(index map[string]string, key, candidate string) {
	if key == "" {
		return
	}
	if previous, ok := index[key]; ok && previous != candidate {
		index[key] = ""
	} else if !ok {
		index[key] = candidate
	}
}

func (p *Proxy) findResource(ctx context.Context, key Key, found *entry, playlist string, original resource) (string, error) {
	found.playlistMu.Lock()
	defer found.playlistMu.Unlock()
	if index := found.playlists[playlist]; index != nil {
		return index.match(original)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, playlist, nil)
	if err != nil {
		return "", upstreamUnavailable()
	}
	plat, _ := p.platforms.Get(key.Platform)
	trusted := func(host string) bool { return plat != nil && plat.TrustedMediaHost(host) }
	for name, value := range requestHeaders(trusted, found.manifest, request.URL) {
		request.Header.Set(name, value)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := p.client.Do(request)
	if err != nil {
		return "", upstreamUnavailable()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusGone {
			return "", errPlaylistRefused
		}
		return "", upstreamUnavailable()
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxPlaylist+1))
	if err != nil || len(data) > maxPlaylist || !bytes.HasPrefix(bytes.TrimSpace(data), []byte("#EXTM3U")) {
		return "", upstreamUnavailable()
	}
	lines := normalizedPlaylistLines(data, response.Request.URL)
	var index *playlistIndex
	if len(found.playlists) < 4 {
		for _, line := range lines {
			if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
				index = &playlistIndex{semantic: make(map[string]string), identity: make(map[string]string)}
				break
			}
		}
	}
	count := 0
	match, identity := "", resourceIdentity(original.url)
	ambiguous := false
	consider := func(raw, semantic string) {
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		u = response.Request.URL.ResolveReference(u)
		if u.Scheme != "http" && u.Scheme != "https" {
			return
		}
		candidate := u.String()
		candidateIdentity := ""
		if index != nil || original.semantic == "" {
			candidateIdentity = resourceIdentity(candidate)
		}
		if (original.semantic != "" && semantic == original.semantic) ||
			(original.semantic == "" && identity != "" && candidateIdentity == identity) {
			if match != "" && match != candidate {
				ambiguous = true
			}
			match = candidate
		}
		if index != nil {
			count++
			if count > p.maxResources {
				index = nil
			} else {
				addCandidate(index.semantic, semantic, candidate)
				addCandidate(index.identity, candidateIdentity, candidate)
			}
		}
	}
	var semantics playlistSemantics
	for _, line := range lines {
		line = strings.TrimSpace(line)
		semantic := semanticLine(&semantics, line, response.Request.URL)
		if strings.HasPrefix(line, "#") {
			for _, attr := range playlistURI.FindAllStringSubmatch(line, -1) {
				consider(attr[1], "")
			}
		} else if line != "" {
			consider(line, semantic)
		}
	}
	if index != nil {
		if found.playlists == nil {
			found.playlists = make(map[string]*playlistIndex)
		}
		found.playlists[playlist] = index
	}
	if match == "" || ambiguous {
		return "", upstreamUnavailable()
	}
	return match, nil
}

func (index *playlistIndex) match(original resource) (string, error) {
	match := ""
	if original.semantic != "" {
		match = index.semantic[original.semantic]
	} else {
		match = index.identity[resourceIdentity(original.url)]
	}
	if match == "" {
		return "", upstreamUnavailable()
	}
	return match, nil
}

func (p *Proxy) rewrite(key Key, found *entry, base *url.URL, data []byte, source string) (string, error) {
	return p.rewriteAt(key, found, base, data, source, "")
}

func (p *Proxy) rewriteAt(key Key, found *entry, base *url.URL, data []byte, source, parent string) (string, error) {
	var result strings.Builder
	result.Grow(len(data))
	var resourceErr error
	address := func(raw, semantic string, retained bool) string {
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
		var id string
		if retained {
			digest := sha256.Sum256([]byte(parent + "\x00" + remote))
			id = hex.EncodeToString(digest[:16])
			found.resources.rememberChild(id, remote, parent, semantic, found.generation, true)
		} else {
			id, err = found.resources.tokens.seal(resourceToken{
				URL: remote, Parent: parent, Semantic: semantic, Generation: found.generation.id,
			})
			if errors.Is(err, errResourceTokenTooLarge) {
				digest := sha256.Sum256([]byte(parent + "\x00" + semantic + "\x00" + remote))
				id = hex.EncodeToString(digest[:16])
				found.resources.rememberChild(id, remote, parent, semantic, found.generation, false)
			} else if err != nil {
				resourceErr = err
				return ""
			}
		}
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
	var semantics playlistSemantics
	for _, line := range normalizedPlaylistLines(data, base) {
		line = strings.TrimSuffix(line, "\r")
		semantic := semanticLine(&semantics, strings.TrimSpace(line), base)
		if strings.HasPrefix(line, "#") {
			tag, _, _ := strings.Cut(line, ":")
			retained := false
			switch tag {
			case "#EXT-X-MEDIA", "#EXT-X-I-FRAME-STREAM-INF", "#EXT-X-KEY", "#EXT-X-SESSION-KEY", "#EXT-X-MAP":
				retained = true
			}
			line = playlistURI.ReplaceAllStringFunc(line, func(attribute string) string {
				return `URI="` + address(attribute[5:len(attribute)-1], "", retained) + `"`
			})
		} else if strings.TrimSpace(line) != "" {
			line = address(strings.TrimSpace(line), semantic, strings.HasPrefix(semantic, "variant:"))
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	if resourceErr != nil {
		return "", resourceErr
	}
	return result.String(), nil
}
