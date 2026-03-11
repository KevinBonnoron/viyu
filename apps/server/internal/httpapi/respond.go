package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
)

// errorBody is the one envelope of every failed custom route.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// respondError answers a failed request with {"code","message"}: the only error shape of the API. An
// *apierr.Error chooses its own status, code and message; its cause (the real reason, which may hold
// downloader output or database details) is logged and never sent. Anything else is an unexpected failure
// and is answered as a generic 500, again with the error logged only.
func (s *Server) respondError(e *core.RequestEvent, err error) error {
	if ctx := e.Request.Context(); ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		// The client left: there is nobody to answer, and nothing to log.
		return nil
	}
	var safe *apierr.Error
	switch {
	case errors.As(err, &safe):
	case errors.Is(err, context.Canceled):
		// The server is shutting down under a request that did not ask for it.
		safe = apierr.New(http.StatusServiceUnavailable, apierr.CodeShuttingDown, "Server is shutting down").WithCause(err)
	case errors.Is(err, context.DeadlineExceeded):
		safe = apierr.New(http.StatusGatewayTimeout, apierr.CodeRequestFailed, "The request took too long.").WithCause(err)
	default:
		var routed *router.ApiError
		if errors.As(err, &routed) && routed.Status >= 400 && routed.Status < 500 {
			safe = apierr.New(routed.Status, apierr.CodeInvalidRequest, routed.Message)
		} else {
			safe = apierr.Internal("Request failed", err)
		}
	}
	if safe.Cause != nil {
		logging.Of(e.App).Error(safe.Message, "code", safe.Code, "status", safe.Status, "error", safe.Cause)
	}
	body := errorBody{Code: safe.Code, Message: safe.Message, Data: safe.Data}
	data, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		data = []byte(`{"code":"` + apierr.CodeInternal + `","message":"Request failed"}`)
	}
	return e.Blob(safe.Status, "application/json", data)
}

// respond answers a successful request with the JSON of value. The value is serialized BEFORE anything is
// written: a video or channel that refuses to serialize (it was never stored, so it has no record id to
// show) must become an error, not a 200 with half a body or a native id.
func (s *Server) respond(e *core.RequestEvent, status int, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return apierr.Internal("Request failed", err)
	}
	return e.Blob(status, "application/json", data)
}

// noContent answers 204.
func noContent(e *core.RequestEvent) error { return e.NoContent(http.StatusNoContent) }
