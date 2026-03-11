package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
)

// serTwitchVOD is a video a Twitch channel exposes in the fixture.
type serTwitchVOD struct{ id, title string }

// serTwitchChannel is a Twitch GraphQL fixture that lists the VODs a test puts on a channel; a test removes
// one the way Twitch lets a VOD expire. Its pages are small so that the catalog has to follow the cursor.
type serTwitchChannel struct {
	mu       sync.Mutex
	vods     []serTwitchVOD
	marathon []string // the video ids of the channel's one collection, "Marathon", in its order
}

func (c *serTwitchChannel) collection(ids ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.marathon = ids
}

func (c *serTwitchChannel) set(vods ...serTwitchVOD) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vods = vods
}

func (c *serTwitchChannel) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string `json:"query"`
			Variables struct {
				First int    `json:"first"`
				After string `json:"after"`
			} `json:"variables"`
		}
		payload, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Errorf("bad GraphQL request: %v", err)
		}
		if id := r.Header.Get("Client-Id"); id != contentTwitchID {
			t.Errorf("client id %q", id)
		}
		c.mu.Lock()
		vods := slices.Clone(c.vods)
		c.mu.Unlock()
		switch {
		case strings.Contains(body.Query, "ViyuCatalog"):
			start := 0
			if body.Variables.After != "" {
				_, _ = fmt.Sscanf(body.Variables.After, "after-%d", &start)
			}
			// Two videos per page, whatever is asked for.
			end := min(start+2, len(vods))
			edges := []map[string]any{}
			for index, vod := range vods[start:end] {
				created := time.Date(2026, 1, 1+start+index, 20, 0, 0, 0, time.UTC).Format(time.RFC3339)
				edges = append(edges, map[string]any{"cursor": fmt.Sprintf("after-%d", end), "node": map[string]any{
					"id": vod.id, "title": vod.title, "createdAt": created, "lengthSeconds": 3600, "viewCount": 10,
					"previewThumbnailURL": "https://static-cdn.example/" + vod.id + ".jpg",
				}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"user": map[string]any{"videos": map[string]any{
				"edges": edges, "pageInfo": map[string]any{"hasNextPage": end < len(vods)},
			}}}})
		case strings.Contains(body.Query, "ViyuCollections"):
			c.mu.Lock()
			marathon := slices.Clone(c.marathon)
			c.mu.Unlock()
			collections := []map[string]any{}
			if len(marathon) > 0 {
				items := []map[string]any{}
				for _, id := range marathon {
					items = append(items, map[string]any{"node": map[string]any{"id": id}})
				}
				collections = append(collections, map[string]any{"node": map[string]any{"title": "Marathon", "items": map[string]any{"edges": items, "pageInfo": map[string]any{"hasNextPage": false}}}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"user": map[string]any{"collections": map[string]any{"edges": collections, "pageInfo": map[string]any{"hasNextPage": false}}}}})
		case strings.Contains(body.Query, "ViyuAccess"):
			_, _ = io.WriteString(w, `{"data":{}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"user":{"login":"streamer","displayName":"Streamer","description":"","profileImageURL":"https://static-cdn.example/streamer.png","followers":{"totalCount":5},"videos":{"edges":[]}}}}`)
		}
	}
}

// What Twitch lists in a catalog needs no downloader beyond the first listing: titles, dates, durations and
// artwork come with the GraphQL pages.
const serTwitchDownloader = `case "$*" in
 *twitch.tv/streamer/videos*) printf '%s\n' '{"_type":"playlist","id":"streamer","entries":[]}' ;;
 *) echo 'unexpected Twitch fixture URL' >&2; exit 9 ;;
esac`

