package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// registerSeries lists the routes of the channel catalogs' series and of the recognition setting.
func (s *Server) registerSeries(api Routes) {
	api.GET("/api/series", s.listSeries)
	api.GET("/api/series/{id}", s.getSeries)
	api.GET("/api/admin/series-ocr", s.getSeriesOCR)
	api.PATCH("/api/admin/series-ocr", s.setSeriesOCR)
}

// seriesList is the answer of GET /api/series.
type seriesList struct {
	Series   []series.Summary `json:"series"`
	Sync     jobs.Sync        `json:"sync"`
	Analysis jobs.Overview    `json:"analysis"`
}

// listSeries answers GET /api/series?profile=: the stored series of the channels the profile subscribes to and
// where their analysis stands. It reads stored data only: the analysis itself runs in the background, and
// retries what failed on its own, so polling this route costs a few queries however many channels there are.
func (s *Server) listSeries(e *core.RequestEvent) error {
	noStore(e)
	profile, err := s.profileParam(e)
	if err != nil {
		return err
	}
	channels, err := s.Subscriptions.Subscribed(profile.Id)
	if err != nil {
		return err
	}
	channels = s.analysable(channels)
	overview, err := s.Analysis.Overview(channels)
	if err != nil {
		return apierr.Internal("Failed to load playlists", err)
	}
	ids := make([]string, len(channels))
	for i, channel := range channels {
		ids[i] = channel.Id
	}
	byChannel, err := s.Series.ForChannels(ids)
	if err != nil {
		return err
	}
	var all []series.Series
	for _, id := range ids {
		all = append(all, byChannel[id]...)
	}
	s.Analysis.ApplyEpisodes(all)
	series.Sort(all)
	summaries := make([]series.Summary, 0, len(all))
	for _, item := range all {
		summaries = append(summaries, addressed(item).Summary)
	}
	return s.respond(e, http.StatusOK, seriesList{Series: summaries, Sync: overview.Sync(), Analysis: overview})
}

// analysable keeps the channels whose platform can list a catalog, once each: the others have no series to
// analyse, so they are neither pending nor an error, they are simply not part of the answer.
func (s *Server) analysable(channels []*core.Record) []*core.Record {
	kept := make([]*core.Record, 0, len(channels))
	seen := map[string]bool{}
	for _, channel := range channels {
		p, ok := s.Platforms.Get(channel.GetString("platform"))
		if !ok || p.CatalogSync() == nil || seen[channel.Id] {
			continue
		}
		seen[channel.Id] = true
		kept = append(kept, channel)
	}
	return kept
}

// addressed addresses the series' channel by its record id: the native id never leaves the server.
func addressed(item series.Series) series.Series {
	item.ChannelID = item.ChannelRecordID
	return item
}

// seriesDetail is the answer of GET /api/series/{id}. Episodes and Dates split Sync for the page: whether
// episodes may still be added, and whether missing publication dates are still being looked up.
type seriesDetail struct {
	Series   series.Series `json:"series"`
	Sync     jobs.Sync     `json:"sync"`
	Episodes jobs.Status   `json:"episodes"`
	Dates    jobs.Status   `json:"dates"`
}

// getSeries answers GET /api/series/{id} where id is a series record id. Series are public catalog-derived
// data, so the record resolves whether or not the profile subscribes to its channel.
func (s *Server) getSeries(e *core.RequestEvent) error {
	noStore(e)
	if _, err := s.profileParam(e); err != nil {
		return err
	}
	item, channel, err := s.Series.Get(e.Request.PathValue("id"))
	if err != nil {
		return err
	}
	status, err := s.Analysis.ChannelStatus(channel)
	if err != nil {
		return apierr.Internal("Failed to load playlist", err)
	}
	list := []series.Series{item}
	s.Analysis.ApplyEpisodes(list)
	sync := jobs.Sync{Status: jobs.Merge(jobs.Complete, status.Status), Loaded: status.Loaded}
	return s.respond(e, http.StatusOK, seriesDetail{Series: addressed(list[0]), Sync: sync, Episodes: status.Episodes, Dates: status.Dates})
}

