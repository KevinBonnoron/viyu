package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// accountBodyMaxBytes bounds the body of a self-service account update.
const accountBodyMaxBytes = 16 << 10

// account tells what the signed-in account may do, read from its record right now (never from the token).
func (s *Server) account(e *core.RequestEvent) error {
	noStore(e)
	admin, err := s.Accounts.IsAdmin(e.Auth.Id)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, map[string]bool{"isAdmin": admin})
}

// updateAccount applies a self-service change and answers a fresh auth response: a new password
// invalidates the tokens issued before it.
func (s *Server) updateAccount(e *core.RequestEvent) error {
	var input accounts.AccountUpdate
	if err := readBody(e, &input, accountBodyMaxBytes); err != nil {
		return apierr.Invalid("Invalid account details.")
	}
	ctx, cancel := s.requestContext(e)
	defer cancel()
	record, err := s.Accounts.Update(ctx, e.Auth.Id, input)
	if err != nil {
		return err
	}
	noStore(e)
	// The auth response evaluates the collection's auth rule against the request, which parses the body
	// again; it was consumed above, so it has none left to offer.
	e.Request.Body, e.Request.ContentLength = http.NoBody, 0
	return apis.RecordAuthResponse(e, record, "account", nil)
}
