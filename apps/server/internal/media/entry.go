package media

import (
	"container/list"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// manifestLife is how long a manifest nobody asks for stays in memory; every request renews it.
const manifestLife = 30 * time.Minute

var nextGeneration atomic.Uint64
var staleGeneration generation

type entry struct {
	manifest platform.Manifest
	// validUntil is when the upstream stops honouring the signed manifest (zero: no signature found).
	validUntil time.Time
	resources  *resourceSet
	generation *generation
	playlistMu sync.Mutex
	playlists  map[string]*playlistIndex
}

func newEntry(manifest platform.Manifest, maxResources int) *entry {
	return &entry{manifest: manifest, validUntil: manifestExpiry(manifest.URL), resources: newResourceSet(maxResources), generation: &generation{id: nextGeneration.Add(1)}}
}

func (e *entry) expired(now time.Time) bool {
	return !e.validUntil.IsZero() && !now.Before(e.validUntil)
}

func (e *entry) resource(id string) (resource, bool) {
	if item, ok := e.resources.get(id); ok {
		return item, true
	}
	token, ok := e.resources.tokens.open(id)
	if !ok {
		return resource{}, false
	}
	issued := &staleGeneration
	if token.Generation == e.generation.id {
		issued = e.generation
	}
	item := resource{id: id, url: token.URL, parent: token.Parent, semantic: token.Semantic, generation: issued}
	return e.resources.rememberIfAbsent(item), true
}

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

// Protected routes precede boundary; segments follow it and are evicted first.
type resourceSet struct {
	max int

	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List
	boundary *list.Element
	tokens   resourceTokens
}

type generation struct{ id uint64 }

type resource struct {
	id, url, parent, semantic string
	generation                *generation
	retained                  bool
	// A shared URL can represent several byte ranges or renditions; it has no unique semantic identity.
	ambiguousSemantics bool
}

func newResourceSet(max int) *resourceSet {
	order := list.New()
	return &resourceSet{max: max, items: make(map[string]*list.Element), order: order, boundary: order.PushBack(nil)}
}

func (s *resourceSet) remember(id, remote string) { s.rememberChild(id, remote, "", "", nil, false) }
func (s *resourceSet) rememberIfAbsent(item resource) resource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if element := s.items[item.id]; element != nil {
		s.touch(element)
		return *element.Value.(*resource)
	}
	s.rememberLocked(item.id, item.url, item.parent, item.semantic, item.generation, item.retained)
	return item
}

func (s *resourceSet) rememberChild(id, remote, parent, semantic string, generation *generation, retained bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberLocked(id, remote, parent, semantic, generation, retained)
}

func (s *resourceSet) rememberLocked(id, remote, parent, semantic string, generation *generation, retained bool) {
	if element := s.items[id]; element != nil {
		item := element.Value.(*resource)
		if item.generation != nil && generation != nil && item.generation.id > generation.id {
			s.touch(element)
			return
		}
		item.url = remote
		item.parent = parent
		if item.semantic != semantic {
			item.ambiguousSemantics = true
		}
		if item.ambiguousSemantics {
			item.semantic = ""
		} else {
			item.semantic = semantic
		}
		item.generation = generation
		item.retained = item.retained || retained
		s.touch(element)
		return
	}
	if len(s.items) >= s.max {
		oldest := s.order.Back()
		if oldest == s.boundary {
			oldest = s.boundary.Prev()
		}
		if oldest != nil {
			delete(s.items, oldest.Value.(*resource).id)
			s.order.Remove(oldest)
		}
	}
	item := &resource{id: id, url: remote, parent: parent, semantic: semantic, generation: generation, retained: retained}
	if retained {
		s.items[id] = s.order.PushFront(item)
	} else {
		s.items[id] = s.order.InsertAfter(item, s.boundary)
	}
}

func (s *resourceSet) touch(element *list.Element) {
	if element.Value.(*resource).retained {
		s.order.MoveToFront(element)
	} else {
		s.order.MoveAfter(element, s.boundary)
	}
}

func (s *resourceSet) lookup(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	element := s.items[id]
	if element == nil {
		return ""
	}
	s.touch(element)
	return element.Value.(*resource).url
}

func (s *resourceSet) get(id string) (resource, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	element := s.items[id]
	if element == nil {
		return resource{}, false
	}
	s.touch(element)
	return *element.Value.(*resource), true
}

func (s *resourceSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
