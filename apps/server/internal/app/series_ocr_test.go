package app

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
)

// serReader is the recognition engine of a test: the episode number of a thumbnail is the name of its file
// ("https://example.test/thumbs/3.jpg" shows episode 3). The engine itself is tested in series/ocr.
type serReader struct{ reads atomic.Int32 }

func (r *serReader) Episode(_ context.Context, thumbnails []string) (int, error) {
	r.reads.Add(1)
	if len(thumbnails) == 0 {
		return 0, fmt.Errorf("no thumbnail")
	}
	episode, err := strconv.Atoi(strings.TrimSuffix(path.Base(thumbnails[0]), ".jpg"))
	if err != nil {
		return 0, err
	}
	return episode, nil
}

// Episode numbers read from thumbnails are part of the series answers only while an administrator allows it:
// turning the setting off drops them at once, turning it on reads them again.
func TestSeriesEpisodesAreReadFromThumbnailsOnlyWhileTheAdministratorAllowsIt(t *testing.T) {
	t.Parallel()
	reader := &serReader{}
	app, handler := newApp(t, serQuickAnalysis, serNoDownloader(t), func(o *Options) { o.OCR = reader })
	_, admin := admUser(t, app, "series-ocr-admin@example.test", "admin")
	owner := testUser(t, app, "series-ocr@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	channel, _ := serSeedCatalog(t, app, serChannel("ocr"), "Outer Wilds #1", "Outer Wilds #2", "Alone | Outer Wilds")
	serSubscribe(t, app, profile, channel)

	// episodes is the episode of each video of the one series, in the order of the series; nil when none.
	episodes := func() []*float64 {
		list := serList(t, handler, token, profile.Id, "")
		if len(list.Series) != 1 {
			return nil
		}
		detail := serGet(t, handler, token, list.Series[0].ID, profile.Id)
		numbers := make([]*float64, len(detail.Series.Items))
		for i, item := range detail.Series.Items {
			numbers[i] = item.Episode
		}
		return numbers
	}
	unnumbered := func(numbers []*float64) int {
		count := 0
		for _, n := range numbers {
			if n == nil {
				count++
			}
		}
		return count
	}
	waitFor(t, "the series", func() bool { return len(episodes()) == 3 })
	waitFor(t, "the episode read from the thumbnail", func() bool { return unnumbered(episodes()) == 0 })
	numbers := episodes()
	if *numbers[0] != 1 || *numbers[1] != 2 || *numbers[2] != 3 {
		t.Fatalf("episodes=%v %v %v, want 1 2 3 (the third one read from its thumbnail)", *numbers[0], *numbers[1], *numbers[2])
	}
	if reads := reader.reads.Load(); reads != 1 {
		t.Fatalf("only the video whose title has no number is read: %d readings", reads)
	}

	disabled := send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":false}`)
	if disabled.Code != http.StatusOK || disabled.Body.String() != `{"enabled":false,"available":true}` {
		t.Fatalf("disable: %d %s", disabled.Code, disabled.Body)
	}
	if got := unnumbered(episodes()); got != 1 {
		t.Fatalf("%d videos are unnumbered after the setting was turned off, want 1: the thumbnail reading must go", got)
	}
	if answer := serList(t, handler, token, profile.Id, ""); answer.Analysis.OCR != (jobs.Counts{}) {
		t.Fatalf("the readings survived the setting: %+v", answer.Analysis.OCR)
	}

	enabled := send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":true}`)
	if enabled.Code != http.StatusOK || enabled.Body.String() != `{"enabled":true,"available":true}` {
		t.Fatalf("enable: %d %s", enabled.Code, enabled.Body)
	}
	waitFor(t, "the episodes read again", func() bool { return unnumbered(episodes()) == 0 })
	if reads := reader.reads.Load(); reads != 2 {
		t.Fatalf("enabling the setting must read what was skipped once more: %d readings", reads)
	}
}
