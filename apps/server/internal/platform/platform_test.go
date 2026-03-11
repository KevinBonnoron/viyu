package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSerializingAnUnstoredRecordFailsClosed(t *testing.T) {
	for name, value := range map[string]any{
		"video":        Video{ID: "native"},
		"channel":      Channel{ID: "native"},
		"card":         ChannelCard{ID: "native"},
		"owner":        Video{RecordID: "rec", ID: "native", ChannelID: "native-channel"},
		"search video": SearchPage{Results: []Result{{Video: &Video{ID: "native"}}}},
	} {
		if _, err := json.Marshal(value); !errors.Is(err, ErrNotStored) {
			t.Fatalf("%s: serialized without a record id: %v", name, err)
		}
	}
}

func TestSerializingSwapsNativeIDsForRecordIDs(t *testing.T) {
	video := Video{RecordID: "v1", ChannelRecordID: "c1", ID: "native", ChannelID: "native-channel", Platform: "p"}
	data, err := json.Marshal(video)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"native"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("native id leaked: %s", data)
		}
	}
	if !strings.Contains(string(data), `"id":"v1"`) || !strings.Contains(string(data), `"channelId":"c1"`) {
		t.Fatalf("record ids missing: %s", data)
	}
	ownerless, err := json.Marshal(Video{RecordID: "v2", ID: "native"})
	if err != nil || !strings.Contains(string(ownerless), `"id":"v2"`) {
		t.Fatalf("ownerless video: %s %v", ownerless, err)
	}
	page, err := json.Marshal(SearchPage{Results: []Result{{Video: &video}, {Channel: &ChannelCard{RecordID: "c2", ID: "x"}}}, Page: 1})
	if err != nil || !strings.Contains(string(page), `"results":[{`) || strings.Contains(string(page), `"x"`) {
		t.Fatalf("page: %s %v", page, err)
	}
}

func TestPublishedHelpers(t *testing.T) {
	day := time.Date(2024, 5, 6, 0, 0, 0, 0, time.UTC)
	if got := PublishedFromStored(day); got != "2024-05-06" {
		t.Fatalf("a midnight is a day: %q", got)
	}
	exact := day.Add(13*time.Hour + 5*time.Minute)
	if got := PublishedFromStored(exact); got != "2024-05-06T13:05:00Z" {
		t.Fatalf("exact time: %q", got)
	}
	if PublishedFromStored(time.Time{}) != "" {
		t.Fatal("zero is unknown")
	}
	if got := PublishedToStored("2024-05-06"); got != "2024-05-06 00:00:00.000Z" {
		t.Fatalf("stored day: %q", got)
	}
	if PublishedToStored("not a date") != "" {
		t.Fatal("garbage must be dropped")
	}
	if got := CarryPublished("2024-05-06", "2024-05-06T13:05:00Z"); got != "2024-05-06T13:05:00Z" {
		t.Fatalf("a day is refined by its exact time: %q", got)
	}
	if got := CarryPublished("2024-05-07", "2024-05-06T13:05:00Z"); got != "2024-05-07" {
		t.Fatalf("another day is kept: %q", got)
	}
	if got := CarryPublished("", "2024-05-06"); got != "2024-05-06" {
		t.Fatalf("a missing one is taken whole: %q", got)
	}
	if ComparePublished("2024-05-06", "2024-05-06T13:05:00Z") >= 0 || ComparePublished("2024-05-07", "2024-05-06T13:05:00Z") <= 0 {
		t.Fatal("publications must order by instant")
	}
	if StartOfDayStored(exact.Add(795*time.Millisecond)) != "2024-05-06 00:00:00.000Z" {
		t.Fatalf("start of day: %q", StartOfDayStored(exact.Add(795*time.Millisecond)))
	}
}

func TestChannelImagesRejectSitePagesAndArtwork(t *testing.T) {
	for _, bad := range []string{"", "https://www.youtube.com/x", "https://i.ytimg.com/vi/x/hq.jpg", "https://example.test", "/relative"} {
		if ChannelImageURL(bad) != "" {
			t.Fatalf("%q accepted", bad)
		}
	}
	if got := ChannelImageURL("//yt3.ggpht.com/a=s88"); got != "https://yt3.googleusercontent.com/a=s88" {
		t.Fatalf("got %q", got)
	}
}
