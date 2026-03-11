package subscriptions

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"golang.org/x/text/unicode/norm"
)

// channelNameKey is a channel name reduced to its letters and digits, without case or accents. Two
// channels of one creator on different platforms rarely spell their name the same way otherwise
// ("ZeratoR" and "zerator", "Angèle" and "Angele").
func channelNameKey(name string) string {
	var key strings.Builder
	for _, r := range norm.NFKD.String(name) {
		if unicode.Is(unicode.M, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			key.WriteRune(unicode.ToLower(r))
		}
	}
	return key.String()
}

// nameSeparators split a channel name that bundles several names, such as "Yves Rougy | Yorzian" or
// "Name (Streams)".
var nameSeparators = regexp.MustCompile(`[|/•·–—()\[\]]|\s[-~:]\s`)

// minSegmentKeyLength keeps one or two letters of a bundled name from matching anything.
const minSegmentKeyLength = 3

// channelNameSegmentKeys returns the keys of the parts of a name that bundles several names, or nil for
// an ordinary name. A part only ever matches the whole name of another channel (never another part), so
// "Yves Rougy | Yorzian" on YouTube meets "Yorzian" on Twitch without merging two unrelated "X | Gaming".
func channelNameSegmentKeys(name string) []string {
	parts := nameSeparators.Split(name, -1)
	if len(parts) < 2 {
		return nil
	}
	seen := map[string]bool{}
	var keys []string
	for _, part := range parts {
		key := channelNameKey(part)
		if len([]rune(key)) >= minSegmentKeyLength && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// AutoGroup links the profile's subscriptions that are obviously one creator on several platforms: same
// channel name, exactly one channel per platform, and none already in a creator. A subscription the user
// separated (ungrouped, see Ungroup) is never touched, so what the user separated stays separated. It
// returns how many creators it created. All the creators are written in one transaction.
func (s *Service) AutoGroup(profileID string) (int, error) {
	created := 0
	err := s.app.RunInTransaction(func(tx core.App) error {
		created = 0
		subscriptions, err := tx.FindRecordsByFilter(subscriptionsCollection, "profile = {:profile} && creator = '' && ungrouped = false", "created", 0, 0, dbx.Params{"profile": profileID})
		if err != nil || len(subscriptions) < 2 {
			return err
		}
		ids := make([]string, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			ids = append(ids, subscription.GetString("channel"))
		}
		channels, err := recordsByID(tx, channelsCollection, ids)
		if err != nil {
			return err
		}
		buckets := map[string][]*core.Record{}
		platforms := map[string]string{}
		segments := map[string][]string{}
		for _, subscription := range subscriptions {
			channel := channels[subscription.GetString("channel")]
			if channel == nil {
				continue
			}
			name := channel.GetString("name")
			if key := channelNameKey(name); key != "" {
				buckets[key] = append(buckets[key], subscription)
				platforms[subscription.Id] = channel.GetString("platform")
				segments[subscription.Id] = channelNameSegmentKeys(name)
			}
		}
		// A bundled name also joins the bucket of each of its parts that is some other channel's whole name,
		// but only when that bucket has no channel of its platform yet: an exact match on the same name
		// always wins.
		for _, subscription := range subscriptions {
			for _, key := range segments[subscription.Id] {
				members, ok := buckets[key]
				if !ok {
					continue
				}
				free := true
				for _, member := range members {
					free = free && platforms[member.Id] != platforms[subscription.Id]
				}
				if free {
					buckets[key] = append(members, subscription)
				}
			}
		}
		keys := make([]string, 0, len(buckets))
		for key := range buckets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		grouped := map[string]bool{}
		for _, key := range keys {
			members := buckets[key]
			distinct := map[string]bool{}
			for _, member := range members {
				distinct[platforms[member.Id]] = true
			}
			if len(distinct) < 2 || len(distinct) != len(members) {
				continue
			}
			taken := false
			for _, member := range members {
				taken = taken || grouped[member.Id]
			}
			if taken {
				continue
			}
			creator, err := newCreator(tx, profileID, members[0])
			if err != nil {
				return err
			}
			for _, member := range members {
				member.Set("creator", creator.Id)
				grouped[member.Id] = true
				if err := tx.Save(member); err != nil {
					return err
				}
			}
			created++
		}
		return nil
	})
	if err != nil {
		return 0, internalError("Failed to group subscriptions", err)
	}
	return created, nil
}
