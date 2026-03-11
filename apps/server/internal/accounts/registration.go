package accounts

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

func registrationEnabled(app core.App) (bool, error) {
	var enabled bool
	err := app.DB().NewQuery("SELECT [[registration_enabled]] FROM {{app_settings}} WHERE [[id]] = 1").Row(&enabled)
	return enabled, err
}

// RegistrationEnabled tells whether anyone may create an account (the public setting).
func (s *Service) RegistrationEnabled() (bool, error) {
	enabled, err := registrationEnabled(s.app)
	if err != nil {
		return false, internal("Failed to load registration settings", err)
	}
	return enabled, nil
}

// AdminRegistration is RegistrationEnabled for an administrator's settings page.
func (s *Service) AdminRegistration(actorID string) (bool, error) {
	return adminRun(s, actorID, func(tx core.App, _ *core.Record) (bool, error) {
		enabled, err := registrationEnabled(tx)
		if err != nil {
			return false, internal("Failed to load registration settings", err)
		}
		return enabled, nil
	})
}

// AdminSetRegistration turns public registration on or off; nil (no choice made) is refused.
func (s *Service) AdminSetRegistration(actorID string, enabled *bool) error {
	_, err := adminRun(s, actorID, func(tx core.App, _ *core.Record) (struct{}, error) {
		if enabled == nil {
			return struct{}{}, AdminInvalid("Choose whether public registration is enabled.", nil)
		}
		_, err := tx.DB().NewQuery("UPDATE {{app_settings}} SET [[registration_enabled]] = {:enabled} WHERE [[id]] = 1").Bind(map[string]any{"enabled": *enabled}).Execute()
		if err != nil {
			return struct{}{}, internal("Failed to update registration settings", err)
		}
		return struct{}{}, nil
	})
	return err
}
