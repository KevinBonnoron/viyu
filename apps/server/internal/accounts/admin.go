package accounts

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	validation "ganigeorgiev.com/pocketbase/ozzo-validation/v4"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/forms"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// adminPageSize is the number of users on a page of the administration list.
const adminPageSize = 25

// Codes the administration screens know by name.
const (
	codeAdminValidation      = "ADMIN_VALIDATION"
	codeAdminNotFound        = "ADMIN_NOT_FOUND"
	codeAdminUnauthenticated = "ADMIN_UNAUTHENTICATED"
	codeAdminRequired        = "ADMIN_REQUIRED"
	codeAdminLastAdmin       = "ADMIN_LAST_ADMIN"
	codeAdminSelfProtection  = "ADMIN_SELF_PROTECTION"
)

// AdminUser is a user as the administration shows it: never a credential, never a token key.
type AdminUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Verified bool   `json:"verified"`
	Created  string `json:"created"`
}

func adminUserOf(record *core.Record) AdminUser {
	return AdminUser{ID: record.Id, Name: record.GetString("name"), Email: record.Email(), Role: record.GetString("role"), Verified: record.Verified(), Created: record.GetString("created")}
}

// UserPage is one page of the administration list.
type UserPage struct {
	Items      []AdminUser `json:"items"`
	Page       int         `json:"page"`
	TotalPages int64       `json:"totalPages"`
	TotalItems int64       `json:"totalItems"`
}

// UserInput is what an administrator may set on a user. A nil field is left as it is.
type UserInput struct {
	Name            *string `json:"name"`
	Email           *string `json:"email"`
	Password        *string `json:"password"`
	PasswordConfirm *string `json:"passwordConfirm"`
	Role            *string `json:"role"`
}

// AdminInvalid is a refused administration request. data is the field-by-field detail a form can show, or nil.
func AdminInvalid(message string, data any) *apierr.Error {
	e := apierr.New(http.StatusBadRequest, codeAdminValidation, message)
	e.Data = data
	return e
}

func adminMissing() *apierr.Error {
	return apierr.New(http.StatusNotFound, codeAdminNotFound, "The requested user was not found.")
}

func adminConflict(code, message string) *apierr.Error {
	return apierr.New(http.StatusConflict, code, message)
}

// adminActor loads the signed-in account and requires it to be an administrator right now: an auth token
// proves identity, never a cached administrator role.
func adminActor(app core.App, userID string) (*core.Record, error) {
	if userID == "" {
		return nil, apierr.New(http.StatusUnauthorized, codeAdminUnauthenticated, "Sign in to continue.")
	}
	actor, err := app.FindRecordById("users", userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.New(http.StatusUnauthorized, codeAdminUnauthenticated, "Your account is no longer available.")
	}
	if err != nil {
		return nil, internal("Administration request failed", err)
	}
	if actor.GetString("role") != "admin" {
		return nil, apierr.New(http.StatusForbidden, codeAdminRequired, "Administrator access is required.")
	}
	return actor, nil
}

// RequireAdmin refuses an account that is not an administrator right now. A handler calls it before it
// queues on any lock of its own; the work itself runs under AdminTx, which checks again.
func (s *Service) RequireAdmin(userID string) error {
	_, err := adminActor(s.app, userID)
	return err
}

// asAPIError keeps an *apierr.Error and turns anything else into an internal failure.
func asAPIError(err error) error {
	var safe *apierr.Error
	if errors.As(err, &safe) {
		return safe
	}
	return internal("Administration request failed", err)
}

// adminRun authorizes the actor without taking any lock (a request from anyone else must not queue behind
// the administrators), then again inside the same serialized write transaction as the action.
func adminRun[T any](s *Service, actorID string, action func(tx core.App, actor *core.Record) (T, error)) (T, error) {
	var result T
	if err := s.RequireAdmin(actorID); err != nil {
		return result, err
	}
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	err := s.app.RunInTransaction(func(tx core.App) error {
		actor, err := adminActor(tx, actorID)
		if err != nil {
			return err
		}
		result, err = action(tx, actor)
		return err
	})
	if err != nil {
		var zero T
		return zero, asAPIError(err)
	}
	return result, nil
}

// AdminTx runs fn, serialized with every other administration write, in a transaction in which the actor
// is an administrator. It is for administration settings that live outside this package; fn must use tx.
func (s *Service) AdminTx(actorID string, fn func(tx core.App, actor *core.Record) error) error {
	_, err := adminRun(s, actorID, func(tx core.App, actor *core.Record) (struct{}, error) {
		return struct{}{}, fn(tx, actor)
	})
	return err
}

// AdminList returns one page of users, oldest first. page is the raw page parameter of the request ("" is
// the first page); q matches the name or the email.
func (s *Service) AdminList(actorID, page, q string) (*UserPage, error) {
	return adminRun(s, actorID, func(tx core.App, _ *core.Record) (*UserPage, error) {
		number := 1
		if page != "" {
			parsed, err := strconv.Atoi(page)
			if err != nil || parsed < 1 || parsed > math.MaxInt/adminPageSize {
				return nil, AdminInvalid("Invalid user list page.", nil)
			}
			number = parsed
		}
		var filter dbx.Expression = dbx.NewExp("1 = 1")
		if q = strings.TrimSpace(q); q != "" {
			filter = dbx.Or(dbx.Like("name", q), dbx.Like("email", q))
		}
		total, err := tx.CountRecords("users", filter)
		if err != nil {
			return nil, internal("Failed to list users", err)
		}
		records := []*core.Record{}
		if err := tx.RecordQuery("users").AndWhere(filter).OrderBy("created", "id").Limit(adminPageSize).Offset(int64((number - 1) * adminPageSize)).All(&records); err != nil {
			return nil, internal("Failed to list users", err)
		}
		items := make([]AdminUser, len(records))
		for i, record := range records {
			items[i] = adminUserOf(record)
		}
		return &UserPage{Items: items, Page: number, TotalPages: (total + adminPageSize - 1) / adminPageSize, TotalItems: total}, nil
	})
}

