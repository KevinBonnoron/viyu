package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/hook"
)

// isNativeOrigin tells whether origin is one of the fixed origins of the Android (Capacitor) and desktop
// (Wails v3) WebView shells. They are matched exactly: no other origin ever receives credentialed CORS
// access. A development server (--dev) also accepts plain http://localhost, the origin of a Wails or
// Capacitor dev shell; a production server does not, so any page another process serves on localhost
// cannot read credentialed responses.
func isNativeOrigin(origin string, dev bool) bool {
	switch origin {
	case "https://localhost", // Capacitor Android
		"capacitor://localhost",  // Capacitor iOS
		"wails://localhost",      // Wails v3 on Linux (WebKitGTK loads the app from wails://)
		"http://wails.localhost": // Wails v3 on Windows
		return true
	case "http://localhost":
		return dev
	}
	return false
}

var corsMethods = []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete}

// CORS is the server's CORS middleware: the native shells get credentialed access, everyone else the
// plain wildcard. The application binds it on the router with Bind: it carries PocketBase's default CORS
// middleware id, so it REPLACES the default without Unbind's recursive exclusions, which would otherwise
// hide native auth responses.
func (s *Server) CORS() *hook.Handler[*core.RequestEvent] {
	dev := s.App.IsDev()
	native := apis.CORS(apis.CORSConfig{
		AllowOriginFunc:  func(origin string) (bool, error) { return isNativeOrigin(origin, dev), nil },
		AllowMethods:     corsMethods,
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		ExposeHeaders:    []string{"Retry-After"},
		AllowCredentials: true,
		MaxAge:           600,
	})
	other := apis.CORS(apis.CORSConfig{AllowOrigins: []string{"*"}, AllowMethods: corsMethods, ExposeHeaders: []string{"Retry-After"}})
	return &hook.Handler[*core.RequestEvent]{
		Id:       apis.DefaultCorsMiddlewareId,
		Priority: apis.DefaultCorsMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if isNativeOrigin(e.Request.Header.Get("Origin"), dev) {
				return native.Func(e)
			}
			return other.Func(e)
		},
	}
}
