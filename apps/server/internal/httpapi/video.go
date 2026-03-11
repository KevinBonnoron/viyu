package httpapi

import (
	"fmt"
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
)

// video answers GET /api/video/{videoId}: the record id of a stored video, never a platform-native id. A
// verified, fresh public record answers by itself; otherwise the platform is asked and what it says is stored
// (publicly, or owned by the account when the request names a connected source).
func (s *Server) video(e *core.RequestEvent) error {
	id := e.Request.PathValue("videoId")
	conn, session, cleanup, err := s.session(e)
	if err != nil {
		return err
	}
	defer cleanup()
	record, err := s.Store.Video(id)
	if err != nil {
		if conn.Connected() {
			return apierr.RequestFailed().WithCause(err)
		}
		return lookupFailure(err, "Video")
	}
	if (conn.Connected() && record.GetString("platform") != conn.Source) || !catalog.VisibleTo(record, viewerID(e)) {
		if conn.Connected() {
			return apierr.RequestFailed()
		}
		return apierr.NotFound("Video")
	}
	platformID, nativeID := record.GetString("platform"), record.GetString("platform_id")
	p, ok := s.Platforms.Get(platformID)
	if !ok {
		return apierr.NotFound("Video")
	}
	if err = s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return err
	}
	if !conn.Connected() {
		if cached, ok := s.Store.CachedAnswer(record, e.Request.URL.Query().Get("chapters") == "1"); ok {
			cached.Comments = p.CommentsMode()
			return s.respond(e, http.StatusOK, cached)
		}
	}
	ctx, done := s.requestContext(e)
	defer done()
	v, err := p.VideoMetadata(ctx, nativeID, session.CookieFile)
	if err != nil {
		// The cause carries what the downloader said; the ids make the log line actionable.
		return apierr.From(err).WithCause(fmt.Errorf("video %s (%s) on %s: %w", id, nativeID, platformID, err))
	}
	v.Source = conn.Source
	if v.Manifest.URL != "" {
		if key, _, keyErr := s.streamKey(e); keyErr == nil {
			s.Media.Remember(key, v.Manifest)
		}
	}
	if !conn.Connected() {
		if err = s.Store.StoreVideo(&v, ""); err != nil {
			return apierr.Internal("Failed to get video metadata", err)
		}
	} else {
		// Store just enough to address the video by record id. An existing public record keeps its verified
		// data, and what only this session knows (chapters) stays out of the store.
		stored := v
		stored.Chapters = nil
		stored.FullMetadata = false
		if err = s.Store.StoreVideo(&stored, e.Auth.Id); err != nil {
			return apierr.Internal("Failed to get video metadata", err)
		}
		v.RecordID, v.ChannelRecordID = stored.RecordID, stored.ChannelRecordID
	}
	v.Comments = p.CommentsMode()
	return s.respond(e, http.StatusOK, v)
}

// videoURL answers GET /api/video/{videoId}/url with the page of the video on its own service. It reads the
// stored record only: the platform-native id never leaves the server, but the address built from it may.
func (s *Server) videoURL(e *core.RequestEvent) error {
	record, err := s.Store.Video(e.Request.PathValue("videoId"))
	if err != nil {
		return lookupFailure(err, "Video")
	}
	if !catalog.VisibleTo(record, viewerID(e)) {
		return apierr.NotFound("Video")
	}
	platformID := record.GetString("platform")
	if err = s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return err
	}
	page := ""
	if p, ok := s.Platforms.Get(platformID); ok {
		page = p.OriginalURL(record.GetString("platform_id"))
	}
	if page == "" {
		return apierr.New(http.StatusNotFound, apierr.CodeNotFound, "This video has no original page")
	}
	return s.respond(e, http.StatusOK, map[string]string{"url": page})
}
