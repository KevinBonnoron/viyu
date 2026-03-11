package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// resolveBodyMaxBytes bounds the body of POST /api/resolve: one address.
const resolveBodyMaxBytes = 8 << 10

// resolved is the answer of POST /api/resolve: exactly one of a video and a channel.
type resolved struct {
	Video   *platform.Video   `json:"video,omitempty"`
	Channel *platform.Channel `json:"channel,omitempty"`
}

// resolve answers POST /api/resolve: it turns a pasted external link into a stored video or channel,
// addressed by record id (clients never see the platform-native id).
//
// A video asks the platform nothing, so the client can open the player at once: a video not stored yet
// gets a stub record, which the player's GET /api/video/{id} completes (that route reads the platform for
// any record that is not verified and fresh), and a verified, fresh record answers with its details. A
// channel answers its profile, which the client shows as the one result of the link: a known channel from
// the store, otherwise from the platform's listing, which also fills the cache its page then reads.
func (s *Server) resolve(e *core.RequestEvent) error {
	var input struct {
		URL string `json:"url"`
	}
	if err := readBody(e, &input, resolveBodyMaxBytes); err != nil {
		return err
	}
	if strings.TrimSpace(input.URL) == "" {
		return apierr.InvalidInput("Provide a video or channel URL.")
	}
	p, target, ok := s.Platforms.ResolveURL(input.URL)
	if !ok {
		return apierr.InvalidInput("Unsupported or invalid video or channel URL.")
	}
	if err := s.Sources.RequireEnabled(e.Auth.Id, p.ID()); err != nil {
		return err
	}
	if target.Channel {
		return s.resolveChannel(e, p, target.ID)
	}
	record, err := s.Store.EnsureVideo(e.Auth.Id, p.ID(), target.ID)
	if err != nil {
		return apierr.Internal("Failed to resolve the video", err)
	}
	if record == nil {
		// Only another account's private record exists: storing the public metadata makes it public.
		return s.resolveFromPlatform(e, p, target.ID)
	}
	video, cached := s.Store.CachedAnswer(record, false)
	if !cached {
		video = platform.Video{ID: target.ID, RecordID: record.Id, Platform: p.ID()}
	}
	video.Comments = p.CommentsMode()
	return s.respond(e, http.StatusOK, resolved{Video: &video})
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
	return s.respond(e, http.StatusOK, resolved{Video: &video})
}

// resolveChannel answers the profile of the channel a link names. A stored channel with a name answers by
// itself (its page refreshes what is stale); otherwise the platform lists it, under whatever canonical id it
// reports (a YouTube handle becomes its channel id), and the listing is stored.
func (s *Server) resolveChannel(e *core.RequestEvent, p platform.Platform, nativeID string) error {
	record, err := s.Store.FindChannel(p.ID(), nativeID)
	switch {
	case err == nil && catalog.VisibleTo(record, viewerID(e)):
		if info := catalog.ChannelOf(record); info != nil {
			return s.respond(e, http.StatusOK, resolved{Channel: info})
		}
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return apierr.Internal("Failed to resolve the channel", err)
	}
	ctx, done := s.requestContext(e)
	defer done()
	info, _, err := s.Refresher.Channel(ctx, p.ID(), nativeID)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, resolved{Channel: &info})
}