// values turns the input into the fields to write. Only these fields are ever granted: an input cannot
// reach the token key, verification of an unchanged email or anything else a form would accept.
func (input UserInput) values(create bool, record *core.Record) (map[string]any, error) {
	data := make(map[string]any, 6)
	if create && input.Role == nil {
		return nil, AdminInvalid("Choose a user or administrator role.", nil)
	}
	if input.Role != nil {
		if *input.Role != "user" && *input.Role != "admin" {
			return nil, AdminInvalid("Choose a user or administrator role.", nil)
		}
		data["role"] = *input.Role
	}
	if input.Name != nil {
		data["name"] = strings.TrimSpace(*input.Name)
	}
	if input.Email != nil {
		email := strings.TrimSpace(*input.Email)
		data["email"] = email
		if email != record.Email() {
			data["verified"] = false
		}
	}
	if create || input.Password != nil || input.PasswordConfirm != nil {
		if input.Password == nil || input.PasswordConfirm == nil || *input.Password == "" || *input.Password != *input.PasswordConfirm {
			return nil, AdminInvalid("Enter a password and matching confirmation.", nil)
		}
		data["password"] = *input.Password
		data["passwordConfirm"] = *input.PasswordConfirm
	}
	return data, nil
}

// submit saves data into record as a trusted form: only whitelisted fields reach it (see values and
// AccountUpdate). A refusal by the record's validation comes back as fields, any other failure as err.
func submit(ctx context.Context, app core.App, record *core.Record, data map[string]any) (fields validation.Errors, err error) {
	form := forms.NewRecordUpsert(app, record)
	form.SetContext(ctx)
	form.GrantManagerAccess()
	form.Load(data)
	err = form.Submit()
	if errors.As(err, &fields) {
		return fields, nil
	}
	return nil, err
}

func adminSubmit(ctx context.Context, tx core.App, record *core.Record, data map[string]any) error {
	fields, err := submit(ctx, tx, record, data)
	if fields != nil {
		return AdminInvalid("The submitted details are invalid.", fields)
	}
	if err != nil {
		return internal("Administration request failed", err)
	}
	return nil
}

// AdminCreate creates a user. The users after-create hook (RegisterHooks) provisions its default profile.
func (s *Service) AdminCreate(ctx context.Context, actorID string, in UserInput) (*AdminUser, error) {
	return adminRun(s, actorID, func(tx core.App, _ *core.Record) (*AdminUser, error) {
		collection, err := tx.FindCollectionByNameOrId("users")
		if err != nil {
			return nil, internal("Administration request failed", err)
		}
		record := core.NewRecord(collection)
		data, err := in.values(true, record)
		if err != nil {
			return nil, err
		}
		if err := adminSubmit(ctx, tx, record, data); err != nil {
			return nil, err
		}
		user := adminUserOf(record)
		return &user, nil
	})
}

func adminFindUser(tx core.App, id string) (*core.Record, error) {
	record, err := tx.FindRecordById("users", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, adminMissing()
	}
	if err != nil {
		return nil, internal("Administration request failed", err)
	}
	return record, nil
}

// adminProtectUser refuses to delete or demote the last administrator, or the actor's own account.
func adminProtectUser(tx core.App, actor, target *core.Record) error {
	if target.GetString("role") == "admin" {
		count, err := tx.CountRecords("users", dbx.HashExp{"role": "admin"})
		if err != nil {
			return internal("Administration request failed", err)
		}
		if count <= 1 {
			return adminConflict(codeAdminLastAdmin, "The last administrator cannot be deleted or demoted.")
		}
	}
	if actor.Id == target.Id {
		return adminConflict(codeAdminSelfProtection, "You cannot delete or demote your own account.")
	}
	return nil
}

// AdminUpdate changes a user: name, email (which un-verifies a new address), password and role.
func (s *Service) AdminUpdate(ctx context.Context, actorID, userID string, in UserInput) (*AdminUser, error) {
	return adminRun(s, actorID, func(tx core.App, actor *core.Record) (*AdminUser, error) {
		record, err := adminFindUser(tx, userID)
		if err != nil {
			return nil, err
		}
		data, err := in.values(false, record)
		if err != nil {
			return nil, err
		}
		if in.Role != nil && *in.Role != "admin" && record.GetString("role") == "admin" {
			if err := adminProtectUser(tx, actor, record); err != nil {
				return nil, err
			}
		}
		if err := adminSubmit(ctx, tx, record, data); err != nil {
			return nil, err
		}
		user := adminUserOf(record)
		return &user, nil
	})
}

// AdminDelete deletes a user; PocketBase follows the schema's cascade relations for what it owns.
func (s *Service) AdminDelete(actorID, userID string) error {
	_, err := adminRun(s, actorID, func(tx core.App, actor *core.Record) (struct{}, error) {
		record, err := adminFindUser(tx, userID)
		if err != nil {
			return struct{}{}, err
		}
		if err := adminProtectUser(tx, actor, record); err != nil {
			return struct{}{}, err
		}
		if err := tx.Delete(record); err != nil {
			return struct{}{}, internal("Failed to delete the user", err)
		}
		return struct{}{}, nil
	})
	return err
}
