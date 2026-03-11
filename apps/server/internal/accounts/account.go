package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	validation "ganigeorgiev.com/pocketbase/ozzo-validation/v4"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// IsAdmin tells whether the account is an administrator right now (read from the record, not from a token).
func (s *Service) IsAdmin(userID string) (bool, error) {
	user, err := s.app.FindRecordById("users", userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, accountGone()
	}
	if err != nil {
		return false, internal("Failed to load the account", err)
	}
	return user.GetString("role") == "admin", nil
}

// AccountUpdate is what an account may change about itself. A nil Name or Email is left alone; setting
// either Password or PasswordConfirm asks for a password change, which OldPassword must prove.
type AccountUpdate struct {
	Name            *string `json:"name"`
	Email           *string `json:"email"`
	OldPassword     string  `json:"oldPassword"`
	Password        *string `json:"password"`
	PasswordConfirm *string `json:"passwordConfirm"`
}

// Update applies a self-service change and returns the saved account record: the caller issues a fresh
// auth token from it, since a new password invalidates the old ones. The email cannot be changed here, and
// nothing but the whitelisted fields is ever written (the role in particular).
func (s *Service) Update(ctx context.Context, userID string, in AccountUpdate) (*core.Record, error) {
	record, err := s.app.FindRecordById("users", userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, accountGone()
	}
	if err != nil {
		return nil, internal("Failed to load the account", err)
	}
	data := make(map[string]any, 4)
	if in.Name != nil {
		data["name"] = strings.TrimSpace(*in.Name)
	}
	if in.Email != nil && strings.TrimSpace(*in.Email) != record.Email() {
		return nil, invalid("Email addresses cannot be changed through self-service.", nil)
	}
	if in.Password != nil || in.PasswordConfirm != nil {
		if in.Password == nil || in.PasswordConfirm == nil || *in.Password == "" || *in.Password != *in.PasswordConfirm {
			return nil, invalid("Enter a new password and matching confirmation.", validation.Errors{"passwordConfirm": validation.NewError("validation_required", "Enter matching password confirmation.")})
		}
		if !record.ValidatePassword(in.OldPassword) {
			return nil, invalid("The current password is incorrect.", validation.Errors{"oldPassword": validation.NewError("validation_invalid_password", "The current password is incorrect.")})
		}
		data["password"] = *in.Password
		data["passwordConfirm"] = *in.PasswordConfirm
	}
	// Only the whitelisted self-service fields above are granted manager access, after checking the
	// current password.
	fields, err := submit(ctx, s.app, record, data)
	if fields != nil {
		return nil, invalid("Your account could not be updated.", fields)
	}
	if err != nil {
		return nil, internal("Your account could not be updated.", err)
	}
	return record, nil
}
