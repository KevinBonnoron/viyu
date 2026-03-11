package httpapi

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// hardenStream applies to everything under /api/stream, errors included: the bytes come from third parties,
// so the browser must never interpret them as a document, whatever the upstream claims. The proxy repeats
// it for what it writes.
func hardenStream(e *core.RequestEvent) {
	header := e.Response.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox; default-src 'none'")
}

// stream answers GET /api/stream/{videoId}: the master playlist of a video, rewritten to come back through
// the server. The account's normal bearer token authorizes it (see streamKey).
func (s *Server) stream(e *core.RequestEvent) error {
	hardenStream(e)
	key, source, err := s.streamKey(e)
	if err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	return s.Media.ServeManifest(e.Response, e.Request.WithContext(ctx), key, source)
}

// streamResource answers GET /api/stream/{videoId}/resource/{resourceId}: a variant playlist, segment or
// other resource a rewritten playlist pointed at.
func (s *Server) streamResource(e *core.RequestEvent) error {
	hardenStream(e)
	key, source, err := s.streamKey(e)
	if err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	return s.Media.ServeResource(e.Response, e.Request.WithContext(ctx), key, e.Request.PathValue("resourceId"), source)
}
