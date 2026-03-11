package httpapi

import (
	"net/http"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// resolveBodyMaxBytes bounds the body of POST /api/resolve: one address.
const resolveBodyMaxBytes = 8 << 10

// resolve answers POST /api/resolve: it turns a pasted external link into a stored video, addressed by record
// id (clients never see the platform-native id). It asks the platform nothing, so the client can open the
// player at once: a video not stored yet gets a stub record, which the player's GET /api/video/{id} completes
// (that route reads the platform for any record that is not verified and fresh), and a verified, fresh record
// answers with its details.
func (s *Server) resolve(e *core.RequestEvent) error {
	var input struct {
		URL string `json:"url"`
	}
	if err := readBody(e, &input, resolveBodyMaxBytes); err != nil {
		return err
	}
	if strings.TrimSpace(input.URL) == "" {
		return apierr.InvalidInput("Provide a video URL.")
	}
	p, nativeID, ok := s.Platforms.ResolveURL(input.URL)
	if !ok {
		return apierr.InvalidInput("Unsupported or invalid video URL.")
	}
	if err := s.Sources.RequireEnabled(e.Auth.Id, p.ID()); err != nil {
		return err
	}
	record, err := s.Store.EnsureVideo(e.Auth.Id, p.ID(), nativeID)
	if err != nil {
		return apierr.Internal("Failed to resolve the video", err)
	}
	if record == nil {
		// Only another account's private record exists: storing the public metadata makes it public.
		return s.resolveFromPlatform(e, p, nativeID)
	}
	video, cached := s.Store.CachedAnswer(record, false)
	if !cached {
		video = platform.Video{ID: nativeID, RecordID: record.Id, Platform: p.ID()}
	}
	video.Comments = p.CommentsMode()
	return s.respond(e, http.StatusOK, video)
}

// resolveFromPlatform reads the video's public metadata, stores it and answers it.
func (s *Server) resolveFromPlatform(e *core.RequestEvent, p platform.Platform, nativeID string) error {
	ctx, done := s.requestContext(e)
	defer done()
	video, err := p.VideoMetadata(ctx, nativeID, "")
	if err != nil {
		return err
	}
	if err := s.Store.StoreVideo(&video, ""); err != nil {
		return apierr.Internal("Failed to resolve the video", err)
	}
	video.Comments = p.CommentsMode()
	return s.respond(e, http.StatusOK, video)
}
