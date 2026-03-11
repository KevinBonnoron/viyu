package accounts

import (
	"fmt"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// RegisterAdminRoles binds the rules of the users collection (who may create an account, who may change a
// role) and adds the `admin promote EMAIL` command, the way to appoint the first administrator.
func RegisterAdminRoles(app *pocketbase.PocketBase) {
	registerRoleGuards(app)

	command := &cobra.Command{Use: "admin", Short: "Manage Viyu administrators"}
	command.AddCommand(&cobra.Command{
		Use:          "promote EMAIL",
		Short:        "Promote an existing Viyu user to administrator",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			// Unlike serve, a custom CLI command does not automatically run the
			// embedded app migrations. Bootstrap also supports direct Cobra use.
			if !app.IsBootstrapped() {
				if err := app.Bootstrap(); err != nil {
					return fmt.Errorf("bootstrap database: %w", err)
				}
			}
			if err := app.RunAllMigrations(); err != nil {
				return fmt.Errorf("migrate database: %w", err)
			}
			if err := PromoteAdmin(app, strings.TrimSpace(args[0])); err != nil {
				return err
			}
			command.Println("Viyu administrator promoted.")
			return nil
		},
	})
	app.RootCmd.AddCommand(command)
}

func registerRoleGuards(app core.App) {
	// Defaults apply to trusted forms/app writes as well as public signups.
	app.OnRecordValidate("users").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.IsNew() && e.Record.GetString("role") == "" {
			e.Record.Set("role", "user")
		}
		return e.Next()
	})
	// Request guards also cover PocketBase dashboard requests. An account is always created as an ordinary
	// user, by anyone (a superuser included); only an authenticated superuser may then promote it.
	app.OnRecordCreateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Auth == nil || !e.Auth.IsSuperuser() {
			enabled, err := registrationEnabled(e.App)
			if err != nil {
				return e.InternalServerError("Registration settings are unavailable.", err)
			}
			if !enabled {
				return e.ForbiddenError("Public registration is disabled.", nil)
			}
		}
		role := e.Record.GetString("role")
		if role != "" && role != "user" {
			return e.BadRequestError("An account is created as an ordinary user; promote it afterwards to grant a role.", nil)
		}
		e.Record.Set("role", "user")
		return e.Next()
	})
	app.OnRecordUpdateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
		original, current := e.Record.Original().GetString("role"), e.Record.GetString("role")
		if e.Auth != nil && e.Auth.IsSuperuser() {
			if original == current || (original == "user" && current == "admin") {
				return e.Next()
			}
			return e.BadRequestError("Only promotion is available here; administrators are demoted from Viyu's user administration, which keeps at least one administrator.", nil)
		}
		info, err := e.RequestInfo()
		if err != nil {
			return e.BadRequestError("Invalid account update.", err)
		}
		if _, present := info.Body["role"]; present || current != original {
			return e.BadRequestError("Roles cannot be changed through self-service.", nil)
		}
		return e.Next()
	})
}

// PromoteAdmin makes an existing user an administrator.
func PromoteAdmin(app core.App, email string) error {
	return app.RunInTransaction(func(tx core.App) error {
		user, err := tx.FindAuthRecordByEmail("users", email)
		if err != nil {
			return fmt.Errorf("existing Viyu user %q not found: %w", email, err)
		}
		user.Set("role", "admin")
		return tx.Save(user)
	})
}
