package httpapi

import (
	"net/http"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/companion"
)

const (
	// companionHeader carries the device token of a paired browser extension.
	companionHeader = "X-Viyu-Companion"
	// companionBodyLimit bounds the body of a sync: 5000 channel ids or 500 history entries.
	companionBodyLimit = 512 << 10
)

// registerCompanion registers the routes of the browser extension. The account side (pairing codes and the
// list of devices) is an ordinary signed-in route; the extension's own routes authenticate with the device
// token in X-Viyu-Companion, which companionRoute checks, so they are registered public.
func (s *Server) registerCompanion(api Routes) {
	api.POST("/api/companion/pairings", private(s.createPairing))
	api.GET("/api/companion/devices", private(s.listDevices))
	api.DELETE("/api/companion/devices/{id}", private(s.revokeDevice))

	api.PublicPOST("/api/companion/pair", private(s.pairDevice))
	api.PublicGET("/api/companion/session", s.companionRoute(s.companionSession))
	api.PublicDELETE("/api/companion/session", s.companionRoute(s.companionUnpair))
	api.PublicPOST("/api/companion/youtube/subscriptions", s.companionRoute(s.syncSubscriptions))
	api.PublicPOST("/api/companion/youtube/history", s.companionRoute(s.syncHistory))
}

// companionRoute authenticates the device that makes the request and hands it to h. Its answers are private.
func (s *Server) companionRoute(h func(*core.RequestEvent, *companion.Device) error) handler {
	return private(func(e *core.RequestEvent) error {
		device, err := s.Companion.Authenticate(e.Request.Header.Get(companionHeader))
		if err != nil {
			return err
		}
		return h(e, device)
	})
}

// createPairing answers POST /api/companion/pairings: a single-use code that pairs a browser to a profile.
func (s *Server) createPairing(e *core.RequestEvent) error {
	var body struct {
		Profile string `json:"profile"`
	}
	if err := readBody(e, &body, sourceBodyLimit); err != nil {
		return err
	}
	code, expires, err := s.Companion.CreatePairing(e.Auth.Id, body.Profile)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, map[string]string{"code": code, "expiresAt": expires.UTC().Format(time.RFC3339)})
}

// listDevices answers GET /api/companion/devices: the browsers paired to the account, newest first.
func (s *Server) listDevices(e *core.RequestEvent) error {
	devices, err := s.Companion.Devices(e.Auth.Id)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, devices)
}

// revokeDevice answers DELETE /api/companion/devices/{id}.
func (s *Server) revokeDevice(e *core.RequestEvent) error {
	if err := s.Companion.Revoke(e.Auth.Id, e.Request.PathValue("id")); err != nil {
		return err
	}
	return noContent(e)
}

// pairDevice answers POST /api/companion/pair: the extension trades the pairing code for its token.
func (s *Server) pairDevice(e *core.RequestEvent) error {
	var body struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := readBody(e, &body, sourceBodyLimit); err != nil {
		return err
	}
	token, device, err := s.Companion.Pair(body.Code, body.Name)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, map[string]any{
		"token":  token,
		"device": map[string]string{"id": device.ID, "name": device.Name, "profileName": device.ProfileName},
	})
}

// companionSession answers GET /api/companion/session: who the token belongs to.
func (s *Server) companionSession(e *core.RequestEvent, device *companion.Device) error {
	return s.respond(e, http.StatusOK, map[string]any{
		"device": map[string]string{"id": device.ID, "name": device.Name, "profileName": device.ProfileName, "lastSyncAt": device.LastSyncAt},
	})
}

// companionUnpair answers DELETE /api/companion/session: the extension unpairs itself, and its token stops
// working at once.
func (s *Server) companionUnpair(e *core.RequestEvent, device *companion.Device) error {
	if err := s.Companion.Revoke(device.UserID(), device.ID); err != nil {
		return err
	}
	return noContent(e)
}

// syncSubscriptions answers POST /api/companion/youtube/subscriptions.
func (s *Server) syncSubscriptions(e *core.RequestEvent, device *companion.Device) error {
	var body struct {
		Channels []string `json:"channels"`
		Complete bool     `json:"complete"`
	}
	if err := readBody(e, &body, companionBodyLimit); err != nil {
		return err
	}
	result, err := s.Companion.SyncSubscriptions(device, body.Channels, body.Complete)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, result)
}

// syncHistory answers POST /api/companion/youtube/history.
func (s *Server) syncHistory(e *core.RequestEvent, device *companion.Device) error {
	var body struct {
		Entries []companion.HistoryEntry `json:"entries"`
	}
	if err := readBody(e, &body, companionBodyLimit); err != nil {
		return err
	}
	result, err := s.Companion.SyncHistory(device, body.Entries)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, result)
}
