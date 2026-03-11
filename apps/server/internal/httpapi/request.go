package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// profileParam returns the profile named by the `profile` query parameter. It must belong to the caller:
// a missing parameter is a bad request, a profile of someone else is simply not found.
func (s *Server) profileParam(e *core.RequestEvent) (*core.Record, error) {
	id := e.Request.URL.Query().Get("profile")
	if id == "" {
		return nil, apierr.Invalid(`Missing query parameter "profile"`)
	}
	return s.Subscriptions.OwnedProfile(e.Auth.Id, id)
}

// readBody decodes the single JSON object of a request body into target. The body is bounded by maxBytes,
// unknown fields and trailing data are refused.
func readBody(e *core.RequestEvent, target any, maxBytes int64) error {
	e.Request.Body = http.MaxBytesReader(e.Response, e.Request.Body, maxBytes)
	decoder := json.NewDecoder(e.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apierr.InvalidInput("Provide a valid request within the allowed size.")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return apierr.InvalidInput("Provide a single JSON request.")
	}
	return nil
}

// noStore marks an answer private to the account: no browser or shared cache may keep it.
func noStore(e *core.RequestEvent) { e.Response.Header().Set("Cache-Control", "no-store") }
