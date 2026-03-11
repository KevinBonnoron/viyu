package catalog

import (
	"slices"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// HistoryVideos loads the videos records the given history rows point at, by record id. A row is
// identified by (profile, video) and nothing else: this is the one place that resolves a row to its video,
// so stats, notifications and the account export all agree. The result is keyed by record id; a row whose
// video is gone has no entry. The ids are loaded in batches, not one query per row.
func (s *Store) HistoryVideos(entries []*core.Record) (map[string]*core.Record, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if id := entry.GetString("video"); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	found := make(map[string]*core.Record, len(ids))
	for chunk := range slices.Chunk(ids, queryBatch) {
		records, err := s.app.FindRecordsByIds(videos, chunk)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			found[record.Id] = record
		}
	}
	return found, nil
}
