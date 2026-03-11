package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// Register adds the whole API to the serve event's router: the native-origin CORS middleware, the health
// shape, every route group and the answer for an unknown API path.
func (s *Server) Register(se *core.ServeEvent) {
	se.Router.Bind(s.CORS())
	// Keep the existing health shape at PocketBase's canonical health URL.
	se.Router.BindFunc(func(e *core.RequestEvent) error {
		if e.Request.Method == http.MethodGet && e.Request.URL.Path == "/api/health" {
			return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
		}
		return e.Next()
	})
	api := s.API(se.Router)
	s.registerContent(api)
	s.registerLibrary(api)
	s.registerAccount(api)
	s.registerSeries(api)
	notFound := s.route(func(*core.RequestEvent) error { return apierr.NotFound("Route") })
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		se.Router.Route(method, "/api", notFound)
		se.Router.Route(method, "/api/{path...}", notFound)
	}
}
