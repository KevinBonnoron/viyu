package accounts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
)

const (
	// ExportFormat and ExportVersion identify the file an export writes and an import accepts.
	ExportFormat  = "viyu-export"
	ExportVersion = 1
	// ImportMaxBytes is the largest export file an import reads.
	ImportMaxBytes = 8 << 20
	// importMaxEntries caps every section of an import, importMaxProfiles the profiles an account may end
	// up with: an import is written in one transaction and must stay bounded.
	importMaxEntries  = 5000
	importMaxProfiles = 100
	// batchSize bounds an IN query.
	batchSize = 500
)

// Account transfers reference channels and videos by their provider keys ({platform, platformId}), not by
// record ids, so an export produced on one instance resolves on another. The shared records an import needs
// are public stubs: expired, without name, title or picture (an import never writes what the file says
// about a channel or a video into a record every account reads), and the background refresh re-hydrates
// them.
type Export struct {
	Format     string     `json:"format"`
	Version    int        `json:"version"`
	ExportedAt time.Time  `json:"exportedAt"`
	Data       ExportData `json:"data"`
}

// Filename is the name a download of the export should carry.
func (e *Export) Filename() string {
	return "viyu-export-" + e.ExportedAt.UTC().Format("2006-01-02") + ".json"
}

// ExportData is what an account carries from one instance to another.
type ExportData struct {
	Settings      *ExportSettings  `json:"settings,omitempty"`
	Profiles      []string         `json:"profiles"`
	Subscriptions []ExportSub      `json:"subscriptions,omitempty"`
	Reactions     []ExportReaction `json:"reactions,omitempty"`
	History       []ExportHistory  `json:"history,omitempty"`
	// ProfileAppearance is keyed by profile name. It is optional so exports from before profiles
	// had an appearance still import, and only newly created profiles take it.
	ProfileAppearance map[string]ExportAppearance `json:"profileAppearance,omitempty"`
	// Sources is the account's choice of sources, keyed by platform. It holds only the choices made, is
	// optional for the same reason, and platforms this instance does not know are skipped on import.
	Sources map[string]bool `json:"sources,omitempty"`
}

// ExportAppearance is the icon and theme of a profile.
type ExportAppearance struct {
	Icon  string `json:"icon,omitempty"`
	Theme string `json:"theme,omitempty"`
}

// ExportSettings are the account-wide viewing settings.
type ExportSettings struct {
	HideShorts         bool   `json:"hideShorts"`
	HideWatched        bool   `json:"hideWatched"`
	HideSubscriberOnly bool   `json:"hideSubscriberOnly"`
	UseOriginalTitles  bool   `json:"useOriginalTitles"`
	PlaybackQuality    string `json:"playbackQuality"`
	AutoSkipSponsors   bool   `json:"autoSkipSponsors"`
	Autoplay           bool   `json:"autoplay"`
}

// ExportChannel names a channel by its provider key.
type ExportChannel struct {
	Platform   string `json:"platform"`
	PlatformID string `json:"platformId"`
}

// ExportVideo names a video by its provider key.
type ExportVideo struct {
	Platform   string `json:"platform"`
	PlatformID string `json:"platformId"`
}

// ExportCreator is the creator a subscription belongs to: Key names it within the profile (two
// subscriptions of a profile with one key share a creator), Name is what it is called.
type ExportCreator struct {
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
}

// ExportSub is one profile's subscription.
type ExportSub struct {
	Profile   string         `json:"profile"`
	Channel   ExportChannel  `json:"channel"`
	Creator   *ExportCreator `json:"creator,omitempty"`
	Ungrouped bool           `json:"ungrouped,omitempty"`
}

// ExportReaction is one profile's reaction to a video.
type ExportReaction struct {
	Profile  string      `json:"profile"`
	Video    ExportVideo `json:"video"`
	Reaction string      `json:"reaction"`
}

// ExportHistory is one profile's watch progress of a video.
type ExportHistory struct {
	Profile         string      `json:"profile"`
	Video           ExportVideo `json:"video"`
	WatchedAt       string      `json:"watchedAt"`
	DurationWatched float64     `json:"durationWatched"`
	VideoDuration   float64     `json:"videoDuration,omitempty"`
	Position        float64     `json:"position"`
	Watched         bool        `json:"watched"`
}

