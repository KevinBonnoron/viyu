package youtube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const channelFeed = `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>newvideo001</yt:videoId><title>La science &amp; ses images</title><published>2026-03-05T00:30:00+02:00</published><updated>2026-11-01T12:00:00Z</updated></entry><entry><yt:videoId>unknown0001</yt:videoId><title>Unknown</title><published>not-a-date</published></entry></feed>`

func TestPublicationDatesUsePublishedNotUpdated(t *testing.T) {
	t.Parallel()
	entries, err := publicationMetadata(strings.NewReader(channelFeed))
	if err != nil {
		t.Fatal(err)
	}
	if entries["newvideo001"].Published != "2026-03-04T22:30:00Z" || entries["newvideo001"].Title != "La science & ses images" {
		t.Fatalf("lost original publication metadata: %+v", entries)
	}
	if _, ok := entries["unknown0001"]; ok {
		t.Fatal("invented a publication date from malformed metadata")
	}
}

// feedProbe records what the fake RSS endpoint was asked.
type feedProbe struct {
	requests atomic.Int32
	query    atomic.Value
}

func feedServer(t *testing.T, status int, body string) (*Platform, *feedProbe) {
	t.Helper()
	probe := &feedProbe{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.query.Store(r.URL.RawQuery)
		probe.requests.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	p, _ := fakeYTDLP(t, "exit 1", func(o *Options) { o.HTTP, o.FeedURL = server.Client(), server.URL+"/feeds/videos.xml" })
	return p, probe
}

func TestEnrichListingCarriesExactTimesAndCanonicalTitlesAndSortsNewestFirst(t *testing.T) {
	t.Parallel()
	p, probe := feedServer(t, http.StatusOK, channelFeed)
	videos := []platform.Video{
		{ID: "oldvideo001", Title: "Old", Published: "2025-01-01"},
		{ID: "unknown0001x", Title: "Not in the feed"},
		{ID: "newvideo001", Title: "Translated listing title", OriginalTitle: "Translated listing title", Published: "2026-03-04"},
		{ID: "unknown0001", Title: "Unknown", Published: "2026-01-01"},
	}
	const channel = "UCabcdefghijklmnopqrstuv"
	p.EnrichListing(context.Background(), channel, videos)
	if query, _ := probe.query.Load().(string); probe.requests.Load() != 1 || query != "channel_id="+channel {
		t.Fatalf("requests=%d query=%q", probe.requests.Load(), query)
	}
	ids := []string{videos[0].ID, videos[1].ID, videos[2].ID, videos[3].ID}
	if strings.Join(ids, ",") != "newvideo001,unknown0001,oldvideo001,unknown0001x" {
		t.Fatalf("not newest first (unknown last): %v", ids)
	}
	news := videos[0]
	if news.Published != "2026-03-04T22:30:00Z" || news.Title != "La science & ses images" || news.OriginalTitle != news.Title || !news.OriginalTitleVerified {
		t.Fatalf("the feed refines the day to its time and gives the canonical title: %+v", news)
	}
	if videos[1].Published != "2026-01-01" || videos[1].OriginalTitleVerified {
		t.Fatalf("an entry with an invalid date is not trusted: %+v", videos[1])
	}
}

func TestEnrichListingNeverFails(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"server error":  {http.StatusInternalServerError, "oops"},
		"not found":     {http.StatusNotFound, ""},
		"malformed xml": {http.StatusOK, "<feed><entry>"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, probe := feedServer(t, tc.status, tc.body)
			videos := []platform.Video{{ID: "newvideo001", Title: "Listing", Published: "2026-03-04"}, {ID: "oldvideo001", Title: "Old", Published: "2025-01-01"}}
			p.EnrichListing(context.Background(), "UCabcdefghijklmnopqrstuv", videos)
			if probe.requests.Load() != 1 || videos[0].Title != "Listing" || videos[0].Published != "2026-03-04" || videos[1].ID != "oldvideo001" {
				t.Fatalf("the listing must stay as it was: %+v", videos)
			}
		})
	}
}

func TestEnrichListingSkipsTheFeedForAChannelWithoutACanonicalIDButStillSorts(t *testing.T) {
	t.Parallel()
	p, probe := feedServer(t, http.StatusOK, channelFeed)
	videos := []platform.Video{{ID: "old", Published: "2025-01-01"}, {ID: "unknown"}, {ID: "new", Published: "2026-02-02"}}
	for _, channel := range []string{"@handle", "UCfixture", ""} {
		p.EnrichListing(context.Background(), channel, videos)
	}
	if probe.requests.Load() != 0 {
		t.Fatalf("the feed was asked for %d channels it cannot name", probe.requests.Load())
	}
	if videos[0].ID != "new" || videos[1].ID != "old" || videos[2].ID != "unknown" {
		t.Fatalf("order: %+v", videos)
	}
}

func TestEnrichListingStopsWithItsContext(t *testing.T) {
	t.Parallel()
	p, _ := feedServer(t, http.StatusOK, channelFeed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	videos := []platform.Video{{ID: "newvideo001", Title: "Listing", Published: "2026-03-04"}}
	p.EnrichListing(ctx, "UCabcdefghijklmnopqrstuv", videos)
	if videos[0].Title != "Listing" {
		t.Fatalf("a cancelled lookup changed the listing: %+v", videos)
	}
}
