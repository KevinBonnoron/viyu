package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// profileStats answers GET /api/stats?profile=&period=: what the profile watched, from its history.
func (s *Server) profileStats(e *core.RequestEvent) error {
	noStore(e)
	profile, err := s.profileParam(e)
	if err != nil {
		return err
	}
	stats, err := s.Stats.Compute(e.Auth.Id, profile.Id, e.Request.URL.Query().Get("period"))
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, stats)
}
