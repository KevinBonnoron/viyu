package catalog

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Stores of the same videos and channels from many goroutines yield exactly one record each and no error:
// there is no lock, the transaction and the unique indexes do the work.
func TestConcurrentStoresOfTheSameRecordsYieldOneRecordEach(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	first := testUser(t, app, "concurrent-first@example.test")
	second := testUser(t, app, "concurrent-second@example.test")
	const goroutines, videoCount, channelCount = 16, 12, 3

	var wg sync.WaitGroup
	failures := make(chan error, goroutines*3)
	recordIDs := make([][]string, goroutines)
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner := []string{"", first.Id, second.Id}[g%3]
			batch := make([]platform.Video, videoCount)
			for i := range batch {
				batch[i] = platform.Video{ID: fmt.Sprintf("video%02d", i), Platform: alpha, Title: "t", ChannelID: fmt.Sprintf("UC%d", i%channelCount), ChannelName: "Channel"}
			}
			if err := s.StoreVideos(batch, owner); err != nil {
				failures <- err
			}
			for _, v := range batch {
				recordIDs[g] = append(recordIDs[g], v.RecordID)
			}
			card := platform.Channel{ID: "UCcard", Platform: alpha, Name: "Card"}
			if err := s.StoreChannelProfile(&card, owner); err != nil {
				failures <- err
			}
			listed := platform.Video{ID: "single", Platform: alpha, Title: "Single", ChannelID: "UC0", ChannelName: "Channel"}
			if err := s.StoreVideo(&listed, owner); err != nil {
				failures <- err
			}
			recordIDs[g] = append(recordIDs[g], listed.RecordID, card.RecordID)
		}()
	}
	wg.Wait()
	close(failures)
	var errs []error
	for err := range failures {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}

	for collection, want := range map[string]int{"videos": videoCount + 1, "channels": channelCount + 1} {
		if count, err := app.CountRecords(collection); err != nil || count != int64(want) {
			t.Fatalf("%s: %d records (%v), want %d", collection, count, err, want)
		}
	}
	for g := 1; g < goroutines; g++ {
		for i := range recordIDs[0] {
			if recordIDs[g][i] == "" || recordIDs[g][i] != recordIDs[0][i] {
				t.Fatalf("goroutine %d got record %q for item %d, goroutine 0 got %q", g, recordIDs[g][i], i, recordIDs[0][i])
			}
		}
	}
	// A public fetch makes a record public for good, whatever order the connected ones ran in.
	for _, collection := range []string{"videos", "channels"} {
		records, err := app.FindAllRecords(collection)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			if owners := record.GetStringSlice("owners"); len(owners) != 0 {
				t.Fatalf("%s %s owners = %v", collection, record.GetString("platform_id"), owners)
			}
		}
	}
}

// An insert that loses the race against another one is retried as an update of the record that won.
func TestAnInsertThatLosesTheRaceBecomesAnUpdate(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	rival := false
	app.OnRecordCreateExecute("videos").BindFunc(func(e *core.RecordEvent) error {
		if !rival && e.Record.GetString("platform_id") == "contested" {
			rival = true
			// Another writer inserts the same video first.
			winner := core.NewRecord(e.Record.Collection())
			winner.Set("platform", alpha)
			winner.Set("platform_id", "contested")
			winner.Set("expires_at", expired())
			SetTitles(winner, "Winner", "Winner")
			if err := e.App.Save(winner); err != nil {
				return err
			}
		}
		return e.Next()
	})
	v := platform.Video{ID: "contested", Platform: alpha, Title: "Loser's title"}
	if err := s.StoreVideo(&v, ""); err != nil {
		t.Fatalf("the lost race must be retried: %v", err)
	}
	if count, err := app.CountRecords("videos"); err != nil || count != 1 {
		t.Fatalf("%d records (%v), want exactly one", count, err)
	}
	record := mustFindVideo(t, s, alpha, "contested")
	if v.RecordID != record.Id {
		t.Fatalf("the video reports %q, the record is %q", v.RecordID, record.Id)
	}
	if title, _ := TitlesOf(record); title != "Loser's title" {
		t.Fatalf("the retry did not update the winner: %q", title)
	}
}

func TestBatchesLargerThanOneTransactionAreAllStored(t *testing.T) {
	t.Parallel()
	s, app, _ := newStore(t)
	const count = 2*batchSize + 30
	batch := make([]platform.Video, count)
	for i := range batch {
		batch[i] = platform.Video{ID: fmt.Sprintf("big%03d", i), Platform: alpha, Title: "t", ChannelID: fmt.Sprintf("UCbig%d", i%7), ChannelName: "c"}
	}
	if err := s.StoreVideos(batch, ""); err != nil {
		t.Fatal(err)
	}
	for _, v := range batch {
		if v.RecordID == "" || v.ChannelRecordID == "" {
			t.Fatalf("%s has no record ids", v.ID)
		}
	}
	if n, err := app.CountRecords("videos"); err != nil || n != count {
		t.Fatalf("%d videos (%v)", n, err)
	}
	if n, err := app.CountRecords("channels"); err != nil || n != 7 {
		t.Fatalf("%d channels (%v)", n, err)
	}
}

// A store bound to a transaction commits or rolls back with it, and wakes the workers only on commit.
func TestAStoreBoundToATransactionFollowsIt(t *testing.T) {
	t.Parallel()
	s, app, w := newStore(t)
	rolledBack := errors.New("rolled back")
	err := app.RunInTransaction(func(tx core.App) error {
		v := listing(alpha, "doomed")
		if err := s.In(tx).StoreVideo(&v, ""); err != nil {
			return err
		}
		if _, err := tx.FindRecordById("videos", v.RecordID); err != nil {
			return err
		}
		return rolledBack
	})
	if !errors.Is(err, rolledBack) {
		t.Fatal(err)
	}
	if _, err := s.FindVideo(alpha, "doomed"); err == nil {
		t.Fatal("the video outlived its transaction")
	}
	if w.profiles.Load() != 0 || w.publications.Load() != 0 {
		t.Fatalf("a rolled back write woke the workers: %d %d", w.profiles.Load(), w.publications.Load())
	}

	if err := app.RunInTransaction(func(tx core.App) error {
		v := listing(alpha, "kept")
		if err := s.In(tx).StoreVideo(&v, ""); err != nil {
			return err
		}
		if w.profiles.Load() != 0 || w.publications.Load() != 0 {
			t.Error("the workers were woken before the commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mustFindVideo(t, s, alpha, "kept")
	if w.profiles.Load() == 0 || w.publications.Load() == 0 {
		t.Fatalf("the commit did not wake the workers: %d %d", w.profiles.Load(), w.publications.Load())
	}
}
