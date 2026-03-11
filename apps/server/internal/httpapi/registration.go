package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
)

// enabledBody is the answer of every registration route.
type enabledBody struct {
	Enabled bool `json:"enabled"`
}

// registration tells whether anyone may create an account: the sign-in screen reads it before there is one.
func (s *Server) registration(e *core.RequestEvent) error {
	noStore(e)
	enabled, err := s.Accounts.RegistrationEnabled()
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, enabledBody{Enabled: enabled})
}

func (s *Server) adminRegistration(e *core.RequestEvent) error {
	actor, err := s.adminActor(e)
	if err != nil {
		return err
	}
	enabled, err := s.Accounts.AdminRegistration(actor)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, enabledBody{Enabled: enabled})
}

func (s *Server) adminSetRegistration(e *core.RequestEvent) error {
	actor, err := s.adminActor(e)
	if err != nil {
		return err
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readBody(e, &input, adminBodyMaxBytes); err != nil {
		return accounts.AdminInvalid("Choose whether public registration is enabled.", nil)
	}
	if err := s.Accounts.AdminSetRegistration(actor, input.Enabled); err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, enabledBody{Enabled: *input.Enabled})
}
