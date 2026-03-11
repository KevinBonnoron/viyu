package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// profileFeed answers GET /api/feed?profile=: the ranked videos of the profile's subscribed channels.
func (s *Server) profileFeed(e *core.RequestEvent) error {
	profile, err := s.profileParam(e)
	if err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	videos, err := s.Feed.Feed(ctx, e.Auth.Id, profile.Id)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, videos)
}

// recommendations answers GET /api/recommendations?profile=&video=: what to watch after a video, the video
// being named by its record id.
func (s *Server) recommendations(e *core.RequestEvent) error {
	profile, err := s.profileParam(e)
	if err != nil {
		return err
	}
	video := e.Request.URL.Query().Get("video")
	if video == "" {
		return apierr.Invalid(`Missing query parameter "video"`)
	}
	ctx, done := s.requestContext(e)
	defer done()
	videos, err := s.Feed.Recommendations(ctx, e.Auth.Id, profile.Id, video)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, videos)
}
