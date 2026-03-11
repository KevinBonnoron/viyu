// Package accounts is everything about the people using the instance: their account and profiles, the
// roles and the registration switch, the administration of the user list, and the export and import of an
// account's data. It is a service layer: plain parameters in, *apierr.Error out, no HTTP.
package accounts

import (
	"net/http"
	"sync"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
)

// Service owns the account rules. It is safe for concurrent use.
type Service struct {
	app       core.App
	store     *catalog.Store
	platforms *platform.Registry

	// adminMu serializes the administration writes: the checks that keep one administrator (and the
	// administrator's own account) in place read what a competing request would change.
	adminMu sync.Mutex
}

// New builds the service. The sources service is not needed by the account rules themselves (an import
// writes activations through sources.WriteActivation, inside its own transaction); it is accepted so the
// composition hands every account-adjacent service the same collaborators.
func New(app core.App, store *catalog.Store, _ *sources.Service, platforms *platform.Registry) *Service {
	return &Service{app: app, store: store, platforms: platforms}
}

// maxProfileNameLength is the limit schema.json sets on profiles.name.
const maxProfileNameLength = 40

// internal is an unexpected failure: the client reads message, the log keeps err.
func internal(message string, err error) *apierr.Error {
	return apierr.Internal(message, err)
}

// invalid is a malformed request that carries structured detail (field validation messages) for the client.
func invalid(message string, data any) *apierr.Error {
	e := apierr.Invalid(message)
	e.Data = data
	return e
}

// accountGone is the answer for a signed-in account that no longer exists.
func accountGone() *apierr.Error {
	return apierr.New(http.StatusUnauthorized, apierr.CodeAuthRequired, "Your account is no longer available.")
}

// truncateRunes cuts s to at most max characters without splitting a multi-byte character.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