// ocrSetting is the answer of the recognition setting routes.
type ocrSetting struct {
	Enabled bool `json:"enabled"`
	// Available tells whether this build can recognize anything: without the `ocr` build tag the setting can be
	// read but not turned on.
	Available bool `json:"available"`
}

func (s *Server) getSeriesOCR(e *core.RequestEvent) error {
	noStore(e)
	if err := s.Accounts.RequireAdmin(e.Auth.Id); err != nil {
		return err
	}
	enabled, err := series.OCREnabled(s.App)
	if err != nil {
		return apierr.Internal("Failed to read the recognition setting", err)
	}
	return s.respond(e, http.StatusOK, ocrSetting{Enabled: enabled, Available: s.OCR != nil})
}

// setSeriesOCR answers PATCH /api/admin/series-ocr. The change is written under the administration lock; the
// analysis is told only once it is committed, since it recomputes the stored series on either transition.
func (s *Server) setSeriesOCR(e *core.RequestEvent) error {
	noStore(e)
	if err := s.Accounts.RequireAdmin(e.Auth.Id); err != nil {
		return err
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readBody(e, &input, 1024); err != nil || input.Enabled == nil {
		return accounts.AdminInvalid("Choose whether thumbnail recognition is enabled.", nil)
	}
	if *input.Enabled && s.OCR == nil {
		return apierr.New(http.StatusConflict, "OCR_UNAVAILABLE", "Thumbnail recognition is not available in this build.")
	}
	changed := false
	err := s.Accounts.AdminTx(e.Auth.Id, func(tx core.App, _ *core.Record) error {
		var err error
		changed, err = series.SetOCREnabled(tx, *input.Enabled)
		return err
	})
	if err != nil {
		return err
	}
	if changed {
		s.Analysis.OCRSettingChanged(*input.Enabled)
	}
	return s.respond(e, http.StatusOK, ocrSetting{Enabled: *input.Enabled, Available: s.OCR != nil})
}

// channelSync answers GET /api/channel/{id}?sync=1: the whole catalog of the channel as the background
// synchronization has it so far, and where that synchronization stands. A connected request (?source=) gets the
// catalog as the account sees it, kept in memory for the connection; a public one the stored catalog, whose
// series the analysis is woken to refresh.
func (s *Server) channelSync(e *core.RequestEvent, record *core.Record, p platform.Platform) error {
	noStore(e)
	if p.CatalogSync() == nil {
		return apierr.InvalidInput("This platform does not support background channel synchronization.")
	}
	if err := s.Sources.RequireEnabled(e.Auth.Id, p.ID()); err != nil {
		return err
	}
	conn, err := s.connectionOf(e)
	if err != nil {
		return err
	}
	key := jobs.CatalogKey{Platform: p.ID(), ChannelID: record.GetString("platform_id")}
	if conn.Connected() {
		if conn.Source != p.ID() {
			return apierr.InvalidInput("The selected source and platform must match.")
		}
		if key, err = s.Catalogs.ConnectedKey(e.Auth.Id, p.ID(), key.ChannelID); err != nil {
			return err
		}
	}
	snapshot, err := s.Catalogs.Snapshot(key, jobs.SnapshotOptions{Retry: e.Request.URL.Query().Get("retry") == "1"})
	if err != nil {
		return err
	}
	if conn.Connected() {
		// Recheck before publishing: reconnecting or disconnecting invalidates snapshots.
		if err := s.Catalogs.ScopeCurrent(key); err != nil {
			return err
		}
	} else {
		// The page view infers and stores the series, so the channel's Playlists tab has them without a
		// dedicated endpoint; the subscription-scoped worker alone would never reach an unsubscribed channel.
		s.Analysis.Wake(record.Id)
	}
	return s.respond(e, http.StatusOK, snapshot)
}
