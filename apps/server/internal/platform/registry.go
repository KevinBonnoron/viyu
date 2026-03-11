package platform

import (
	"net/url"
	"strings"
)

// Registry holds the platforms in display order. It is built once, injected where it is needed and never
// changes afterwards, so it is safe for concurrent use.
type Registry struct {
	list []Platform
	byID map[string]Platform
}

// NewRegistry lists platforms in display order; the first one is the default.
func NewRegistry(platforms ...Platform) *Registry {
	r := &Registry{list: append([]Platform(nil), platforms...), byID: make(map[string]Platform, len(platforms))}
	for _, p := range platforms {
		r.byID[p.ID()] = p
	}
	return r
}

// All lists every platform in display order. The caller must not modify the slice.
func (r *Registry) All() []Platform { return r.list }

// IDs lists the platform ids in display order.
func (r *Registry) IDs() []string {
	ids := make([]string, len(r.list))
	for i, p := range r.list {
		ids[i] = p.ID()
	}
	return ids
}

// Get returns the platform with the id.
func (r *Registry) Get(id string) (Platform, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// Default is the platform a request names when it names none: the first one registered.
func (r *Registry) Default() Platform { return r.list[0] }

// Or returns the platform with the id, or the default one when id is empty.
func (r *Registry) Or(id string) (Platform, bool) {
	if id == "" {
		return r.Default(), true
	}
	return r.Get(id)
}

// ClaimVideoID returns the first platform, in registry order, for which a bare video id can only be its own.
func (r *Registry) ClaimVideoID(id string) (Platform, bool) {
	for _, p := range r.list {
		if p.ClaimsVideoID(id) {
			return p, true
		}
	}
	return nil, false
}

// ResolveURL finds the platform and native video id a pasted external link names. Anything that is not a
// plain web address of a video is rejected.
func (r *Registry) ResolveURL(raw string) (Platform, string, bool) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "https://" + value
	}
	address, err := url.Parse(value)
	if err != nil || address.User != nil || address.Port() != "" {
		return nil, "", false
	}
	link := Link{URL: address, Host: strings.ToLower(address.Hostname())}
	for _, part := range strings.Split(strings.Trim(address.Path, "/"), "/") {
		if part != "" {
			link.Parts = append(link.Parts, part)
		}
	}
	for _, p := range r.list {
		if id, ok := p.ResolveURL(link); ok {
			return p, id, true
		}
	}
	return nil, "", false
}

// WithChat lists the platforms that offer a chat.
func (r *Registry) WithChat() []Platform {
	var chatting []Platform
	for _, p := range r.list {
		if p.Chat() != nil {
			chatting = append(chatting, p)
		}
	}
	return chatting
}

// WithCatalogSync lists the platforms whose channel catalog can be synchronized.
func (r *Registry) WithCatalogSync() []Platform {
	var syncable []Platform
	for _, p := range r.list {
		if p.CatalogSync() != nil {
			syncable = append(syncable, p)
		}
	}
	return syncable
}
