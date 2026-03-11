package series

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	seriesCollection   = "series"
	videosCollection   = "videos"
	channelsCollection = "channels"

	// queryBatch bounds how many ids one IN query looks up.
	queryBatch = 200
)

// Store persists series as records of the series collection: a name, the channel they belong to and the
// relation to their videos. Series are public data derived from the public catalog, so the store knows
// nothing of accounts. It holds no state of its own and is safe for concurrent use.
type Store struct {
	app       core.App
	catalog   *catalog.Store
	platforms *platform.Registry
	parser    *Parser
}

// NewStore returns the store of app. The parser reads the titles of stored videos back into episodes;
// the registry gives the canonical form of a channel's native id.
func NewStore(app core.App, store *catalog.Store, platforms *platform.Registry, parser *Parser) *Store {
	return &Store{app: app, catalog: store, platforms: platforms, parser: parser}
}

// Save persists the series of one channel, all in one transaction, and gives each series the id of its
// record (and its channel's record id): the grouping hash Infer starts with is internal. A series that
// cannot be stored ends with an empty ID, and so does every series when Save fails.
//
// The videos of a series are stored by record id, found with batched queries; a video that is not stored is
// left out. A series record is written only when its set of videos changed, since every save is a realtime
// event pushed to all clients. catalogComplete says whether list was inferred from the channel's whole
// catalog: only then does the list tell which series no longer exist and which videos left one. An
// incomplete catalog only adds: it never removes a stored member nor a stored series.
func (s *Store) Save(channelRecordID string, catalogComplete bool, list []Series) error {
	ids := make([]string, len(list))
	err := s.app.RunInTransaction(func(tx core.App) error {
		clear(ids)
		return s.save(tx, channelRecordID, catalogComplete, list, ids)
	})
	for i := range list {
		list[i].ID = ""
		if err == nil {
			list[i].ID = ids[i]
			list[i].ChannelRecordID = channelRecordID
		}
	}
	return err
}

