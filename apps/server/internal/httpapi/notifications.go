package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// profileQuery returns the id of the profile named by the `profile` query parameter, for a service that
// checks the ownership itself.
func profileQuery(e *core.RequestEvent) (string, error) {
	id := e.Request.URL.Query().Get("profile")
	if id == "" {
		return "", apierr.Invalid(`Missing query parameter "profile"`)
	}
	return id, nil
}

// notifications answers GET /api/notifications?profile=: what the profile's subscriptions released since it
// last looked.
func (s *Server) notifications(e *core.RequestEvent) error {
	noStore(e)
	profileID, err := profileQuery(e)
	if err != nil {
		return err
	}
	answer, err := s.Subscriptions.NewVideos(e.Auth.Id, profileID)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, answer)
}

// markNotificationsSeen answers POST /api/notifications/seen: the profile has seen everything up to asOf,
// of one creator when a channel is named.
func (s *Server) markNotificationsSeen(e *core.RequestEvent) error {
	var input struct {
		Profile string `json:"profile"`
		AsOf    string `json:"asOf"`
		Channel string `json:"channel"`
	}
	if err := e.BindBody(&input); err != nil {
		return apierr.Invalid("Invalid notification marker")
	}
	if err := s.Subscriptions.MarkSeen(e.Auth.Id, input.Profile, input.AsOf, input.Channel); err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, map[string]bool{"ok": true})
}
