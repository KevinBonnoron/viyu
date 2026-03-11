package httpapi

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// clearHistory answers DELETE /api/history?profile=: it empties the watch history of one of the caller's
// own profiles.
func (s *Server) clearHistory(e *core.RequestEvent) error {
	profileID, err := profileQuery(e)
	if err != nil {
		return err
	}
	if err := s.Subscriptions.ClearHistory(e.Auth.Id, profileID); err != nil {
		return err
	}
	return noContent(e)
}
