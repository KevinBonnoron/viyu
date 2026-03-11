package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
)

// adminBodyMaxBytes bounds the body of an administration request.
const adminBodyMaxBytes = 16 << 10

// adminActor marks the answer private and refuses a request that does not come from an administrator right
// now (a token proves identity, never a cached role). It runs before the body is read and before the
// service queues on the lock that serializes administration writes; the service checks again inside its
// transaction.
func (s *Server) adminActor(e *core.RequestEvent) (string, error) {
	noStore(e)
	if err := s.Accounts.RequireAdmin(e.Auth.Id); err != nil {
		return "", err
	}
	return e.Auth.Id, nil
}

func (s *Server) adminListUsers(e *core.RequestEvent) error {
	actor, err := s.adminActor(e)
	if err != nil {
		return err
	}
	query := e.Request.URL.Query()
	page, err := s.Accounts.AdminList(actor, query.Get("page"), query.Get("q"))
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, page)
}

// adminUserInput reads the user an administrator describes.
func adminUserInput(e *core.RequestEvent) (accounts.UserInput, error) {
	var input accounts.UserInput
	if err := readBody(e, &input, adminBodyMaxBytes); err != nil {
		return input, accounts.AdminInvalid("Invalid user details.", nil)
	}
	return input, nil
}

func (s *Server) adminCreateUser(e *core.RequestEvent) error {
	actor, err := s.adminActor(e)
	if err != nil {
		return err
	}
	input, err := adminUserInput(e)
	if err != nil {
		return err
	}
	ctx, cancel := s.requestContext(e)
	defer cancel()
	user, err := s.Accounts.AdminCreate(ctx, actor, input)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusCreated, user)
}

func (s *Server) adminUpdateUser(e *core.RequestEvent) error {
	actor, err := s.adminActor(e)
	if err != nil {
		return err
	}
	input, err := adminUserInput(e)
	if err != nil {
		return err
	}
	ctx, cancel := s.requestContext(e)
	defer cancel()
	user, err := s.Accounts.AdminUpdate(ctx, actor, e.Request.PathValue("id"), input)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, user)
}

func (s *Server) adminDeleteUser(e *core.RequestEvent) error {
	actor, err := s.adminActor(e)
	if err != nil {
		return err
	}
	if err := s.Accounts.AdminDelete(actor, e.Request.PathValue("id")); err != nil {
		return err
	}
	return noContent(e)
}
