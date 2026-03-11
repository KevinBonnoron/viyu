package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// searchScript answers the three queries a search makes: the channel filter, the second page and the first.
func searchScript(t *testing.T) string {
	t.Helper()
	line := func(value map[string]any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	video := func(index int) string {
		return line(map[string]any{"id": fmt.Sprintf("video%06d", index), "title": fmt.Sprintf("Video %d", index), "channel_id": "UCcreator", "channel": "Creator", "duration": 60})
	}
	channel := line(map[string]any{"_type": "url", "id": "UCego", "ie_key": "YoutubeTab", "channel": "EGO", "channel_id": "UCego", "url": "https://www.youtube.com/channel/UCego", "thumbnails": []map[string]any{{"url": "//yt3.ggpht.com/ego=s176-c", "width": 176, "height": 176}}})
	first := []string{}
	for index := 1; index <= 20; index++ {
		first = append(first, video(index))
		if index == 1 {
			first = append(first, channel)
		}
	}
	second := []string{video(20), video(21)}
	return fmt.Sprintf(`
case "$*" in
 *'search_query='*) printf '%%s\n' '%s';;
 *'--playlist-start 21'*) printf '%%s\n' '%s';;
 *) printf '%%s\n' '%s';;
esac
`, channel, strings.Join(second, "\n"), strings.Join(first, "\n"))
}

func TestSearchPromotesExactChannelAndContinuesWithoutLosingLookahead(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, searchScript(t))
	page, err := p.Search(context.Background(), platform.Session{}, "eGo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || !page.HasMore || len(page.Results) != 20 {
		t.Fatalf("first page: page=%d more=%v results=%d", page.Page, page.HasMore, len(page.Results))
	}
	card := page.Results[0].Channel
	if card == nil || card.ID != "UCego" || card.Platform != "youtube" || card.Thumbnail != "https://yt3.googleusercontent.com/ego=s176-c" || card.Source != "" {
		t.Fatalf("the exact channel was missing, duplicated or not promoted: %+v", page.Results[0])
	}
	for index, result := range page.Results[1:] {
		if result.Video == nil || result.Video.ID != fmt.Sprintf("video%06d", index+1) {
			t.Fatalf("video relevance order changed or a channel was repeated: %d %+v", index, result)
		}
	}
	args := calls(t, log)
	if !strings.Contains(args, "ytsearch21:eGo --dump-json --flat-playlist --no-download --playlist-start 1 --playlist-end 21") {
		t.Fatalf("the first page reads one result more than it shows: %s", args)
	}

	next, err := p.Search(context.Background(), platform.Session{}, "eGo", 2)
	if err != nil {
		t.Fatal(err)
	}
	if next.Page != 2 || next.HasMore || len(next.Results) != 2 || next.Results[0].Video.ID != "video000020" || next.Results[1].Video.ID != "video000021" {
		t.Fatalf("lost the lookahead result or fabricated more pages: %+v", next)
	}
	if args := calls(t, log); !strings.Contains(args, "ytsearch41:eGo --dump-json --flat-playlist --no-download --playlist-start 21 --playlist-end 41") {
		t.Fatalf("second page arguments: %s", args)
	}
}

func TestSearchRejectsInvalidPagesAndQueriesWithoutRunningAnything(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, searchScript(t))
	for _, page := range []int{0, -1, platform.MaxListPage + 1, 1 << 62} {
		_, err := p.Search(context.Background(), platform.Session{}, "eGo", page)
		assertCode(t, err, "INVALID_SOURCE_INPUT")
	}
	_, err := p.Search(context.Background(), platform.Session{}, "  ", 1)
	assertCode(t, err, "INVALID_SOURCE_INPUT")
	if got := calls(t, log); got != "" {
		t.Fatalf("an invalid search started yt-dlp: %s", got)
	}
}

func TestConnectedSearchMarksItsResultsWithTheirSource(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, searchScript(t))
	page, err := p.Search(context.Background(), platform.Session{UserID: "user", CookieFile: "/private/jar.txt"}, "eGo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Results[0].Channel.Source != "youtube" || page.Results[1].Video.Source != "youtube" {
		t.Fatalf("results of a connected search are private: %+v", page.Results[:2])
	}
	for _, line := range strings.Split(strings.TrimSpace(calls(t, log)), "\n") {
		if !strings.Contains(line, "--cookies /private/jar.txt") {
			t.Fatalf("a connected search ran without the session: %s", line)
		}
	}
	public, _ := fakeYTDLP(t, searchScript(t))
	anonymous, err := public.Search(context.Background(), platform.Session{}, "eGo", 1)
	if err != nil || anonymous.Results[1].Video.Source != "" {
		t.Fatalf("a public search carries no source: %v %+v", err, anonymous.Results[1])
	}
}

func TestSearchWithoutResultsIsAnEmptyListNotNull(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, "exit 0")
	page, err := p.Search(context.Background(), platform.Session{}, "nothing", 2)
	if err != nil || page.Results == nil || len(page.Results) != 0 || page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}
