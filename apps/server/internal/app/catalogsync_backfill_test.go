package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
)

// csyncBackfillScript is a yt-dlp stand-in for a channel whose listing leaves out what the background work
// completes: the listing of a channel carries one undated video and no avatar, the single-entry lookup of the
// profile backfill carries the avatar, and the page of a video carries its publication date.
func csyncBackfillScript(channel string) string {
	return fmt.Sprintf(`case "$*" in
 *--dump-single-json*)
  case "$*" in
   *'--playlist-end 1 '*)
    printf '%%s\n' '{"_type":"playlist","id":"%[1]s","channel_id":"%[1]s","channel":"Subscribed","thumbnails":[{"id":"avatar_uncropped","url":"https://yt3.googleusercontent.com/fixture-avatar=s0"}],"entries":[{"id":"listed00001","title":"Listed","channel_id":"%[1]s","channel":"Subscribed"}]}' ;;
   *)
    printf '%%s\n' '{"_type":"playlist","id":"%[1]s","channel_id":"%[1]s","channel":"Subscribed","entries":[{"id":"listed00001","title":"Listed","channel_id":"%[1]s","channel":"Subscribed"}]}' ;;
  esac
  exit 0 ;;
esac
for last; do :; done
id=${last##*v=}
printf '{"id":"%%s","title":"Full title","fulltitle":"Full title","channel_id":"%[1]s","channel":"Subscribed","upload_date":"20260304","timestamp":1772606400,"duration":10}\n' "$id"`, channel)
}

// Listing a channel leaves its profile and its videos' dates to the background workers: the store wakes them
// (the application wires the wake-ups, nobody asks the workers), and what they learn lands on the same
// records. Both workers wait an hour before their first pass, so only the wake-up can have run them.
func TestListingsWakeTheBackfillsThatCompleteChannelsAndVideos(t *testing.T) {
	t.Parallel()
	native := serChannel("subscribed")
	app, handler := newApp(t, withDownloader(t, csyncBackfillScript(native)), func(o *Options) {
		o.DisableChannelProfiles = false
		o.Jobs.Profiles.StartDelay = time.Hour
		o.Jobs.Publications.StartDelay = time.Hour
	})
	channelID := storedChannel(t, app, testPlatforms().Default().ID(), native, "Subscribed")

	res := request(t, handler, "/api/channel/"+channelID, "")
	acctExpect(t, res, http.StatusOK, "")
	if strings.Contains(res.Body.String(), "fixture-avatar") || strings.Contains(res.Body.String(), "2026-03-04") {
		t.Fatalf("the listing already had what the background work is to find: %s", res.Body)
	}

	waitFor(t, "the avatar found by the profile backfill", func() bool {
		record, err := app.FindRecordById("channels", channelID)
		return err == nil && strings.Contains(record.GetString("thumbnail"), "fixture-avatar")
	})
	waitFor(t, "the publication date found by the publication backfill", func() bool {
		record, err := newStore(app).FindVideo(testPlatforms().Default().ID(), "listed00001")
		return err == nil && catalog.PublishedOf(record) == "2026-03-04T06:40:00Z"
	})
	record, err := newStore(app).FindVideo(testPlatforms().Default().ID(), "listed00001")
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := catalog.TitlesOf(record); title != "Full title" || !catalog.MetadataOf(record).OriginalTitleVerified {
		t.Fatalf("the video did not get its full metadata through the normal path: %q %+v", title, catalog.MetadataOf(record))
	}
}