// ParseExport decodes the file of an export. A file that is not one answers INVALID_REQUEST.
func ParseExport(body []byte) (*Export, error) {
	var payload Export
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, notAnExport()
	}
	if err := payload.check(); err != nil {
		return nil, err
	}
	return &payload, nil
}

func notAnExport() *apierr.Error { return apierr.Invalid("This file is not a Viyu export.") }

func (e *Export) check() error {
	if e == nil || e.Format != ExportFormat || e.Version != ExportVersion {
		return notAnExport()
	}
	return nil
}

// recordsByID loads the records of collection with the given ids, in batches.
func recordsByID(app core.App, collection string, ids []string) (map[string]*core.Record, error) {
	records := make(map[string]*core.Record, len(ids))
	for chunk := range slices.Chunk(ids, batchSize) {
		batch, err := app.FindRecordsByIds(collection, chunk)
		if err != nil {
			return nil, err
		}
		for _, record := range batch {
			records[record.Id] = record
		}
	}
	return records, nil
}

// relationIDs lists the distinct, non-empty values of a relation field of rows.
func relationIDs(rows []*core.Record, relation string) []string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := row.GetString(relation); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// Export gathers everything the account carries: settings, source choices, profiles with their
// subscriptions, reactions and history. Videos and channels are named by their provider keys.
func (s *Service) Export(userID string) (*Export, error) {
	data, err := s.exportData(userID)
	if err != nil {
		return nil, internal("Failed to export account data", err)
	}
	return &Export{Format: ExportFormat, Version: ExportVersion, ExportedAt: time.Now().UTC(), Data: *data}, nil
}

