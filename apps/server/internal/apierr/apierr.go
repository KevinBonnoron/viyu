// Package apierr is the error every layer of the server returns when a failure is safe to show a client:
// a stable code, a message written for people and the HTTP status that goes with them. The HTTP layer
// serializes it as {"code","message"} and nothing else; downloader stderr, cookies, file names and database
// details only ever travel in Cause, which is logged and never sent.
package apierr

import (
	"errors"
	"net/http"
)

// Error is a client-safe failure.
type Error struct {
	Code    string
	Message string
	Status  int
	// Cause is the underlying failure, for logs only.
	Cause error
	// Data is optional structured detail for the client (field validation messages of a form), serialized
	// as "data" next to the code and the message; nil for every other error.
	Data any
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

// WithCause returns a copy of e that remembers why it happened.
func (e *Error) WithCause(cause error) *Error {
	copied := *e
	copied.Cause = cause
	return &copied
}

// New builds an error.
func New(status int, code, message string) *Error {
	return &Error{Code: code, Message: message, Status: status}
}

// Codes the client knows by name.
const (
	CodeRequestFailed     = "SOURCE_REQUEST_FAILED"
	CodeInvalidSourceArg  = "INVALID_SOURCE_INPUT"
	CodeSubscriberOnly    = "SOURCE_SUBSCRIBER_ONLY"
	CodeChannelOffline    = "SOURCE_CHANNEL_OFFLINE"
	CodeSessionInvalid    = "SOURCE_SESSION_INVALID"
	CodeNotConnected      = "SOURCE_NOT_CONNECTED"
	CodeNotConnectable    = "SOURCE_NOT_CONNECTABLE"
	CodeDisabled          = "SOURCE_DISABLED"
	CodeSourceAuth        = "SOURCE_AUTH_REQUIRED"
	CodeAuthRequired      = "AUTH_REQUIRED"
	CodeStreamNotFound    = "STREAM_NOT_FOUND"
	CodeResourceExpired   = "STREAM_RESOURCE_EXPIRED"
	CodeStreamUpstream    = "STREAM_UPSTREAM_UNAVAILABLE"
	CodeNotFound          = "NOT_FOUND"
	CodeInvalidRequest    = "INVALID_REQUEST"
	CodeInternal          = "INTERNAL_ERROR"
	CodeUnavailable       = "UNAVAILABLE"
	CodeTooManyRequests   = "TOO_MANY_REQUESTS"
	CodeChatConnections   = "CHAT_TOO_MANY_CONNECTIONS"
	CodeForbidden         = "FORBIDDEN"
	CodeConflict          = "CONFLICT"
	CodeShuttingDown      = "SHUTTING_DOWN"
	CodeUpstreamRateLimit = "SOURCE_RATE_LIMITED"

	CodeCompanionUnauthorized = "COMPANION_UNAUTHORIZED"
)

// RequestFailed is the generic upstream failure: the platform or the downloader did not answer.
func RequestFailed() *Error {
	return New(http.StatusBadGateway, CodeRequestFailed, "The source request failed. Please try again.")
}

// RateLimited says the upstream asked the server to slow down.
func RateLimited() *Error {
	return New(http.StatusServiceUnavailable, CodeUpstreamRateLimit, "The source is asking for fewer requests. Please try again later.")
}

// InvalidInput is a request the client got wrong.
func InvalidInput(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidSourceArg, message)
}

// SubscriberOnly is a video reserved for subscribers.
func SubscriberOnly() *Error {
	return New(http.StatusForbidden, CodeSubscriberOnly, "This video is reserved for the channel's subscribers.")
}

// ChannelOffline is a live lookup of a channel that is not broadcasting.
func ChannelOffline() *Error {
	return New(http.StatusNotFound, CodeChannelOffline, "This channel is not live right now.")
}

// SessionInvalid is a connected session that expired or was refused.
func SessionInvalid() *Error {
	return New(http.StatusUnauthorized, CodeSessionInvalid, "The source session is unavailable or expired. Import fresh cookies to reconnect.")
}

// NotConnected asks the account to connect a source first.
func NotConnected() *Error {
	return New(http.StatusConflict, CodeNotConnected, "Connect this source first.")
}

// NotConnectable is a connection request on a platform with no account to connect.
func NotConnectable() *Error {
	return New(http.StatusBadRequest, CodeNotConnectable, "This source has no account to connect.")
}

// Disabled is a request about a source the account turned off.
func Disabled() *Error {
	return New(http.StatusConflict, CodeDisabled, "This source is turned off. Turn it on in Sources to use it.")
}

// SourceAuthRequired is a connected-source request without a signed-in account.
func SourceAuthRequired() *Error {
	return New(http.StatusUnauthorized, CodeSourceAuth, "Sign in to use a connected source.")
}

// AuthRequired asks for a signed-in account.
func AuthRequired() *Error {
	return New(http.StatusUnauthorized, CodeAuthRequired, "Sign in to continue.")
}

// CompanionUnauthorized is a companion request whose device token is missing, unknown or revoked.
func CompanionUnauthorized() *Error {
	return New(http.StatusUnauthorized, CodeCompanionUnauthorized, "This browser is not paired with Viyu, or its pairing was removed. Pair it again from Sources.")
}

// StreamUnavailable is a video that cannot be streamed.
func StreamUnavailable() *Error {
	return New(http.StatusNotFound, CodeStreamNotFound, "This video cannot be streamed.")
}

// NotFound is a missing or invisible record; what names it ("Video", "Channel"…).
func NotFound(what string) *Error {
	return New(http.StatusNotFound, CodeNotFound, what+" not found")
}

// Invalid is a malformed request that is not about a source.
func Invalid(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidRequest, message)
}

// Internal is an unexpected failure; the message is what the client may read, the cause what the log keeps.
func Internal(message string, cause error) *Error {
	return &Error{Code: CodeInternal, Message: message, Status: http.StatusInternalServerError, Cause: cause}
}

// Unavailable is a service that cannot take the request right now.
func Unavailable(message string) *Error {
	return New(http.StatusServiceUnavailable, CodeUnavailable, message)
}

// From returns err as an *Error: itself when it is one, a generic upstream failure otherwise (the
// original error stays as the cause).
func From(err error) *Error {
	var safe *Error
	if errors.As(err, &safe) {
		return safe
	}
	return RequestFailed().WithCause(err)
}
