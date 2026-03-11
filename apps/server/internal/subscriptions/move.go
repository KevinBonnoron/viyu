package subscriptions

import (
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// MoveResult says what Move did. Moved counts the subscriptions that now exist on the target profile because
// of the call (the rows that moved, or the rows a copy created); Merged counts the ones the target already
// had, which absorbed the request instead of being duplicated. MovedIDs and MergedIDs name those rows on the
// target profile: a client undoes a move by moving MovedIDs back and copying MergedIDs back.
type MoveResult struct {
	Moved     int      `json:"moved"`
	Merged    int      `json:"merged"`
	MovedIDs  []string `json:"movedIds"`
	MergedIDs []string `json:"mergedIds"`
}

// Move transfers subscriptions from one profile of the account to another, or copies them when copying is set,
// in one transaction. A subscription whose channel the target already follows is merged: the source row is
// deleted (kept when copying) and the target's own is left as it is, so the target never holds a duplicate.
//
// A creator whose members all move goes along with them. When only some of its members move, the moved ones
// leave the group and the creator that remains on the source is pruned below two members as usual. Copying
// follows the same rule with a new creator on the target. The notification markers, the series and the watch
// history belong to the activity of a profile and stay where they are.
func (s *Service) Move(userID, fromID, toID string, subscriptionIDs []string, copying bool) (*MoveResult, error) {
	from, err := s.OwnedProfile(userID, strings.TrimSpace(fromID))
	if err != nil {
		return nil, err
	}
	to, err := s.OwnedProfile(userID, strings.TrimSpace(toID))
	if err != nil {
		return nil, err
	}
	if from.Id == to.Id {
		return nil, apierr.Invalid("Choose another profile")
	}
	ids := make([]string, 0, len(subscriptionIDs))
	named := make(map[string]bool, len(subscriptionIDs))
	for _, id := range subscriptionIDs {
		if id = strings.TrimSpace(id); id != "" && !named[id] {
			named[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, apierr.Invalid("At least one subscription is required")
	}
	result := &MoveResult{MovedIDs: []string{}, MergedIDs: []string{}}
	err = s.app.RunInTransaction(func(tx core.App) error {
		*result = MoveResult{MovedIDs: []string{}, MergedIDs: []string{}}
		all, err := tx.FindRecordsByFilter(subscriptionsCollection, "profile = {:profile}", "created", 0, 0, dbx.Params{"profile": from.Id})
		if err != nil {
			return err
		}
		byID := make(map[string]*core.Record, len(all))
		members := map[string]int{}
		for _, subscription := range all {
			byID[subscription.Id] = subscription
			if creator := subscription.GetString("creator"); creator != "" {
				members[creator]++
			}
		}
		selected := make([]*core.Record, 0, len(ids))
		picked := map[string]int{}
		for _, id := range ids {
			subscription := byID[id]
			if subscription == nil {
				return apierr.NotFound("Subscription")
			}
			selected = append(selected, subscription)
			if creator := subscription.GetString("creator"); creator != "" {
				picked[creator]++
			}
		}
		existing, err := tx.FindRecordsByFilter(subscriptionsCollection, "profile = {:profile}", "created", 0, 0, dbx.Params{"profile": to.Id})
		if err != nil {
			return err
		}
		held := make(map[string]*core.Record, len(existing))
		for _, subscription := range existing {
			held[subscription.GetString("channel")] = subscription
		}

		// The creators that travel whole, and the creator each of them has on the target.
		destination := map[string]string{}
		for creatorID, count := range picked {
			if count != members[creatorID] {
				continue
			}
			creator, err := tx.FindRecordById(creatorsCollection, creatorID)
			if err != nil {
				return err
			}
			if copying {
				record, err := StoreCreator(tx, to.Id, creator.GetString("name"))
				if err != nil {
					return err
				}
				destination[creatorID] = record.Id
				continue
			}
			creator.Set("profile", to.Id)
			if err := tx.Save(creator); err != nil {
				return err
			}
			destination[creatorID] = creatorID
		}

		for _, subscription := range selected {
			creator := destination[subscription.GetString("creator")]
			if twin := held[subscription.GetString("channel")]; twin != nil {
				result.Merged++
				result.MergedIDs = append(result.MergedIDs, twin.Id)
				// A twin outside any group joins the creator that travels with its channel.
				if creator != "" && twin.GetString("creator") == "" {
					twin.Set("creator", creator)
					twin.Set("ungrouped", false)
					if err := tx.Save(twin); err != nil {
						return err
					}
				}
				if !copying {
					if err := tx.Delete(subscription); err != nil {
						return err
					}
				}
				continue
			}
			moved := subscription
			if copying {
				collection, err := tx.FindCollectionByNameOrId(subscriptionsCollection)
				if err != nil {
					return err
				}
				moved = core.NewRecord(collection)
				moved.Set("channel", subscription.GetString("channel"))
				moved.Set("ungrouped", subscription.GetBool("ungrouped"))
			}
			moved.Set("profile", to.Id)
			moved.Set("creator", creator)
			if err := tx.Save(moved); err != nil {
				return err
			}
			result.Moved++
			result.MovedIDs = append(result.MovedIDs, moved.Id)
		}
		if !copying {
			if err := PruneCreators(tx, from.Id); err != nil {
				return err
			}
		}
		return PruneCreators(tx, to.Id)
	})
	if err != nil {
		return nil, internalError("Failed to move subscriptions", err)
	}
	return result, nil
}
