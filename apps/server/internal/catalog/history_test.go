package catalog

import (
	"fmt"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// The client writes history rows as {profile, video} with the relation to the videos record and nothing
// else: that is the one form a row has, and HistoryVideos resolves it for stats, notifications and exports.
func TestHistoryVideosResolveRowsThroughTheVideoRelation(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	user := testUser(t, app, "history@example.test")
	profile := testProfile(t, app, user, "Mine")
	sibling := testProfile(t, app, user, "Sibling")

	var rows []*core.Record
	row := func(profile *core.Record, video *core.Record) *core.Record {
		return testRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z", "duration_watched": 60})
	}
	first := mustFindVideoAfterStore(t, s, listing(alpha, "first"))
	second := mustFindVideoAfterStore(t, s, listing(solo, "second"))
	rows = append(rows, row(profile, first), row(profile, second), row(sibling, first))
	// A row whose video is gone has no entry, and a row without a video is skipped.
	gone := mustFindVideoAfterStore(t, s, listing(alpha, "gone"))
	rows = append(rows, row(profile, gone))
	if err := app.Delete(gone); err != nil {
		t.Fatal(err)
	}
	rows, err := app.FindRecordsByIds("watch_history", idsOf(rows))
	if err != nil {
		t.Fatal(err)
	}
	rows = append(rows, core.NewRecord(rows[0].Collection()))

	videos, err := s.HistoryVideos(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 || videos[first.Id] == nil || videos[second.Id] == nil || videos[gone.Id] != nil {
		t.Fatalf("resolved %d videos: %v", len(videos), videos)
	}
	if got := videos[second.Id]; got.GetString("platform") != solo || got.GetString("platform_id") != "second" {
		t.Fatalf("second = %v", got.FieldsData())
	}
	if videos, err := s.HistoryVideos(nil); err != nil || len(videos) != 0 {
		t.Fatalf("no rows: %v %v", videos, err)
	}
}

func TestHistoryVideosLoadsMoreRowsThanOneQueryHolds(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	user := testUser(t, app, "history-bulk@example.test")
	profile := testProfile(t, app, user, "Bulk")
	const count = queryBatch + queryBatch/2
	batch := make([]platform.Video, count)
	for i := range batch {
		batch[i] = platform.Video{ID: fmt.Sprintf("hist%03d", i), Platform: alpha, Title: "t"}
	}
	if err := s.StoreVideos(batch, ""); err != nil {
		t.Fatal(err)
	}
	rows := make([]*core.Record, 0, count+1)
	for _, v := range batch {
		rows = append(rows, testRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": v.RecordID, "watched_at": "2026-01-01 00:00:00.000Z"}))
	}
	// Two rows of one video count once.
	rows = append(rows, rows[0])
	videos, err := s.HistoryVideos(rows)
	if err != nil || len(videos) != count {
		t.Fatalf("resolved %d of %d: %v", len(videos), count, err)
	}
	for _, v := range batch {
		if videos[v.RecordID] == nil || videos[v.RecordID].GetString("platform_id") != v.ID {
			t.Fatalf("%s did not resolve", v.ID)
		}
	}
}

func mustFindVideoAfterStore(t *testing.T, s *Store, v platform.Video) *core.Record {
	t.Helper()
	stored := mustStore(t, s, v, "")
	record, err := s.Video(stored.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func idsOf(records []*core.Record) []string {
	ids := make([]string, len(records))
	for i, r := range records {
		ids[i] = r.Id
	}
	return ids
}
