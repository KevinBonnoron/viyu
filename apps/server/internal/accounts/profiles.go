package accounts

import (
	"database/sql"
	"errors"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
)

func ensureDefaultProfile(app core.App, user *core.Record) (*core.Record, error) {
	profile, err := app.FindFirstRecordByFilter("profiles", "user = {:user}", dbx.Params{"user": user.Id})
	if err == nil {
		return profile, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	profiles, err := app.FindCollectionByNameOrId("profiles")
	if err != nil {
		return nil, err
	}
	name := truncateRunes(strings.TrimSpace(user.GetString("name")), maxProfileNameLength)
	if name == "" {
		name = "Profile 1"
	}
	profile = core.NewRecord(profiles)
	profile.Set("user", user.Id)
	profile.Set("name", name)
	if err := app.Save(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

// RegisterHooks binds the rules around profiles that the records API cannot express: a new account gets a
// default profile (a failure is logged and never fails the signup), and a request cannot delete the last
// profile of an account. Deleting an account cascades to its profiles without going through the request
// hook.
func RegisterHooks(app core.App) {
	app.OnRecordAfterCreateSuccess("users").BindFunc(func(event *core.RecordEvent) error {
		if _, err := ensureDefaultProfile(event.App, event.Record); err != nil {
			logging.Of(event.App).Error("Failed to create the default profile", "user", event.Record.Id, "error", err)
		}
		return event.Next()
	})
	app.OnRecordDeleteRequest("profiles").BindFunc(func(event *core.RecordRequestEvent) error {
		count, err := event.App.CountRecords("profiles", dbx.HashExp{"user": event.Record.GetString("user")})
		if err != nil {
			return err
		}
		if count <= 1 {
			return event.BadRequestError("The last profile of an account cannot be deleted.", nil)
		}
		return event.Next()
	})
}
