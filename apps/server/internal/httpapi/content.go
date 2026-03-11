package httpapi

import (
	"database/sql"
	"errors"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// registerContent lists the routes that browse and play what the platforms offer: search, a pasted link, a
// video, a channel, the conversation around them, live status, and the stream itself.
func (s *Server) registerContent(api Routes) {
	api.GET("/api/search", s.search)
	api.POST("/api/resolve", s.resolve)
	api.GET("/api/video/{videoId}", s.video)
	api.GET("/api/video/{videoId}/url", s.videoURL)
	api.GET("/api/video/{videoId}/comments", s.videoComments)
	api.GET("/api/channel/{id}", s.channel)
	api.GET("/api/channel/{id}/live", s.channelLive)
	api.GET("/api/subscriptions/live", s.subscriptionsLive)
	api.GET("/api/chat/live/{login}", s.chatLive)
	api.GET("/api/chat/vod/{id}", s.chatVOD)
	api.GET("/api/stream/{videoId}", s.stream)
	api.GET("/api/stream/{videoId}/resource/{resourceId}", s.streamResource)
}

// lookupFailure is the failure of reading a record by id: a record that does not exist is "<what> not found",
// anything else (the database) is an unexpected failure.
func lookupFailure(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apierr.NotFound(what)
	}
	return apierr.Internal("Failed to read the record", err)
}
