package httpapi

import (
	"net/http"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// connectedListingSize is how many videos the listing of a connected channel asks for.
const connectedListingSize = 30

// channelPage is the answer of GET /api/channel/{id}.
type channelPage struct {
	Channel *platform.Channel `json:"channel"`
	Videos  []platform.Video  `json:"videos"`
}

// channel answers GET /api/channel/{id}: the record id of a stored channel, never a native handle, with its
// latest videos. A connected request lists the channel as the account sees it and keeps the result private to
// it; a public one is answered from the cache while it is fresh, and refreshed otherwise.
func (s *Server) channel(e *core.RequestEvent) error {
	record, err := s.Store.Channel(e.Request.PathValue("id"))
	if err != nil {
		return lookupFailure(err, "Channel")
	}
	if !catalog.VisibleTo(record, viewerID(e)) {
		return apierr.NotFound("Channel")
	}
	platformID, nativeID := record.GetString("platform"), record.GetString("platform_id")
	p, ok := s.Platforms.Get(platformID)
	if !ok {
		return apierr.NotFound("Channel")
	}
	if e.Request.URL.Query().Get("sync") == "1" {
		return s.channelSync(e, record, p)
	}
	conn, session, cleanup, err := s.session(e)
	if err != nil {
		return err
	}
	defer cleanup()
	if conn.Connected() && conn.Source != platformID {
		return apierr.InvalidInput("The selected source and platform must match.")
	}
	if err = s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	if conn.Connected() {
		info, videos, err := p.ChannelListing(ctx, nativeID, connectedListingSize, session.CookieFile)
		if err != nil {
			return err
		}
		p.EnrichListing(ctx, info.ID, videos)
		for i := range videos {
			videos[i].Source = conn.Source
		}
		if err := s.Store.StoreVideos(videos, e.Auth.Id); err != nil {
			logging.Of(s.App).Warn("Failed to cache connected channel videos", "id", nativeID, "platform", platformID, "error", err)
		}
		if err := s.Store.StoreChannelListing(&info, e.Auth.Id); err != nil {
			logging.Of(s.App).Warn("Failed to cache connected channel info", "id", nativeID, "platform", platformID, "error", err)
		}
		return s.respond(e, http.StatusOK, channelPage{Channel: &info, Videos: storedVideos(videos)})
	}
	if record.GetDateTime("expires_at").Time().After(time.Now()) {
		if info := catalog.ChannelOf(record); info != nil {
			if videos, err := s.Store.CachedChannelVideos(record.Id, viewerID(e)); err == nil && len(videos) > 0 {
				return s.respond(e, http.StatusOK, channelPage{Channel: info, Videos: videos})
			}
		}
	}
	info, videos, err := s.Refresher.Channel(ctx, platformID, nativeID)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, channelPage{Channel: &info, Videos: storedVideos(videos)})
}

// storedVideos keeps the videos that have a record id (the others could not be stored and must not be shown; a
// native id never leaves the server), as a list that serializes as [] when empty.
func storedVideos(videos []platform.Video) []platform.Video {
	kept := make([]platform.Video, 0, len(videos))
	for _, v := range videos {
		if v.RecordID != "" {
			kept = append(kept, v)
		}
	}
	return kept
}

// channelLive answers GET /api/channel/{id}/live. The status is public information, so it never uses a
// connected source, and a lookup that fails is reported as "not live": the answer only decorates the channel
// page, it must never turn into an error there.
func (s *Server) channelLive(e *core.RequestEvent) error {
	record, err := s.Store.Channel(e.Request.PathValue("id"))
	if err != nil {
		return lookupFailure(err, "Channel")
	}
	if !catalog.VisibleTo(record, viewerID(e)) {
		return apierr.NotFound("Channel")
	}
	platformID, nativeID := record.GetString("platform"), record.GetString("platform_id")
	p, ok := s.Platforms.Get(platformID)
	if !ok {
		return apierr.NotFound("Channel")
	}
	if err = s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return err
	}
	if !p.ValidLiveChannel(nativeID) {
		return apierr.InvalidInput("Provide a valid channel.")
	}
	ctx, done := s.requestContext(e)
	defer done()
	stream := s.Live.Lookup(ctx, platformID, nativeID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.respond(e, http.StatusOK, stream)
}
