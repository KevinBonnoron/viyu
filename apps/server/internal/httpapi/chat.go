package httpapi

import (
	"net/http"
	"strconv"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
)

// chatLive answers GET /api/chat/live/{login}: a Server-Sent Events stream of the channel's chat. The route
// names no platform: the first platform with a chat that accepts the login serves it.
func (s *Server) chatLive(e *core.RequestEvent) error {
	noStore(e)
	login := e.Request.PathValue("login")
	for _, p := range s.Platforms.WithChat() {
		source := p.Chat()
		if !source.ValidChannel(login) {
			continue
		}
		if err := s.Sources.RequireEnabled(e.Auth.Id, p.ID()); err != nil {
			return err
		}
		ctx, done := s.requestContext(e)
		defer done()
		return s.Chat.ServeLive(e.Response, e.Request.WithContext(ctx), e.Auth.Id, p.ID(), source, login)
	}
	return apierr.Invalid("Provide a valid channel login.")
}

// chatVOD answers GET /api/chat/vod/{id}?offset=<seconds> or ?cursor=<opaque>: the recorded chat of a stored
// video, addressed by its record id. The video must belong to a platform that has a chat.
func (s *Server) chatVOD(e *core.RequestEvent) error {
	noStore(e)
	record, err := s.Store.Video(e.Request.PathValue("id"))
	if err != nil {
		return lookupFailure(err, "Video")
	}
	if !catalog.VisibleTo(record, viewerID(e)) {
		return apierr.NotFound("Video")
	}
	platformID := record.GetString("platform")
	p, ok := s.Platforms.Get(platformID)
	if !ok || p.Chat() == nil {
		return apierr.NotFound("Video")
	}
	if err = s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return err
	}
	query := e.Request.URL.Query()
	cursor := query.Get("cursor")
	if query.Has("cursor") && cursor == "" {
		return apierr.Invalid("Provide a valid cursor.")
	}
	offset := 0
	if query.Has("offset") {
		if offset, err = strconv.Atoi(query.Get("offset")); err != nil {
			return apierr.Invalid("Provide a valid offset in seconds.")
		}
	}
	ctx, done := s.requestContext(e)
	defer done()
	page, err := s.Chat.Replay(ctx, platformID, p.Chat(), record.GetString("platform_id"), offset, cursor)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, page)
}
