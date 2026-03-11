package httpapi

import (
	"net/http"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// resolveBodyMaxBytes bounds the body of POST /api/resolve: one address.
const resolveBodyMaxBytes = 8 << 10

// resolve answers POST /api/resolve: it turns a pasted external link into a stored video. It reads the
// video's metadata, stores it, and answers it addressed by record id: clients never see the platform-native id.
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
