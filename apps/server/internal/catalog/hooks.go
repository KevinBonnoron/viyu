package catalog

import (
	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/hook"
	"github.com/KevinBonnoron/viyu/apps/server/internal/logging"
)

// soleOwnerFilter selects the records only one account owns.
const soleOwnerFilter = "owners:length = 1 && owners.id ?= {:user}"

// RegisterHooks makes deleting an account leave no private record behind. A record the account alone
// owned would otherwise end up with no owners, which is how a public record looks.
//
// The handler runs after PocketBase's own one (priority 100 against 99), so it already works inside the
// transaction that deletes the account: a failure here rolls the deletion back instead of leaving
// records public. Records that other accounts own too just lose this owner, which PocketBase does when
// it cleans up the relation after this handler.
func RegisterHooks(app core.App) {
	app.OnRecordDeleteExecute("users").Bind(&hook.Handler[*core.RecordEvent]{
		Priority: 100,
		Func: func(e *core.RecordEvent) error {
			if err := deleteSoleOwnedRecords(e.App, e.Record.Id); err != nil {
				return err
			}
			return e.Next()
		},
	})
}

// deleteSoleOwnedRecords deletes the videos, then the channels, that userID alone owns. A channel is
// deleted together with the subscriptions of the account's own profiles, which would be deleted with the
// account anyway; one that another account's profile subscribes to is kept (and logged), since
// deleting it is impossible while that subscription exists.
func deleteSoleOwnedRecords(app core.App, userID string) error {
	params := dbx.Params{"user": userID}
	owned, err := app.FindRecordsByFilter(videos, soleOwnerFilter, "", 0, 0, params)
	if err != nil {
		return err
	}
	for _, video := range owned {
		if err := app.Delete(video); err != nil {
			return err
		}
	}
	ownedChannels, err := app.FindRecordsByFilter(channels, soleOwnerFilter, "", 0, 0, params)
	if err != nil {
		return err
	}
	for _, channel := range ownedChannels {
		subscriptions, err := app.FindRecordsByFilter("subscriptions", "channel = {:channel}", "", 0, 0, dbx.Params{"channel": channel.Id})
		if err != nil {
			return err
		}
		foreign := false
		for _, subscription := range subscriptions {
			profile, err := app.FindRecordById("profiles", subscription.GetString("profile"))
			if err != nil || profile.GetString("user") != userID {
				foreign = true
				break
			}
		}
		if foreign {
			logging.Of(app).Warn("A private channel is subscribed to by another account and stays after its owner was deleted", "channel", channel.Id)
			continue
		}
		for _, subscription := range subscriptions {
			if err := app.Delete(subscription); err != nil {
				return err
			}
		}
		if err := app.Delete(channel); err != nil {
			return err
		}
	}
	return nil
}
