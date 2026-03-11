package catalog

import (
	"database/sql"
	"errors"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// liveVideo stores a video flagged live, with fresh full metadata.
func liveVideo(t *testing.T, s *Store, platformID, channelID, id string) *core.Record {
	t.Helper()
	mustStore(t, s, platform.Video{ID: id, Platform: platformID, Title: id, Live: true, ChannelID: channelID, ChannelName: channelID, FullMetadata: true}, "")
	return mustFindVideo(t, s, platformID, id)
}

func TestFinishedStreamBecomesAnOrdinaryVideo(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	ended := liveVideo(t, s, alpha, "@quiet", "endedstream1")
	running := liveVideo(t, s, alpha, "@onair", "livestream1")
	if !MetadataOf(ended).Fresh() {
		t.Fatal("fixture metadata must start fresh")
	}

	// The channel is stored under its handle; the platform lists the forms it may be found under.
	if err := s.EndLive(alpha, platform.LiveEnd{ChannelIDs: []string{"quiet", "@quiet"}}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := app.FindRecordById("videos", ended.Id)
	if err != nil || reloaded.GetBool("live") {
		t.Fatalf("the flag of a stream that ended must be cleared: %v", err)
	}
	if MetadataOf(reloaded).Fresh() {
		t.Fatal("its metadata must be expired so the next opening fetches the final details")
	}
	if still, err := app.FindRecordById("videos", running.Id); err != nil || !still.GetBool("live") || !MetadataOf(still).Fresh() {
		t.Fatalf("a stream in progress keeps its flag and metadata: %v", err)
	}
	// An unknown channel is not an error.
	if err := s.EndLive(alpha, platform.LiveEnd{ChannelIDs: []string{"nobody"}}); err != nil {
		t.Fatal(err)
	}
}

func TestFinishedStubStreamIsDeletedUnlessSomethingReferencesIt(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	owner := testUser(t, app, "liked-stream@example.test")
	profile := testProfile(t, app, owner, "Likes")

	liveVideo(t, s, solo, "quiet", "quiet")
	liveVideo(t, s, solo, "onair", "onair")
	liked := liveVideo(t, s, solo, "liked", "liked")
	testRecord(t, app, "video_reactions", map[string]any{"profile": profile.Id, "video": liked.Id, "reaction": "like"})
	watched := liveVideo(t, s, solo, "watched", "watched")
	testRecord(t, app, "watch_history", map[string]any{"profile": profile.Id, "video": watched.Id, "watched_at": "2026-01-01 00:00:00.000Z"})
	inSeries := liveVideo(t, s, solo, "series", "series")
	channel := mustFindChannel(t, s, solo, "series")
	testRecord(t, app, "series", map[string]any{"name": "Marathon", "channel": channel.Id, "videos": []string{inSeries.Id}})

	for _, login := range []string{"quiet", "liked", "watched", "series"} {
		if err := s.EndLive(solo, platform.LiveEnd{ChannelIDs: []string{login}, StubVideoID: login}); err != nil {
			t.Fatal(login, err)
		}
	}
	if _, err := s.FindVideo(solo, "quiet"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the record of a stream that ended is still listed: %v", err)
	}
	// What is referenced survives, still flagged: clients keep hiding it until it is really live again.
	for _, login := range []string{"liked", "watched", "series"} {
		if record, err := s.FindVideo(solo, login); err != nil || !record.GetBool("live") {
			t.Fatalf("%s: a referenced stream's record must survive, flagged: %v", login, err)
		}
	}
	if count, err := app.CountRecords("video_reactions"); err != nil || count != 1 {
		t.Fatalf("the like was lost: %d %v", count, err)
	}
	if count, err := app.CountRecords("watch_history"); err != nil || count != 1 {
		t.Fatalf("the history entry was lost: %d %v", count, err)
	}
	// A stream still in progress is left alone, and so is a stub that is not flagged.
	if record, err := s.FindVideo(solo, "onair"); err != nil || !record.GetBool("live") {
		t.Fatalf("a stream still in progress must keep its record and flag: %v", err)
	}
	mustStore(t, s, platform.Video{ID: "replay", Platform: solo, Title: "Replay", ChannelID: "replay", ChannelName: "r"}, "")
	if err := s.EndLive(solo, platform.LiveEnd{ChannelIDs: []string{"replay"}, StubVideoID: "replay"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindVideo(solo, "replay"); err != nil {
		t.Fatalf("a video that was never flagged live was deleted: %v", err)
	}
}

func TestEndingALiveStreamAlsoClearsTheOtherStreamVideosOfTheChannel(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	liveVideo(t, s, solo, "mixed", "mixed")
	other := liveVideo(t, s, solo, "mixed", "9001")
	if err := s.EndLive(solo, platform.LiveEnd{ChannelIDs: []string{"mixed"}, StubVideoID: "mixed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindVideo(solo, "mixed"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the stub was kept: %v", err)
	}
	reloaded, err := s.Video(other.Id)
	if err != nil || reloaded.GetBool("live") || MetadataOf(reloaded).Fresh() {
		t.Fatalf("another stream video of the channel must become an ordinary video: %v", err)
	}
}
