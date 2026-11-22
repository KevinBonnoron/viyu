package youtube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const aboutChannelID = "UC_x5XG1OV2P6uZZ5FSM9Ttw"

// aboutViewJSON is the About panel model of a channel page, as YouTube answered for @GoogleDevelopers to an
// English Accept-Language (description, link list and share command left out); replace takes old/new pairs.
func aboutViewJSON(replace ...string) string {
	view := `{"description":"Welcome to Google for Developers!","descriptionLabel":{"content":"Description","styleRuns":[{"startIndex":0,"length":11}]},` +
		`"country":"United States","subscriberCountText":"2.69M subscribers","viewCountText":"395,868,404 views",` +
		`"joinedDateText":{"content":"Joined Aug 22, 2007","styleRuns":[{"startIndex":0,"length":19}]},` +
		`"canonicalChannelUrl":"http://www.youtube.com/@GoogleDevelopers","channelId":"` + aboutChannelID + `","videoCountText":"6,129 videos"}`
	return strings.NewReplacer(replace...).Replace(view)
}

// aboutHTML wraps a panel model in the shape of the channel page: the data object the page was rendered
// from, whose first response endpoint opens the About panel.
func aboutHTML(view string) string {
	return `<!DOCTYPE html><html><head><script nonce="x">var other = {"a":1};</script><script nonce="x">var ytInitialData = ` +
		`{"responseContext":{"serviceTrackingParams":[{"service":"GFEEDBACK","params":[{"key":"route","value":"channel.about"}]}]},` +
		`"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[]}},` +
		`"onResponseReceivedEndpoints":[{"clickTrackingParams":"CAAQhGci","showEngagementPanelEndpoint":{"engagementPanel":{"engagementPanelSectionListRenderer":` +
		`{"header":{"engagementPanelTitleHeaderRenderer":{"title":{"simpleText":"Google for Developers"}}},"content":{"sectionListRenderer":{"contents":[` +
		`{"itemSectionRenderer":{"contents":[{"aboutChannelRenderer":{"metadata":{"aboutChannelViewModel":` + view + `}}}]}}]}}}}}}]};</script></head><body></body></html>`
}

var aboutNow = time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)

func TestParseAboutReadsTheJoinDateCountryAndReportedTotals(t *testing.T) {
	t.Parallel()
	about, err := parseAbout([]byte(aboutHTML(aboutViewJSON())), aboutChannelID, aboutNow)
	if err != nil {
		t.Fatal(err)
	}
	if about.CreatedAt != "2007-08-22" || about.Country != "United States" {
		t.Fatalf("about: %+v", about)
	}
	if about.VideoCount == nil || *about.VideoCount != 6129 || about.ViewCount == nil || *about.ViewCount != 395868404 {
		t.Fatalf("totals: %v %v", about.VideoCount, about.ViewCount)
	}
}

func TestParseAboutReadsADayWithoutLeadingZeroAndSingularCounts(t *testing.T) {
	t.Parallel()
	view := aboutViewJSON("Joined Aug 22, 2007", "Joined Jun 3, 2008", "395,868,404 views", "1 view", "6,129 videos", "1 video")
	about, err := parseAbout([]byte(aboutHTML(view)), "", aboutNow)
	if err != nil {
		t.Fatal(err)
	}
	if about.CreatedAt != "2008-06-03" || about.ViewCount == nil || *about.ViewCount != 1 || about.VideoCount == nil || *about.VideoCount != 1 {
		t.Fatalf("about: %+v", about)
	}
}

func TestParseAboutLeavesOutWhatIsMissingOrNotUnderstood(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		view string
		want channelAbout
	}{
		"no country": {
			view: `{"viewCountText":"1,137,008,128 views","joinedDateText":{"content":"Joined Jun 3, 2008"},"channelId":"` + aboutChannelID + `","videoCountText":"6,205 videos"}`,
			want: channelAbout{CreatedAt: "2008-06-03", ViewCount: new(float64(1137008128)), VideoCount: new(float64(6205))},
		},
		"another language is not parsed and its country is not kept": {
			view: aboutViewJSON(`"United States"`, `"Vereinigte Staaten"`, "395,868,404 views", "395.868.404 Aufrufe", "Joined Aug 22, 2007", "Am 22.08.2007 beigetreten", "6,129 videos", "6.129\u00a0Videos"),
		},
		"before YouTube existed": {
			view: aboutViewJSON("Aug 22, 2007", "Aug 22, 1999", "395,868,404 views", "x", "6,129 videos", "x"),
		},
		"in the future": {
			view: aboutViewJSON("Aug 22, 2007", "Dec 25, 2026", "395,868,404 views", "x", "6,129 videos", "x"),
		},
		"a day that does not exist": {
			view: aboutViewJSON("Aug 22, 2007", "Feb 30, 2020", "395,868,404 views", "x", "6,129 videos", "x"),
		},
		"not a join text": {
			view: aboutViewJSON("Joined Aug 22, 2007", "Last updated Aug 22, 2007", "395,868,404 views", "x", "6,129 videos", "x"),
		},
		"abbreviated counts are not exact": {
			view: aboutViewJSON("395,868,404 views", "395M views", "6,129 videos", "6.1K videos", "Joined Aug 22, 2007", "x"),
		},
		"counts without their unit": {
			view: aboutViewJSON("395,868,404 views", "395,868,404", "6,129 videos", "6,129", "Joined Aug 22, 2007", "x"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			about, err := parseAbout([]byte(aboutHTML(tc.view)), "", aboutNow)
			if err != nil {
				t.Fatal(err)
			}
			if !sameAbout(about, tc.want) {
				t.Fatalf("got %+v, want %+v", describe(about), describe(tc.want))
			}
		})
	}
}

