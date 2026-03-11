package subscriptions

import (
	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// ClearHistory deletes every watch history entry of one of the account's own profiles; clearing an empty
// history is not an error. The entries go one by one, in one transaction: record deletes broadcast realtime
// events, so connected clients drop the entries without a refetch, and a failure leaves the history whole.
func (s *Service) ClearHistory(userID, profileID string) error {
	profile, err := s.OwnedProfile(userID, profileID)
	if err != nil {
		return err
	}
	err = s.app.RunInTransaction(func(tx core.App) error {
		entries, err := tx.FindRecordsByFilter(historyCollection, "profile = {:profile}", "", 0, 0, dbx.Params{"profile": profile.Id})
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := tx.Delete(entry); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return internalError("Failed to clear history", err)
	}
	return nil
}
