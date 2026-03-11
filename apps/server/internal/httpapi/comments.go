package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/comments"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// videoComments answers GET /api/video/{videoId}/comments: the comment list of a video, public unless the
// request names a connected source (the list is then bound to the account and its session).
func (s *Server) videoComments(e *core.RequestEvent) error {
	noStore(e)
	conn, err := s.connectionOf(e)
	if err != nil {
		return err
	}
	record, err := s.Store.Video(e.Request.PathValue("videoId"))
	if err != nil {
		return lookupFailure(err, "Video")
	}
	if !catalog.VisibleTo(record, viewerID(e)) {
		return apierr.NotFound("Video")
	}
	platformID, nativeID := record.GetString("platform"), record.GetString("platform_id")
	if conn.Connected() && platformID != conn.Source {
		return apierr.RequestFailed()
	}
	p, ok := s.Platforms.Get(platformID)
	if !ok {
		return apierr.NotFound("Video")
	}
	if p.CommentsMode() != platform.CommentsList {
		return apierr.InvalidInput("This platform has no comment list.")
	}
	if !p.ValidVideoID(nativeID) {
		return apierr.InvalidInput("Provide a valid video ID.")
	}
	if err = s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return err
	}
	key := comments.Key{Platform: platformID, VideoID: nativeID}
	if conn.Connected() {
		key.UserID, key.Revision = e.Auth.Id, conn.Revision
	}
	ctx, done := s.requestContext(e)
	defer done()
	list, err := s.Comments.List(ctx, key)
	if err != nil {
		return err
	}
	if list == nil {
		list = []platform.Comment{}
	}
	return s.respond(e, http.StatusOK, map[string]any{"comments": list})
}
