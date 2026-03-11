// Package httpapi is the HTTP surface of the server: thin handlers that read a request, call a service and
// write the answer, the middleware they share (authentication, ownership, shutdown tracking) and the one
// error envelope. It holds no business rule and no state of its own.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// Server serves the API. Build it with New and register its routes on the serve event.
type Server struct {
	Deps
}

// New builds the server over its dependencies.
func New(deps Deps) *Server { return &Server{Deps: deps} }

// handler is a route handler that returns its failure instead of writing it: the wrapper made by route
// renders every error through respondError, so no handler has an error shape of its own.
type handler func(e *core.RequestEvent) error

// route wraps a handler with shutdown tracking (a request in flight is awaited by the group and has its
// blocked writes interrupted when the server stops) and the single error envelope.
func (s *Server) route(h handler) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		leave, ok := s.Group.Enter()
		if !ok {
			return s.respondError(e, apierr.Unavailable("Server is shutting down"))
		}
		defer leave()
		// A stream outlives the shutdown signal otherwise: wake its blocked writes so it returns.
		stop := context.AfterFunc(s.Group.Context(), func() { _ = http.NewResponseController(e.Response).SetWriteDeadline(time.Now()) })
		defer stop()
		if err := h(e); err != nil {
			return s.respondError(e, err)
		}
		return nil
	}
}

// Routes registers the handlers of one router. Every network route needs a signed-in account of the users
// collection (a connected source's session and the private records are never public); Public registers
// the few that do not.
type Routes struct {
	s *Server
	r *router.Router[*core.RequestEvent]
}

// API returns the route registrar of a serve event's router.
func (s *Server) API(r *router.Router[*core.RequestEvent]) Routes { return Routes{s: s, r: r} }

func (rt Routes) add(method, path string, h handler, public bool) *router.Route[*core.RequestEvent] {
	route := rt.r.Route(method, path, rt.s.route(h))
	if !public {
		route.Bind(apis.RequireAuth("users"))
	}
	return route
}

// GET, POST, PUT, PATCH and DELETE register an authenticated route.
func (rt Routes) GET(path string, h handler) *router.Route[*core.RequestEvent] {
	return rt.add(http.MethodGet, path, h, false)
}
func (rt Routes) POST(path string, h handler) *router.Route[*core.RequestEvent] {
	return rt.add(http.MethodPost, path, h, false)
}
func (rt Routes) PUT(path string, h handler) *router.Route[*core.RequestEvent] {
	return rt.add(http.MethodPut, path, h, false)
}
func (rt Routes) PATCH(path string, h handler) *router.Route[*core.RequestEvent] {
	return rt.add(http.MethodPatch, path, h, false)
}
func (rt Routes) DELETE(path string, h handler) *router.Route[*core.RequestEvent] {
	return rt.add(http.MethodDelete, path, h, false)
}

// PublicGET registers a route that needs no account.
func (rt Routes) PublicGET(path string, h handler) *router.Route[*core.RequestEvent] {
	return rt.add(http.MethodGet, path, h, true)
}

// requestContext derives the context of a request from the request's own, which also ends when the server
// shuts down, so a downloader or an upstream fetch started for it is stopped with the application.
func (s *Server) requestContext(e *core.RequestEvent) (context.Context, func()) {
	ctx, cancel := context.WithCancel(e.Request.Context())
	stop := context.AfterFunc(s.Group.Context(), cancel)
	return ctx, func() { stop(); cancel() }
}

// viewerID is the id of the account making the request, empty when there is none.
func viewerID(e *core.RequestEvent) string {
	if e.Auth == nil {
		return ""
	}
	return e.Auth.Id
}