func (s *Store) save(tx core.App, channelRecordID string, catalogComplete bool, list []Series, ids []string) error {
	channel, err := tx.FindRecordById(channelsCollection, channelRecordID)
	if err != nil {
		return err
	}
	existing, err := tx.FindRecordsByFilter(seriesCollection, "channel = {:channel}", "id", 0, 0, dbx.Params{"channel": channel.Id})
	if err != nil {
		return err
	}
	byName := make(map[string]*core.Record, len(existing))
	for _, record := range existing {
		byName[record.GetString("name")] = record
	}
	records, err := videoRecordsOf(s.catalog.In(tx), list)
	if err != nil {
		return err
	}
	collection, err := tx.FindCollectionByNameOrId(seriesCollection)
	if err != nil {
		return err
	}
	kept := make(map[string]bool, len(list))
	for i, entry := range list {
		name := strings.TrimSpace(entry.Title)
		if name == "" {
			continue
		}
		kept[name] = true
		record := byName[name]
		curated := entry.Order == OrderCollection
		videoIDs := memberIDs(entry, records)
		isNew := record == nil
		if isNew {
			record = core.NewRecord(collection)
			record.Set("channel", channel.Id)
			record.Set("name", name)
		} else {
			stored := record.GetStringSlice("videos")
			if !catalogComplete {
				// A catalog that is still loading cannot say what a collection leaves out, nor in what
				// order: it never rewrites a series a collection ordered, and only adds to the others.
				if record.GetBool("curated") && !curated {
					ids[i] = record.Id
					continue
				}
				for _, id := range stored {
					if !slices.Contains(videoIDs, id) {
						videoIDs = append(videoIDs, id)
					}
				}
			}
			if record.GetBool("curated") == curated && sameVideos(stored, videoIDs, curated) {
				ids[i] = record.Id
				continue
			}
		}
		record.Set("videos", videoIDs)
		record.Set("curated", curated)
		if err := tx.Save(record); err != nil {
			return err
		}
		byName[name] = record
		ids[i] = record.Id
	}
	if catalogComplete {
		for name, record := range byName {
			if !kept[name] {
				if err := tx.Delete(record); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// MembershipMatches tells whether the stored series of a channel are exactly the expected ones, with the
// same videos each. The analysis worker uses it to see whether stored series are stale; a read never does.
func (s *Store) MembershipMatches(channelRecordID string, expected []Series) (bool, error) {
	var stored []*core.Record
	if err := s.app.RecordQuery(seriesCollection).AndWhere(dbx.HashExp{"channel": channelRecordID}).All(&stored); err != nil {
		return false, err
	}
	if len(stored) != len(expected) {
		return false, nil
	}
	byName := make(map[string]*core.Record, len(stored))
	for _, record := range stored {
		byName[record.GetString("name")] = record
	}
	records, err := videoRecordsOf(s.catalog, expected)
	if err != nil {
		return false, err
	}
	for _, entry := range expected {
		record := byName[strings.TrimSpace(entry.Title)]
		if record == nil || record.GetBool("curated") != (entry.Order == OrderCollection) || !sameVideos(record.GetStringSlice("videos"), memberIDs(entry, records), entry.Order == OrderCollection) {
			return false, nil
		}
	}
	return true, nil
}

// ForChannels reads the stored series of the channels, by channel record id, with their videos presented and
// ordered. It never infers anything: what was stored is what is answered, so reading is cheap however many
// channels there are (one query for the series, one batch of queries for the channels and one for the
// videos of all of them). A channel with no series has no entry. Episode numbers read from thumbnails are
// not part of storage: apply them with Parser.WithEpisodes.
func (s *Store) ForChannels(channelRecordIDs []string) (map[string][]Series, error) {
	out := map[string][]Series{}
	var records []*core.Record
	for chunk := range slices.Chunk(channelRecordIDs, queryBatch) {
		values := make([]any, len(chunk))
		for i, id := range chunk {
			values[i] = id
		}
		var found []*core.Record
		if err := s.app.RecordQuery(seriesCollection).AndWhere(dbx.In("channel", values...)).OrderBy("name ASC", "id ASC").All(&found); err != nil {
			return nil, loadFailed(err)
		}
		records = append(records, found...)
	}
	if len(records) == 0 {
		return out, nil
	}
	channels, err := s.channelRecords(records)
	if err != nil {
		return nil, loadFailed(err)
	}
	list, err := s.assemble(records, channels)
	if err != nil {
		return nil, loadFailed(err)
	}
	for _, entry := range list {
		out[entry.ChannelRecordID] = append(out[entry.ChannelRecordID], entry)
	}
	return out, nil
}

// Get reads one stored series by its record id, with its videos presented and ordered, and the record of
// its channel. The error is an *apierr.Error: NotFound when no series has the id.
func (s *Store) Get(seriesRecordID string) (Series, *core.Record, error) {
	record, err := s.app.FindRecordById(seriesCollection, seriesRecordID)
	if errors.Is(err, sql.ErrNoRows) {
		return Series{}, nil, apierr.NotFound("Series")
	}
	if err != nil {
		return Series{}, nil, loadFailed(err)
	}
	channel, err := s.app.FindRecordById(channelsCollection, record.GetString("channel"))
	if err != nil {
		return Series{}, nil, loadFailed(err)
	}
	list, err := s.assemble([]*core.Record{record}, map[string]*core.Record{channel.Id: channel})
	if err != nil {
		return Series{}, nil, loadFailed(err)
	}
	return list[0], channel, nil
}

func loadFailed(err error) error {
	return apierr.Internal("Failed to load series", err)
}

func (s *Store) channelRecords(records []*core.Record) (map[string]*core.Record, error) {
	var ids []string
	seen := map[string]bool{}
	for _, record := range records {
		if id := record.GetString("channel"); !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	channels := make(map[string]*core.Record, len(ids))
	for chunk := range slices.Chunk(ids, queryBatch) {
		found, err := s.app.FindRecordsByIds(channelsCollection, chunk)
		if err != nil {
			return nil, err
		}
		for _, channel := range found {
			channels[channel.Id] = channel
		}
	}
	return channels, nil
}

// assemble turns series records into series: every member video of all of them is loaded and presented
// once, the numbering is read back from the titles and each series is ordered and summarized. A member
// that no longer exists is left out.
func (s *Store) assemble(records []*core.Record, channels map[string]*core.Record) ([]Series, error) {
	var ids []string
	seen := map[string]bool{}
	for _, record := range records {
		for _, id := range record.GetStringSlice("videos") {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	var videoRecords []*core.Record
	for chunk := range slices.Chunk(ids, queryBatch) {
		found, err := s.app.FindRecordsByIds(videosCollection, chunk)
		if err != nil {
			return nil, err
		}
		videoRecords = append(videoRecords, found...)
	}
	videos := make(map[string]platform.Video, len(videoRecords))
	for _, video := range s.catalog.VideosOf(videoRecords) {
		videos[video.RecordID] = video
	}
	list := make([]Series, 0, len(records))
	for _, record := range records {
		channel := channels[record.GetString("channel")]
		if channel == nil {
			return nil, sql.ErrNoRows
		}
		entry := Series{
			Summary: Summary{
				ID:              record.Id,
				Title:           record.GetString("name"),
				ChannelID:       s.nativeChannelID(channel),
				ChannelRecordID: channel.Id,
				ChannelName:     channel.GetString("name"),
				Platform:        channel.GetString("platform"),
			},
			Items: []Item{},
		}
		for _, id := range record.GetStringSlice("videos") {
			video, ok := videos[id]
			if !ok {
				continue
			}
			item := Item{Video: video}
			if !record.GetBool("curated") {
				parsed, _ := s.parser.Parse(TitleOf(video))
				item.Episode, item.Season = parsed.Episode, parsed.Season
			}
			entry.Items = append(entry.Items, item)
		}
		if record.GetBool("curated") {
			// The order a collection gave is the stored order of the videos.
			entry.Order = OrderCollection
		} else {
			order(&entry)
		}
		summarize(&entry)
		list = append(list, entry)
	}
	return list, nil
}

// nativeChannelID is the canonical form of the channel's native id.
func (s *Store) nativeChannelID(channel *core.Record) string {
	id := channel.GetString("platform_id")
	if p, ok := s.platforms.Get(channel.GetString("platform")); ok {
		if aliases := p.ChannelIDAliases(id); len(aliases) > 0 {
			return aliases[0]
		}
	}
	return id
}

// videoRecordsOf finds the stored records of every video of the series, keyed platform+":"+native id: one
// batched query per platform and 200 videos.
func videoRecordsOf(store *catalog.Store, list []Series) (map[string]*core.Record, error) {
	native := map[string][]string{}
	seen := map[string]bool{}
	for _, entry := range list {
		for _, item := range entry.Items {
			key := episodeKey(item.Video)
			if item.Video.ID == "" || item.Video.Platform == "" || seen[key] {
				continue
			}
			seen[key] = true
			native[item.Video.Platform] = append(native[item.Video.Platform], item.Video.ID)
		}
	}
	records := make(map[string]*core.Record, len(seen))
	for platformID, ids := range native {
		found, err := store.VideoRecords(platformID, ids)
		if err != nil {
			return nil, err
		}
		for id, record := range found {
			records[platformID+":"+id] = record
		}
	}
	return records, nil
}

// memberIDs lists the record ids of the stored videos of a series, in order, each once.
func memberIDs(entry Series, records map[string]*core.Record) []string {
	ids := make([]string, 0, len(entry.Items))
	seen := make(map[string]bool, len(entry.Items))
	for _, item := range entry.Items {
		record := records[episodeKey(item.Video)]
		if record != nil && !seen[record.Id] {
			seen[record.Id] = true
			ids = append(ids, record.Id)
		}
	}
	return ids
}

// sameVideos tells whether two lists hold the same record ids, in the same order when ordered.
func sameVideos(a, b []string, ordered bool) bool {
	if ordered {
		return slices.Equal(a, b)
	}
	return sameMembers(a, b)
}

// sameMembers tells whether two lists hold the same record ids, whatever their order.
func sameMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
	}
	return true
}