func (s *Service) exportData(userID string) (*ExportData, error) {
	data := &ExportData{Profiles: []string{}}
	params := dbx.Params{"user": userID}

	if record, err := s.app.FindFirstRecordByFilter("settings", "user = {:user}", params); err == nil {
		data.Settings = &ExportSettings{
			HideShorts:         record.GetBool("hide_shorts"),
			HideWatched:        record.GetBool("hide_watched"),
			HideSubscriberOnly: record.GetBool("hide_subscriber_only"),
			UseOriginalTitles:  record.GetBool("use_original_titles"),
			PlaybackQuality:    record.GetString("playback_quality"),
			AutoSkipSponsors:   record.GetBool("auto_skip_sponsors"),
			Autoplay:           record.GetBool("autoplay"),
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	activations, err := s.app.FindRecordsByFilter(sources.ActivationsCollection, "user = {:user}", "platform", 0, 0, params)
	if err != nil {
		return nil, err
	}
	for _, activation := range activations {
		if data.Sources == nil {
			data.Sources = map[string]bool{}
		}
		data.Sources[activation.GetString("platform")] = activation.GetBool("enabled")
	}

	profiles, err := s.app.FindRecordsByFilter("profiles", "user = {:user}", "name", 0, 0, params)
	if err != nil {
		return nil, err
	}
	profileNames := make(map[string]string, len(profiles))
	for _, profile := range profiles {
		name := profile.GetString("name")
		profileNames[profile.Id] = name
		data.Profiles = append(data.Profiles, name)
		if icon, theme := profile.GetString("icon"), profile.GetString("theme"); icon != "" || theme != "" {
			if data.ProfileAppearance == nil {
				data.ProfileAppearance = map[string]ExportAppearance{}
			}
			data.ProfileAppearance[name] = ExportAppearance{Icon: icon, Theme: theme}
		}
	}

	subs, err := s.app.FindRecordsByFilter("subscriptions", "profile.user = {:user}", "", 0, 0, params)
	if err != nil {
		return nil, err
	}
	channels, err := recordsByID(s.app, "channels", relationIDs(subs, "channel"))
	if err != nil {
		return nil, err
	}
	creators, err := recordsByID(s.app, "creators", relationIDs(subs, "creator"))
	if err != nil {
		return nil, err
	}
	for _, sub := range subs {
		channel := channels[sub.GetString("channel")]
		if channel == nil {
			continue
		}
		exported := ExportSub{
			Profile:   profileNames[sub.GetString("profile")],
			Channel:   ExportChannel{Platform: channel.GetString("platform"), PlatformID: channel.GetString("platform_id")},
			Ungrouped: sub.GetBool("ungrouped"),
		}
		if creator := creators[sub.GetString("creator")]; creator != nil {
			exported.Creator = &ExportCreator{Key: creator.Id, Name: creator.GetString("name")}
		}
		data.Subscriptions = append(data.Subscriptions, exported)
	}

	reactions, err := s.app.FindRecordsByFilter("video_reactions", "profile.user = {:user}", "", 0, 0, params)
	if err != nil {
		return nil, err
	}
	reactionVideos, err := s.store.HistoryVideos(reactions)
	if err != nil {
		return nil, err
	}
	for _, reaction := range reactions {
		video := reactionVideos[reaction.GetString("video")]
		if video == nil {
			continue
		}
		data.Reactions = append(data.Reactions, ExportReaction{
			Profile:  profileNames[reaction.GetString("profile")],
			Video:    ExportVideo{Platform: video.GetString("platform"), PlatformID: video.GetString("platform_id")},
			Reaction: reaction.GetString("reaction"),
		})
	}

	history, err := s.app.FindRecordsByFilter("watch_history", "profile.user = {:user}", "", 0, 0, params)
	if err != nil {
		return nil, err
	}
	videos, err := s.store.HistoryVideos(history)
	if err != nil {
		return nil, err
	}
	for _, entry := range history {
		video := videos[entry.GetString("video")]
		if video == nil {
			continue
		}
		data.History = append(data.History, ExportHistory{
			Profile:         profileNames[entry.GetString("profile")],
			Video:           ExportVideo{Platform: video.GetString("platform"), PlatformID: video.GetString("platform_id")},
			WatchedAt:       entry.GetString("watched_at"),
			DurationWatched: entry.GetFloat("duration_watched"),
			VideoDuration:   entry.GetFloat("video_duration"),
			Position:        entry.GetFloat("position"),
			Watched:         entry.GetBool("watched"),
		})
	}
	return data, nil
}

var profileIconPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// applyProfileAppearance copies the icon and theme an export carries, skipping values this
// instance would reject so a stale or hand-edited export cannot fail the whole import.
func applyProfileAppearance(profile *core.Record, appearance ExportAppearance) {
	if profileIconPattern.MatchString(appearance.Icon) {
		profile.Set("icon", appearance.Icon)
	}
	if themes, ok := profile.Collection().Fields.GetByName("theme").(*core.SelectField); ok && slices.Contains(themes.Values, appearance.Theme) {
		profile.Set("theme", appearance.Theme)
	}
}

// validateImport refuses a file that holds more than an import may write.
func validateImport(data *ExportData) error {
	if len(data.Profiles) > importMaxProfiles {
		return apierr.Invalid(fmt.Sprintf("The export holds too many profiles (at most %d).", importMaxProfiles))
	}
	for _, section := range []struct {
		name string
		size int
	}{{"subscriptions", len(data.Subscriptions)}, {"reactions", len(data.Reactions)}, {"history entries", len(data.History)}} {
		if section.size > importMaxEntries {
			return apierr.Invalid(fmt.Sprintf("The export holds too many %s (at most %d).", section.name, importMaxEntries))
		}
	}
	return nil
}

// Import merges the export into the account in one transaction: either all of it lands or none of it. An
// entry naming a platform this instance does not know, or an identifier the platform would not accept, is
// skipped and counted under "skipped"; the counts of everything else are what was newly created. A file
// the import cannot take (wrong format, too large, a nameless profile) answers INVALID_REQUEST and writes
// nothing.
func (s *Service) Import(userID string, data *Export) (map[string]int, error) {
	if err := data.check(); err != nil {
		return nil, err
	}
	if err := validateImport(&data.Data); err != nil {
		return nil, err
	}
	counts := map[string]int{"profiles": 0, "subscriptions": 0, "reactions": 0, "history": 0, "skipped": 0}
	err := s.app.RunInTransaction(func(tx core.App) error {
		return s.merge(tx, userID, &data.Data, counts)
	})
	if err != nil {
		var safe *apierr.Error
		if errors.As(err, &safe) {
			return nil, safe
		}
		return nil, internal("Failed to import account data", err)
	}
	return counts, nil
}

// merge writes the export through tx. The stubs it creates are found or created by the catalog store
// bound to the same transaction, which wakes the backfill workers once it committed.
func (s *Service) merge(tx core.App, userID string, data *ExportData, counts map[string]int) error {
	store := s.store.In(tx)

	profiles, err := tx.FindRecordsByFilter("profiles", "user = {:user}", "", 0, 0, dbx.Params{"user": userID})
	if err != nil {
		return err
	}
	profileByName := make(map[string]*core.Record, len(profiles))
	for _, profile := range profiles {
		profileByName[profile.GetString("name")] = profile
	}
	ensureProfile := func(name string) (*core.Record, error) {
		name = truncateRunes(strings.TrimSpace(name), maxProfileNameLength)
		if name == "" {
			return nil, apierr.Invalid("A profile name is required.")
		}
		if profile := profileByName[name]; profile != nil {
			return profile, nil
		}
		if len(profileByName) >= importMaxProfiles {
			return nil, apierr.Invalid(fmt.Sprintf("The export would give the account more than %d profiles.", importMaxProfiles))
		}
		profilesCollection, err := tx.FindCollectionByNameOrId("profiles")
		if err != nil {
			return nil, err
		}
		profile := core.NewRecord(profilesCollection)
		profile.Set("user", userID)
		profile.Set("name", name)
		applyProfileAppearance(profile, data.ProfileAppearance[name])
		if err := tx.Save(profile); err != nil {
			return nil, err
		}
		profileByName[name] = profile
		counts["profiles"]++
		return profile, nil
	}
	for _, name := range data.Profiles {
		if _, err := ensureProfile(name); err != nil {
			return err
		}
	}

	if data.Settings != nil {
		settings, err := tx.FindFirstRecordByFilter("settings", "user = {:user}", dbx.Params{"user": userID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			settingsCollection, err := tx.FindCollectionByNameOrId("settings")
			if err != nil {
				return err
			}
			settings = core.NewRecord(settingsCollection)
			settings.Set("user", userID)
		}
		settings.Set("hide_shorts", data.Settings.HideShorts)
		settings.Set("hide_watched", data.Settings.HideWatched)
		settings.Set("hide_subscriber_only", data.Settings.HideSubscriberOnly)
		settings.Set("use_original_titles", data.Settings.UseOriginalTitles)
		settings.Set("playback_quality", data.Settings.PlaybackQuality)
		settings.Set("auto_skip_sponsors", data.Settings.AutoSkipSponsors)
		settings.Set("autoplay", data.Settings.Autoplay)
		if err := tx.Save(settings); err != nil {
			return err
		}
	}

	for platformID, enabled := range data.Sources {
		if _, known := s.platforms.Get(platformID); !known {
			continue
		}
		if err := sources.WriteActivation(tx, userID, platformID, enabled); err != nil {
			return err
		}
	}

	// A reference is resolved once per import, whatever the number of entries that name it.
	channels := newStubs(func(ref ExportChannel) (*core.Record, error) {
		return store.EnsureChannel(userID, ref.Platform, ref.PlatformID)
	})
	videos := newStubs(func(ref ExportVideo) (*core.Record, error) {
		return store.EnsureVideo(userID, ref.Platform, ref.PlatformID)
	})

	// The creators of the export become the creators of the profiles, one per key, stored when first used.
	imported := map[[2]string]string{}
	creatorOf := func(profileID string, ref *ExportCreator) (string, error) {
		if ref == nil || strings.TrimSpace(ref.Key) == "" {
			return "", nil
		}
		key := [2]string{profileID, ref.Key}
		if id, done := imported[key]; done {
			return id, nil
		}
		creator, err := subscriptions.StoreCreator(tx, profileID, ref.Name)
		if err != nil {
			return "", err
		}
		imported[key] = creator.Id
		return creator.Id, nil
	}

	for _, sub := range data.Subscriptions {
		profile, err := ensureProfile(sub.Profile)
		if err != nil {
			return err
		}
		channel, err := channels.get(sub.Channel)
		if err != nil {
			return err
		}
		if channel == nil {
			counts["skipped"]++
			continue
		}
		existing, err := tx.FindFirstRecordByFilter("subscriptions", "profile = {:profile} && channel = {:channel}", dbx.Params{"profile": profile.Id, "channel": channel.Id})
		if err == nil {
			if existing.GetString("creator") == "" {
				creator, err := creatorOf(profile.Id, sub.Creator)
				if err != nil {
					return err
				}
				if creator != "" {
					existing.Set("creator", creator)
					existing.Set("ungrouped", false)
					if err := tx.Save(existing); err != nil {
						return err
					}
				}
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		subscriptionsCollection, err := tx.FindCollectionByNameOrId("subscriptions")
		if err != nil {
			return err
		}
		creator, err := creatorOf(profile.Id, sub.Creator)
		if err != nil {
			return err
		}
		record := core.NewRecord(subscriptionsCollection)
		record.Set("profile", profile.Id)
		record.Set("channel", channel.Id)
		record.Set("creator", creator)
		record.Set("ungrouped", sub.Ungrouped && creator == "")
		if err := tx.Save(record); err != nil {
			return err
		}
		counts["subscriptions"]++
	}
	// A creator is a link between channels: one whose other channels were skipped is not kept.
	for _, profile := range profileByName {
		if err := subscriptions.PruneCreators(tx, profile.Id); err != nil {
			return err
		}
	}

	for _, reaction := range data.Reactions {
		if reaction.Reaction != "like" && reaction.Reaction != "dislike" && reaction.Reaction != "not_interested" {
			counts["skipped"]++
			continue
		}
		profile, err := ensureProfile(reaction.Profile)
		if err != nil {
			return err
		}
		video, err := videos.get(reaction.Video)
		if err != nil {
			return err
		}
		if video == nil {
			counts["skipped"]++
			continue
		}
		existing, err := tx.FindFirstRecordByFilter("video_reactions", "profile = {:profile} && video = {:video}", dbx.Params{"profile": profile.Id, "video": video.Id})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			reactionsCollection, err := tx.FindCollectionByNameOrId("video_reactions")
			if err != nil {
				return err
			}
			existing = core.NewRecord(reactionsCollection)
			existing.Set("profile", profile.Id)
			existing.Set("video", video.Id)
			counts["reactions"]++
		}
		existing.Set("reaction", reaction.Reaction)
		if err := tx.Save(existing); err != nil {
			return err
		}
	}

	for _, entry := range data.History {
		watchedAt, err := types.ParseDateTime(entry.WatchedAt)
		if err != nil || watchedAt.IsZero() {
			counts["skipped"]++
			continue
		}
		profile, err := ensureProfile(entry.Profile)
		if err != nil {
			return err
		}
		video, err := videos.get(entry.Video)
		if err != nil {
			return err
		}
		if video == nil {
			counts["skipped"]++
			continue
		}
		existing, err := tx.FindFirstRecordByFilter("watch_history", "profile = {:profile} && video = {:video}", dbx.Params{"profile": profile.Id, "video": video.Id})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			historyCollection, err := tx.FindCollectionByNameOrId("watch_history")
			if err != nil {
				return err
			}
			existing = core.NewRecord(historyCollection)
			existing.Set("profile", profile.Id)
			existing.Set("video", video.Id)
			counts["history"]++
		}
		existing.Set("watched_at", watchedAt)
		existing.Set("duration_watched", max(entry.DurationWatched, 0))
		if entry.VideoDuration > 0 {
			existing.Set("video_duration", entry.VideoDuration)
		}
		existing.Set("position", max(entry.Position, 0))
		existing.Set("watched", entry.Watched)
		if err := tx.Save(existing); err != nil {
			return err
		}
	}
	return nil
}

// stubs resolves the references of an import to records, once each; a reference this instance refuses
// resolves to nil.
type stubs[K comparable] struct {
	resolve func(K) (*core.Record, error)
	found   map[K]*core.Record
}

func newStubs[K comparable](resolve func(K) (*core.Record, error)) *stubs[K] {
	return &stubs[K]{resolve: resolve, found: map[K]*core.Record{}}
}

func (s *stubs[K]) get(ref K) (*core.Record, error) {
	if record, done := s.found[ref]; done {
		return record, nil
	}
	record, err := s.resolve(ref)
	if err != nil {
		return nil, err
	}
	s.found[ref] = record
	return record, nil
}