// A Twitch channel gets series like a YouTube one: they are inferred from the titles of its catalog, ordered
// by the numbers in them with the dates Twitch gave (so no video page is fetched), and they shrink when VODs
// expire: a series left with one video disappears.
func TestTwitchSeriesAreInferredFromTheCatalogAndShrinkWhenVODsExpire(t *testing.T) {
	t.Parallel()
	channel := &serTwitchChannel{}
	channel.set(
		serTwitchVOD{"1005", "Balatro #2"}, serTwitchVOD{"1004", "Balatro #1"},
		serTwitchVOD{"1003", "Outer Wilds #3"}, serTwitchVOD{"1002", "Outer Wilds #2"}, serTwitchVOD{"1001", "Outer Wilds #1"},
		serTwitchVOD{"1000", "Just chatting tonight"},
	)
	twitch, _ := contentTwitchServer(t, channel.handler(t))
	downloader, calls := contentDownloader(t, serTwitchDownloader)
	app, handler := newApp(t, serQuickAnalysis, twitch, downloader)
	owner := testUser(t, app, "series-twitch-vods@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	record := storedChannel(t, app, "twitch", "streamer", "Streamer")
	serSubscribe(t, app, profile, record)

	before := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return serComplete(a) && len(a.Series) == 2 && a.Sync.Loaded == 6
	})
	counts := map[string]int{}
	for _, s := range before.Series {
		if s.Platform != "twitch" || s.ChannelID != record {
			t.Fatalf("a Twitch series is not addressed by its channel record: %+v", s)
		}
		counts[s.Title] = s.VideoCount
	}
	if counts["Outer Wilds"] != 3 || counts["Balatro"] != 2 {
		t.Fatalf("series=%v", counts)
	}
	var outerWilds string
	for _, s := range before.Series {
		if s.Title == "Outer Wilds" {
			outerWilds = s.ID
		}
	}
	detail := serGet(t, handler, token, outerWilds, profile.Id)
	var episodes []float64
	for _, item := range detail.Series.Items {
		if item.Episode == nil || item.Video.Published == "" || item.Video.Thumbnail == "" || item.Video.Duration != 3600 {
			t.Fatalf("a Twitch episode lacks what its catalog listed: %+v", item)
		}
		episodes = append(episodes, *item.Episode)
	}
	if !slices.Equal(episodes, []float64{1, 2, 3}) {
		t.Fatalf("episodes in order: %v", episodes)
	}
	if n := calls.count("twitch.tv/videos/"); n != 0 {
		t.Fatalf("%d video pages were fetched for dates the catalog already had", n)
	}

	// The first episodes expire: the next synchronization no longer sees them.
	channel.set(serTwitchVOD{"1005", "Balatro #2"}, serTwitchVOD{"1003", "Outer Wilds #3"}, serTwitchVOD{"1002", "Outer Wilds #2"}, serTwitchVOD{"1000", "Just chatting tonight"})
	row := serCatalogRow(t, app, "streamer")
	row.Set("completed_at", time.Now().Add(-48*time.Hour))
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	serSubscribe(t, app, acctRecord(t, app, "profiles", map[string]any{"user": owner.Id, "name": "Another"}), record)
	after := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return serComplete(a) && a.Sync.Loaded == 4 && len(a.Series) == 1
	})
	if after.Series[0].Title != "Outer Wilds" || after.Series[0].VideoCount != 2 || after.Series[0].ID != outerWilds {
		t.Fatalf("the series did not shrink with the catalog: %+v", after.Series)
	}
	// The series left with one video is gone from storage, not just from the answer.
	if left, err := app.FindRecordsByFilter("series", "channel = {:channel}", "", 0, 0, map[string]any{"channel": record}); err != nil || len(left) != 1 {
		t.Fatalf("stored series after the expiry: %d %v", len(left), err)
	}
	if status := after.Sync.Status; status != jobs.Complete {
		t.Fatalf("sync status %s", status)
	}
}

// The collections a Twitch channel made are series called what the streamer called them, in the streamer's
// order, and their videos leave the series the titles name.
func TestTwitchCollectionsAreSeriesInTheStreamersOrder(t *testing.T) {
	t.Parallel()
	channel := &serTwitchChannel{}
	channel.set(
		serTwitchVOD{"1005", "Balatro #2"}, serTwitchVOD{"1004", "Balatro #1"},
		serTwitchVOD{"1003", "Outer Wilds #3"}, serTwitchVOD{"1002", "Outer Wilds #2"}, serTwitchVOD{"1001", "Outer Wilds #1"},
		serTwitchVOD{"1000", "Just chatting tonight"},
	)
	// The streamer's order is neither the titles' nor the dates'; one video of it has expired.
	channel.collection("1000", "1003", "9999", "1001", "1005")
	twitch, _ := contentTwitchServer(t, channel.handler(t))
	app, handler := newApp(t, serQuickAnalysis, twitch, withDownloader(t, serTwitchDownloader))
	owner := testUser(t, app, "series-twitch-collections@example.test")
	token := tokenOf(t, owner)
	profile := profileOf(t, app, owner)
	record := storedChannel(t, app, "twitch", "streamer", "Streamer")
	serSubscribe(t, app, profile, record)

	answer := serWaitList(t, handler, token, profile.Id, func(a serAnswer) bool {
		return serComplete(a) && a.Sync.Loaded == 6 && len(a.Series) >= 1
	})
	// Outer Wilds #2 alone and Balatro #1 alone are no series once their neighbours belong to the collection.
	if len(answer.Series) != 1 || answer.Series[0].Title != "Marathon" || answer.Series[0].Order != "collection" || answer.Series[0].VideoCount != 4 {
		t.Fatalf("series=%+v", answer.Series)
	}
	detail := serGet(t, handler, token, answer.Series[0].ID, profile.Id)
	var titles []string
	for _, item := range detail.Series.Items {
		if item.Episode != nil || item.Season != nil {
			t.Fatalf("a collection numbers nothing: %+v", item)
		}
		titles = append(titles, item.Video.Title)
	}
	if want := []string{"Just chatting tonight", "Outer Wilds #3", "Outer Wilds #1", "Balatro #2"}; !slices.Equal(titles, want) {
		t.Fatalf("titles=%v, want %v", titles, want)
	}
}
