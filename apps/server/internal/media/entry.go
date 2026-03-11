package media

import (
	"container/list"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// manifestLife is how long a manifest nobody asks for stays in memory; every request renews it.
const manifestLife = 30 * time.Minute

// entry is one authorized manifest and the signed resources its playlists pointed at.
type entry struct {
	manifest platform.Manifest
	// validUntil is when the upstream stops honouring the signed manifest (zero: no signature found).
	// Activity never keeps an entry alive past it.
	validUntil time.Time
	resources  *resourceSet
}

func newEntry(manifest platform.Manifest, maxResources int) *entry {
	return &entry{manifest: manifest, validUntil: manifestExpiry(manifest.URL), resources: newResourceSet(maxResources)}
}

// lifetime is how long the entry lives from now: the standard keep-alive, cut short by the signature.
func (e *entry) lifetime(now time.Time) time.Duration {
	life := manifestLife
	if !e.validUntil.IsZero() {
		if left := e.validUntil.Sub(now); left < life {
			life = left
		}
	}
	return life
}

// manifestExpiry reads the signature deadline of a manifest address (`expire` query parameter or path
// segment) and takes a safety margin off it.
func manifestExpiry(raw string) time.Time {
	parsed, err := url.Parse(raw)
	if err != nil {
		return time.Time{}
	}
	expire := parsed.Query().Get("expire")
	previous := ""
	for part := range strings.SplitSeq(parsed.Path, "/") {
		if previous == "expire" {
			expire = part
			break
		}
		previous = part
	}
	seconds, err := strconv.ParseInt(expire, 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).Add(-30 * time.Second)
}

// resourceSet remembers the segment/variant addresses of one manifest, least recently used first out: a
// hostile or enormous playlist cannot grow memory. It maps a resource id to its element in order, which
// holds the most recently used address first.
type resourceSet struct {
	max int

	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List
}

type resource struct{ id, url string }

func newResourceSet(max int) *resourceSet {
	return &resourceSet{max: max, items: make(map[string]*list.Element), order: list.New()}
}

// remember records an address, evicting the least recently used one when the set is full.
func (s *resourceSet) remember(id, remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if element := s.items[id]; element != nil {
		element.Value.(*resource).url = remote
		s.order.MoveToFront(element)
		return
	}
	if s.order.Len() >= s.max {
		if oldest := s.order.Back(); oldest != nil {
			delete(s.items, oldest.Value.(*resource).id)
			s.order.Remove(oldest)
		}
	}
	s.items[id] = s.order.PushFront(&resource{id: id, url: remote})
}

// lookup returns a remembered address ("" when unknown) and marks it recently used.
func (s *resourceSet) lookup(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	element := s.items[id]
	if element == nil {
		return ""
	}
	s.order.MoveToFront(element)
	return element.Value.(*resource).url
}

func (s *resourceSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}
