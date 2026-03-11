package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// subscriptionsLive answers GET /api/subscriptions/live?profile=: which of the profile's subscribed channels
// are live now, most watched first. Every lookup goes through the shared short-lived cache, so the
// subscriptions page can ask again every couple of minutes without hitting the platforms each time.
func (s *Server) subscriptionsLive(e *core.RequestEvent) error {
	profile, err := s.profileParam(e)
	if err != nil {
		return err
	}
	channels, err := s.Subscriptions.Subscribed(profile.Id)
	if err != nil {
		return err
	}
	if channels, err = s.Sources.KeepEnabled(e.Auth.Id, channels); err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	streams, err := s.Live.Subscribed(ctx, channels)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, streams)
}
