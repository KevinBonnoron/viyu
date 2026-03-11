package catalog

import (
	"errors"
	"slices"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func TestEnsureCreatesPublicExpiredStubsAndFindsExistingRecords(t *testing.T) {
	t.Parallel()
	s, _, w := newStore(t)
	channel, err := s.EnsureChannel("", alpha, "@UCexport")
	if err != nil || channel == nil {
		t.Fatalf("channel stub: %v %v", channel, err)
	}
	video, err := s.EnsureVideo("", alpha, "exportvideo")
	if err != nil || video == nil {
		t.Fatalf("video stub: %v %v", video, err)
	}
	for _, record := range []*core.Record{channel, video} {
		if len(record.GetStringSlice("owners")) != 0 || record.GetDateTime("expires_at").Time().Year() >= 2000 {
			t.Errorf("a stub is public and expired: %+v", record.FieldsData())
		}
	}
	if w.profiles.Load() != 1 || w.publications.Load() != 1 {
		t.Fatalf("creating stubs must ask for their completion: %d %d", w.profiles.Load(), w.publications.Load())
	}
	again, err := s.EnsureVideo("", alpha, "exportvideo")
	if err != nil || again.Id != video.Id || w.publications.Load() != 1 {
		t.Fatalf("an existing record is found, not re-created: %v %v", again, err)
	}
	// A stored record is returned as it is: nothing the file says about it is written.
	stored := mustStore(t, s, platform.Video{ID: "known", Platform: alpha, Title: "Known"}, "")
	known, err := s.EnsureVideo("", alpha, "known")
	if err != nil || known.Id != stored.RecordID {
		t.Fatalf("known: %v %v", known, err)
	}
	if title, _ := TitlesOf(known); title != "Known" {
		t.Fatalf("title = %q", title)
	}
}

func TestEnsureRefusesWhatTheInstanceDoesNotAccept(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	owner := testUser(t, app, "ensure-owner@example.test")
	stranger := testUser(t, app, "ensure-stranger@example.test")
	for name, ref := range map[string]struct{ platform, id string }{
		"unknown platform": {"vimeo", "abc"},
		"invalid id":       {alpha, "not a valid id!"},
		"empty id":         {alpha, ""},
	} {
		if record, err := s.EnsureVideo("", ref.platform, ref.id); record != nil || err != nil {
			t.Errorf("%s: video %v %v", name, record, err)
		}
		if record, err := s.EnsureChannel("", ref.platform, ref.id); record != nil || err != nil {
			t.Errorf("%s: channel %v %v", name, record, err)
		}
	}
	if count, _ := app.CountRecords("videos"); count != 0 {
		t.Fatalf("a refused reference stored %d videos", count)
	}

	// A private record is not visible to another account: nothing is revealed and nothing is written.
	mustStore(t, s, listing(alpha, "private"), owner.Id)
	if record, err := s.EnsureVideo(stranger.Id, alpha, "private"); record != nil || err != nil {
		t.Fatalf("a private video was handed to a stranger: %v %v", record, err)
	}
	if record, err := s.EnsureChannel(stranger.Id, alpha, "UCprivate"); record != nil || err != nil {
		t.Fatalf("a private channel was handed to a stranger: %v %v", record, err)
	}
	if record, err := s.EnsureVideo(owner.Id, alpha, "private"); err != nil || record == nil {
		t.Fatalf("the owner must get its record: %v %v", record, err)
	}
	record := mustFindVideo(t, s, alpha, "private")
	if !slices.Equal(record.GetStringSlice("owners"), []string{owner.Id}) {
		t.Fatalf("owners = %v", record.GetStringSlice("owners"))
	}
}

func TestEnsureWorksInsideATransaction(t *testing.T) {
	t.Parallel()
	s, app, w := newStore(t)
	failed := errors.New("import failed")
	err := app.RunInTransaction(func(tx core.App) error {
		if _, err := s.In(tx).EnsureChannel("", alpha, "UCtx"); err != nil {
			return err
		}
		if _, err := s.In(tx).EnsureVideo("", alpha, "txvideo"); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatal(err)
	}
	if count, _ := app.CountRecords("channels"); count != 0 {
		t.Fatal("a rolled back import left a channel stub")
	}
	if w.profiles.Load() != 0 || w.publications.Load() != 0 {
		t.Fatal("a rolled back import woke the workers")
	}
	if err := app.RunInTransaction(func(tx core.App) error {
		_, err := s.In(tx).EnsureVideo("", alpha, "txvideo")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mustFindVideo(t, s, alpha, "txvideo")
	if w.publications.Load() != 1 {
		t.Fatalf("publications woken %d times", w.publications.Load())
	}
}
