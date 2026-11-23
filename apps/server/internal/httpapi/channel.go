package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// connectedListingSize is how many videos the listing of a connected channel asks for.
const connectedListingSize = 30

// channelPage is the answer of GET /api/channel/{id}.
type channelPage struct {
	Channel *platform.Channel `json:"channel"`
	Videos  []platform.Video  `json:"videos"`
	Page    int               `json:"page"`
	Limit   int               `json:"limit"`
	HasMore bool              `json:"hasMore"`
	Total   int               `json:"total"`
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
	page, limit, query, ascending, err := channelPageParams(e)
	if err != nil {
		return err
	}
	if e.Request.URL.Query().Get("sync") == "1" {
		return s.channelSync(e, record, p, page, limit, query, ascending)
	}
	connInfo, err := s.connectionOf(e)
	if err != nil {
		return err
	}
	if connInfo.Connected() && p.CatalogSync() != nil {
		return s.channelSync(e, record, p, page, limit, query, ascending)
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
		return s.respond(e, http.StatusOK, makeChannelPage(&info, storedVideos(videos), page, limit, query, ascending))
	}
	var cached *channelPage
	if record.GetDateTime("expires_at").Time().After(time.Now()) {
		if info := catalog.ChannelOf(record); info != nil {
			if strings.TrimSpace(query) == "" {
				if videos, total, err := s.Store.CachedChannelVideoPage(record.Id, viewerID(e), page, limit, ascending); err == nil && total > 0 {
					cached = &channelPage{Channel: info, Videos: videos, Page: page, Limit: limit, Total: total, HasMore: page*limit < total}
				}
			} else if videos, err := s.Store.CachedChannelVideos(record.Id, viewerID(e)); err == nil && len(videos) > 0 {
				result := makeChannelPage(info, videos, page, limit, query, ascending)
				cached = &result
			}
		}
	}
	// A usable cached page is the answer once the channel's profile was fetched; before that (a channel
	// stored without its metadata) the profile is fetched first, but a failing lookup must not turn a page
	// that was good into an error: the stored page answers and the profile backfill tries again.
	if cached != nil && catalog.ProfileSynced(record) {
		return s.respond(e, http.StatusOK, *cached)
	}
	info, videos, err := s.Refresher.Channel(ctx, platformID, nativeID)
	if err != nil {
		if cached != nil {
			logging.Of(s.App).Warn("Failed to fetch channel profile, serving the cached page", "id", nativeID, "platform", platformID, "error", err)
			return s.respond(e, http.StatusOK, *cached)
		}
		return err
	}
	return s.respond(e, http.StatusOK, makeChannelPage(&info, storedVideos(videos), page, limit, query, ascending))
}

func channelPageParams(e *core.RequestEvent) (int, int, string, bool, error) {
	values := e.Request.URL.Query()
	page, limit := 1, 30
	if value := values.Get("page"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > math.MaxInt/50 {
			return 0, 0, "", false, apierr.InvalidInput("Page must be a positive integer.")
		}
		page = n
	}
	if value := values.Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 50 {
			return 0, 0, "", false, apierr.InvalidInput("Limit must be between 1 and 50.")
		}
		limit = n
	}
	order := values.Get("order")
	if order != "" && order != "asc" && order != "desc" {
		return 0, 0, "", false, apierr.InvalidInput("Order must be asc or desc.")
	}
	return page, limit, values.Get("q"), order == "asc", nil
}

func makeChannelPage(channel *platform.Channel, videos []platform.Video, page, limit int, query string, ascending bool) channelPage {
	selected, total := jobs.PageVideos(videos, page, limit, query, ascending)
	return channelPage{Channel: channel, Videos: selected, Page: page, Limit: limit, Total: total, HasMore: page*limit < total}
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