func TestParseAboutRejectsAPageThatIsNotTheChannelsAboutPanel(t *testing.T) {
	t.Parallel()
	for name, page := range map[string]string{
		"not a channel page":    `<html>consent</html>`,
		"malformed data":        `<script>var ytInitialData = {"onResponseReceivedEndpoints":[</script>`,
		"no panel":              `<script>var ytInitialData = {"onResponseReceivedEndpoints":[{"clickTrackingParams":"x"}]};</script>`,
		"another channel":       aboutHTML(aboutViewJSON(aboutChannelID, "UCabcdefghijklmnopqrstuv")),
		"an empty panel":        aboutHTML(`{}`),
		"a modified date alone": aboutHTML(`{"description":"Last modified Aug 22, 2007"}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if about, err := parseAbout([]byte(page), aboutChannelID, aboutNow); err == nil {
				t.Fatalf("accepted %+v", about)
			}
		})
	}
}

func sameAbout(a, b channelAbout) bool { return describe(a) == describe(b) }

func describe(a channelAbout) string {
	count := func(value *float64) string {
		if value == nil {
			return "unknown"
		}
		return strconv.FormatFloat(*value, 'f', -1, 64)
	}
	return a.CreatedAt + "|" + a.Country + "|" + count(a.VideoCount) + "|" + count(a.ViewCount)
}

// aboutProbe records what the fake channel page was asked.
type aboutProbe struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (p *aboutProbe) last(t *testing.T) *http.Request {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		t.Fatal("the channel page was not asked")
	}
	return p.requests[len(p.requests)-1]
}

func (p *aboutProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

const playlistScript = `
case "$*" in
 *--dump-single-json*) printf '%s\n' '{"_type":"playlist","id":"` + aboutChannelID + `","channel_id":"` + aboutChannelID + `","channel":"Google for Developers","title":"Google for Developers","channel_follower_count":2690000,"entries":[{"id":"fixture-video","title":"Fixture video"},{"id":"second-video","title":"Second"}]}'; exit 0 ;;
esac
exit 1
`

func aboutServer(t *testing.T, status int, body string) (*Platform, *aboutProbe, string) {
	t.Helper()
	probe := &aboutProbe{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.mu.Lock()
		probe.requests = append(probe.requests, r.Clone(context.Background()))
		probe.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	p, log := fakeYTDLP(t, playlistScript, func(o *Options) { o.HTTP, o.AboutURL = server.Client(), server.URL+"/" })
	return p, probe, log
}

func TestChannelListingCarriesTheAboutPageMetadata(t *testing.T) {
	t.Parallel()
	p, probe, _ := aboutServer(t, http.StatusOK, aboutHTML(aboutViewJSON()))
	info, videos, err := p.ChannelListing(context.Background(), aboutChannelID, 30, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.CreatedAt != "2007-08-22" || info.Country != "United States" {
		t.Fatalf("channel: %+v", info)
	}
	if info.VideoCount == nil || *info.VideoCount != 6129 || info.ViewCount == nil || *info.ViewCount != 395868404 {
		t.Fatalf("the totals are the ones the page reports, not the size of the listing: %v %v", info.VideoCount, info.ViewCount)
	}
	if len(videos) != 2 || info.Name != "Google for Developers" || info.SubscriberCount != 2690000 {
		t.Fatalf("the listing is kept: %+v %+v", info, videos)
	}
	request := probe.last(t)
	if request.Method != http.MethodGet || request.URL.Path != "/channel/"+aboutChannelID+"/about" || request.URL.RawQuery != "" {
		t.Fatalf("request: %s %s", request.Method, request.URL)
	}
	if request.Header.Get("Accept-Language") != "en-US,en;q=0.9" || request.Header.Get("Cookie") != "SOCS=CAI" || request.Header.Get("Authorization") != "" {
		t.Fatalf("headers: %v", request.Header)
	}
}

func TestChannelListingAsksThePageOfTheCanonicalIDWhenListedByHandle(t *testing.T) {
	t.Parallel()
	p, probe, _ := aboutServer(t, http.StatusOK, aboutHTML(aboutViewJSON()))
	info, _, err := p.ChannelListing(context.Background(), "@GoogleDevelopers", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := probe.last(t).URL.Path; got != "/channel/"+aboutChannelID+"/about" || info.CreatedAt != "2007-08-22" {
		t.Fatalf("the page is asked by the id the listing resolved: %s %+v", got, info)
	}
}

func TestConnectedChannelListingReadsThePageWithoutTheSession(t *testing.T) {
	t.Parallel()
	p, probe, log := aboutServer(t, http.StatusOK, aboutHTML(aboutViewJSON()))
	info, videos, err := p.ChannelListing(context.Background(), aboutChannelID, 30, "/private/jar.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := calls(t, log); !strings.Contains(got, "--cookies /private/jar.txt") {
		t.Fatalf("the listing runs as the session: %s", got)
	}
	if info.CreatedAt != "2007-08-22" || len(videos) != 2 {
		t.Fatalf("channel: %+v", info)
	}
	request := probe.last(t)
	if request.Header.Get("Cookie") != "SOCS=CAI" || strings.Contains(request.URL.String(), "jar") {
		t.Fatalf("the page request must not carry the session: %v %s", request.Header, request.URL)
	}
}

func TestChannelListingSurvivesAnAboutPageThatIsUnavailable(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"server error":  {http.StatusInternalServerError, "oops"},
		"not found":     {http.StatusNotFound, ""},
		"consent page":  {http.StatusOK, "<html>Before you continue to YouTube</html>"},
		"another panel": {http.StatusOK, aboutHTML(aboutViewJSON(aboutChannelID, "UCabcdefghijklmnopqrstuv"))},
		"localized":     {http.StatusOK, aboutHTML(aboutViewJSON("Joined Aug 22, 2007", "Am 22.08.2007 beigetreten", "395,868,404 views", "395.868.404 Aufrufe", "6,129 videos", "6.129 Videos", "United States", "Vereinigte Staaten"))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, probe, _ := aboutServer(t, tc.status, tc.body)
			info, videos, err := p.ChannelListing(context.Background(), aboutChannelID, 30, "")
			if err != nil {
				t.Fatalf("the optional page failed the listing: %v", err)
			}
			if probe.count() != 1 || len(videos) != 2 || info.Name != "Google for Developers" {
				t.Fatalf("the listing must stay as it was: %+v %+v", info, videos)
			}
			if info.CreatedAt != "" || info.Country != "" || info.VideoCount != nil || info.ViewCount != nil {
				t.Fatalf("metadata was invented: %+v", info)
			}
		})
	}
}

func TestChannelListingDoesNotAskThePageOfAChannelWithoutAnAddress(t *testing.T) {
	t.Parallel()
	probe := &aboutProbe{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.mu.Lock()
		probe.requests = append(probe.requests, r.Clone(context.Background()))
		probe.mu.Unlock()
	}))
	t.Cleanup(server.Close)
	p, _ := fakeYTDLP(t, listingScript, func(o *Options) { o.HTTP, o.AboutURL = server.Client(), server.URL })
	info, _, err := p.ChannelListing(context.Background(), "UCfixture", 30, "")
	if err != nil || probe.count() != 0 || info.CreatedAt != "" {
		t.Fatalf("a channel named by an id that is not canonical has no page: %v %d %+v", err, probe.count(), info)
	}
}

func TestEnrichChannelStopsWithItsContextAndKeepsTheProfile(t *testing.T) {
	t.Parallel()
	p, _, _ := aboutServer(t, http.StatusOK, aboutHTML(aboutViewJSON()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info := platform.Channel{ID: aboutChannelID, CreatedAt: "2001-01-01"}
	p.enrichChannel(ctx, aboutChannelID, &info)
	if info.CreatedAt != "2001-01-01" {
		t.Fatalf("a cancelled lookup changed the profile: %+v", info)
	}
}

func TestAboutPathNamesOnlyChannelsWithAPage(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		aboutChannelID:      "/channel/" + aboutChannelID + "/about",
		"@GoogleDevelopers": "/@GoogleDevelopers/about",
		"@café":             "/@caf%C3%A9/about",
		"UCfixture":         "",
		"GoogleDevelopers":  "",
		"@a/../b":           "",
		"":                  "",
	} {
		if got, ok := aboutPath(id); got != want || ok != (want != "") {
			t.Errorf("aboutPath(%q) = %q, %v", id, got, ok)
		}
	}
}
