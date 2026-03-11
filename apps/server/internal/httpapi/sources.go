package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

const (
	// sourceBodyLimit bounds the small JSON bodies of the source routes.
	sourceBodyLimit = 4 << 10
	// connectBodyLimit bounds the cookie export a connection is imported from.
	connectBodyLimit = 300 << 10
)

// private marks the answer of a handler as personal: neither the feeds nor the connection status of an
// account may be kept by a browser or a shared HTTP cache.
func private(h handler) handler {
	return func(e *core.RequestEvent) error {
		noStore(e)
		return h(e)
	}
}

// listSources answers GET /api/sources: every platform as the account sees it.
func (s *Server) listSources(e *core.RequestEvent) error {
	sources, err := s.Sources.List(e.Auth.Id)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, sources)
}

// setSourceActivation answers PUT /api/sources/{platform}/activation.
func (s *Server) setSourceActivation(e *core.RequestEvent) error {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readBody(e, &body, sourceBodyLimit); err != nil {
		return err
	}
	if body.Enabled == nil {
		return apierr.InvalidInput("Provide whether the source is enabled.")
	}
	platformID := e.Request.PathValue("platform")
	if err := s.Sources.SetEnabled(e.Auth.Id, platformID, *body.Enabled); err != nil {
		return err
	}
	source, err := s.Sources.Current(e.Auth.Id, platformID)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, source)
}

// connectSource answers POST /api/sources/{platform}/connect: it imports the account's cookies.
func (s *Server) connectSource(e *core.RequestEvent) error {
	var body struct {
		Cookies string `json:"cookies"`
		Label   string `json:"label"`
	}
	if err := readBody(e, &body, connectBodyLimit); err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	source, err := s.Sources.Connect(ctx, e.Auth.Id, e.Request.PathValue("platform"), body.Cookies, body.Label)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, source)
}

// checkSource answers POST /api/sources/{platform}/check: does the stored session still work.
func (s *Server) checkSource(e *core.RequestEvent) error {
	ctx, done := s.requestContext(e)
	defer done()
	source, err := s.Sources.Check(ctx, e.Auth.Id, e.Request.PathValue("platform"))
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, source)
}

// disconnectSource answers DELETE /api/sources/{platform}. The comments fetched with the connection go
// with it.
func (s *Server) disconnectSource(e *core.RequestEvent) error {
	platformID := e.Request.PathValue("platform")
	if err := s.Sources.Disconnect(e.Auth.Id, platformID); err != nil {
		return err
	}
	s.Comments.Invalidate(e.Auth.Id, platformID)
	return noContent(e)
}

// sourceFeed answers GET /api/sources/{platform}/feed: a page of the account's personal feed. The videos are
// stored first, so each one is shown with its record id.
func (s *Server) sourceFeed(e *core.RequestEvent) error {
	query := e.Request.URL.Query()
	page, limit := 1, 30
	var err error
	if raw := query.Get("page"); raw != "" {
		if page, err = strconv.Atoi(raw); err != nil {
			return apierr.InvalidInput("Use a positive page number.")
		}
	}
	if raw := query.Get("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil {
			return apierr.InvalidInput("Use a limit between 1 and 50.")
		}
	}
	ctx, done := s.requestContext(e)
	defer done()
	feed, err := s.Sources.Feed(ctx, e.Auth.Id, e.Request.PathValue("platform"), query.Get("kind"), page, limit)
	if err != nil {
		return err
	}
	if err := s.Store.StoreVideos(feed.Videos, e.Auth.Id); err != nil {
		return apierr.Internal("Failed to read the feed", err)
	}
	return s.respond(e, http.StatusOK, feed)
}

// watchSource answers POST /api/sources/{platform}/watch: it marks a video watched on the platform. The
// video is named by its record id and must be a video of that platform.
func (s *Server) watchSource(e *core.RequestEvent) error {
	var body struct {
		VideoID         string  `json:"videoId"`
		PlayedSeconds   float64 `json:"playedSeconds"`
		DurationSeconds float64 `json:"durationSeconds"`
	}
	if err := readBody(e, &body, sourceBodyLimit); err != nil {
		return err
	}
	platformID, nativeID, err := s.Store.VideoIdentity(body.VideoID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return apierr.InvalidInput("Provide a valid video.")
	case err != nil:
		return apierr.Internal("Failed to mark the video watched", err)
	case platformID != e.Request.PathValue("platform"):
		return apierr.InvalidInput("Provide a valid video.")
	}
	ctx, done := s.requestContext(e)
	defer done()
	if err := s.Sources.Watch(ctx, e.Auth.Id, platformID, nativeID, body.PlayedSeconds, body.DurationSeconds); err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, map[string]string{"status": "marked"})
}
