package httpapi

import (
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
)

// createSubscription answers POST /api/subscriptions: the profile subscribes to a stored channel. Waking the
// analysis of the new channel is the application's business (a hook on the record), not the route's.
func (s *Server) createSubscription(e *core.RequestEvent) error {
	var input struct {
		Profile   string `json:"profile"`
		ChannelID string `json:"channelId"`
		Creator   string `json:"creator"`
	}
	if err := e.BindBody(&input); err != nil {
		return apierr.Invalid("Invalid subscription")
	}
	subscription, err := s.Subscriptions.Create(e.Auth.Id, input.Profile, input.ChannelID, input.Creator)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, subscription)
}

// groupSubscriptions answers POST /api/subscriptions/group: it links subscriptions on distinct platforms
// into one creator.
func (s *Server) groupSubscriptions(e *core.RequestEvent) error {
	var input struct {
		Profile         string   `json:"profile"`
		SubscriptionIDs []string `json:"subscriptionIds"`
	}
	if err := e.BindBody(&input); err != nil {
		return apierr.Invalid("Invalid subscription group")
	}
	members, err := s.Subscriptions.Group(e.Auth.Id, input.Profile, input.SubscriptionIDs)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, members)
}

// ungroupSubscriptions answers POST /api/subscriptions/ungroup: it separates a creator.
func (s *Server) ungroupSubscriptions(e *core.RequestEvent) error {
	var input struct {
		Profile string `json:"profile"`
		Creator string `json:"creator"`
	}
	if err := e.BindBody(&input); err != nil {
		return apierr.Invalid("Invalid subscription group")
	}
	members, err := s.Subscriptions.Ungroup(e.Auth.Id, input.Profile, input.Creator)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, members)
}

// moveSubscriptions answers POST /api/subscriptions/move: it moves (or, with copy, copies) subscriptions from
// one profile of the account to another.
func (s *Server) moveSubscriptions(e *core.RequestEvent) error {
	var input struct {
		From            string   `json:"from"`
		To              string   `json:"to"`
		SubscriptionIDs []string `json:"subscriptionIds"`
		Copy            bool     `json:"copy"`
	}
	if err := e.BindBody(&input); err != nil {
		return apierr.Invalid("Invalid subscription move")
	}
	result, err := s.Subscriptions.Move(e.Auth.Id, input.From, input.To, input.SubscriptionIDs, input.Copy)
	if err != nil {
		return err
	}
	return s.respond(e, http.StatusOK, result)
}

// refreshSubscriptions answers POST /api/subscriptions/refresh?profile=: it refreshes the listings of the
// profile's expired channels. The videos themselves are read through the records API, so the page shows the
// cached rows at once and reloads when this reports that something changed.
func (s *Server) refreshSubscriptions(e *core.RequestEvent) error {
	profile, err := s.profileParam(e)
	if err != nil {
		return err
	}
	// Creators found on two platforms are merged before the (slow) refresh, then again once it has brought
	// fresher channel names.
	s.autoGroup(profile.Id)
	channels, err := s.Subscriptions.Subscribed(profile.Id)
	if err != nil {
		return err
	}
	if channels, err = s.Sources.KeepEnabled(e.Auth.Id, channels); err != nil {
		return err
	}
	ctx, done := s.requestContext(e)
	defer done()
	refreshed, failed, err := s.Refresher.Refresh(ctx, channels)
	if err != nil {
		return err
	}
	if refreshed > 0 {
		s.autoGroup(profile.Id)
	}
	// A stream that ended since the last visit must not keep its live badge.
	s.Live.Reconcile(ctx, channels)
	return s.respond(e, http.StatusOK, map[string]int{"refreshed": refreshed, "failed": failed})
}

// autoGroup merges a profile's obvious cross-platform duplicates. A failure is only logged: grouping is a
// convenience.
func (s *Server) autoGroup(profileID string) {
	if _, err := s.Subscriptions.AutoGroup(profileID); err != nil {
		logging.Of(s.App).Warn("Failed to group subscriptions automatically", "error", err)
	}
}
