package httpapi

import (
	"fmt"
	"io"
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// exportAccount downloads everything the account carries from one instance to another.
func (s *Server) exportAccount(e *core.RequestEvent) error {
	export, err := s.Accounts.Export(e.Auth.Id)
	if err != nil {
		return err
	}
	e.Response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", export.Filename()))
	noStore(e)
	return s.respond(e, http.StatusOK, export)
}

// importAccount merges an export file into the account and answers what was newly created.
func (s *Server) importAccount(e *core.RequestEvent) error {
	body, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, accounts.ImportMaxBytes))
	if err != nil {
		return apierr.Invalid("The export file is too large.")
	}
	export, err := accounts.ParseExport(body)
	if err != nil {
		return err
	}
	counts, err := s.Accounts.Import(e.Auth.Id, export)
	if err != nil {
		return err
	}
	noStore(e)
	return s.respond(e, http.StatusOK, counts)
}
