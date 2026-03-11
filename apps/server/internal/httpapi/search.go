package httpapi

import (
	"net/http"
	"strconv"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// searchAllPlatforms is the value of the `platform` parameter that searches every platform the account uses.
const searchAllPlatforms = "all"

// search answers GET /api/search?q=&page=&platform=&source=: one platform (public, or as a connected source),
// or every platform the account uses at once. The results are stored first, so each shows its record id.
func (s *Server) search(e *core.RequestEvent) error {
	conn, session, cleanup, err := s.session(e)
	if err != nil {
		return err
	}
	defer cleanup()
	query := e.Request.URL.Query()
	text := query.Get("q")
	if text == "" {
		return apierr.Invalid(`Missing query parameter "q"`)
	}
	all := !conn.Connected() && query.Get("platform") == searchAllPlatforms
	var p platform.Platform
	if !all {
		if p, err = s.requestedPlatform(e, conn.Source); err != nil {
			return err
		}
		if err = s.Sources.RequireEnabled(e.Auth.Id, p.ID()); err != nil {
			return err
		}
	}
	page := 1
	if value := query.Get("page"); value != "" {
		if page, err = strconv.Atoi(value); err != nil || page < 1 || page > platform.MaxListPage {
			return apierr.Invalid("Invalid search page")
		}
	}
	ctx, done := s.requestContext(e)
	defer done()
	var result platform.SearchPage
	if all {
		active, err := s.Sources.Activations(e.Auth.Id)
		if err != nil {
			return err
		}
		result, err = s.Search.All(ctx, s.Search.Enabled(active), text, page)
		if err != nil {
			return err
		}
	} else if result, err = s.Search.One(ctx, p, session, text, page); err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, result)
}
