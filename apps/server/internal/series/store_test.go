package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

var fakeID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)

// fakePlatform is the platform the series tests store videos under: it stores channels under a handle
// with or without its "@".
type fakePlatform struct {
	platform.Platform // the methods a store never calls stay unimplemented
}

func (fakePlatform) ID() string                  { return "youtube" }
func (fakePlatform) SingleTitle() bool           { return false }
func (fakePlatform) ValidVideoID(id string) bool { return fakeID.MatchString(id) }
func (fakePlatform) ValidChannelID(id string) bool {
	return fakeID.MatchString(strings.TrimPrefix(id, "@"))
}
func (fakePlatform) ChannelIDAliases(id string) []string {
	bare := strings.TrimPrefix(id, "@")
	return []string{bare, "@" + bare}
}

type fixture struct {
	app     core.App
	catalog *catalog.Store
	parser  *Parser
	store   *Store
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	app := testutil.NewApp(t)
	registry := platform.NewRegistry(fakePlatform{})
	store := catalog.New(app, registry, catalog.Options{})
	parser := NewParser(nil)
	return &fixture{app: app, catalog: store, parser: parser, store: NewStore(app, store, registry, parser)}
}

// seed stores the videos of a channel (the way a catalog sync does) and returns them presented, with their
// record ids, plus the record id of the channel.
func (f *fixture) seed(t testing.TB, channel string, videos ...platform.Video) ([]platform.Video, string) {
	t.Helper()
	videos = channelVideos(channel, videos...)
	if err := f.catalog.StoreVideos(videos, ""); err != nil {
		t.Fatal(err)
	}
	record, err := f.catalog.FindChannel("youtube", channel)
	if err != nil {
		t.Fatal(err)
	}
	return videos, record.Id
}

func (f *fixture) records(t testing.TB, channelID string) []*core.Record {
	t.Helper()
	records, err := f.app.FindRecordsByFilter("series", "channel = {:channel}", "name", 0, 0, dbx.Params{"channel": channelID})
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// countQueries counts the queries fn runs whose text contains needle.
func countQueries(app core.App, needle string, fn func()) int {
	var count atomic.Int32
	for _, builder := range []dbx.Builder{app.ConcurrentDB(), app.NonconcurrentDB()} {
		db := builder.(*dbx.DB)
		previous := db.QueryLogFunc
		db.QueryLogFunc = func(_ context.Context, _ time.Duration, query string, _ *sql.Rows, _ error) {
			if strings.Contains(query, needle) {
				count.Add(1)
			}
		}
		defer func() { db.QueryLogFunc = previous }()
	}
	fn()
	return int(count.Load())
}

func countUpdates(app core.App, collection string) *atomic.Int32 {
	var updates atomic.Int32
	app.OnRecordAfterUpdateSuccess(collection).BindFunc(func(e *core.RecordEvent) error {
		updates.Add(1)
		return e.Next()
	})
	return &updates
}

func TestSavePersistsChannelAndVideoRelationsWithStableIDs(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "series-one", Title: "Series #1", Published: "2026-01-01"},
		platform.Video{ID: "series-two", Title: "Series #2", Published: "2026-01-02"},
	)
	list := f.parser.Infer(videos, nil)
	if len(list) != 1 {
		t.Fatalf("series=%+v", list)
	}
	if err := f.store.Save(channelID, true, list); err != nil {
		t.Fatal(err)
	}
	records := f.records(t, channelID)
	if len(records) != 1 || records[0].GetString("name") != "Series" || len(records[0].GetStringSlice("videos")) != 2 {
		t.Fatalf("series did not persist both video relations: %+v", records)
	}
	id := records[0].Id
	if list[0].ID != id || list[0].ChannelRecordID != channelID {
		t.Fatalf("stored ids = %q/%q, want series %q channel %q", list[0].ID, list[0].ChannelRecordID, id, channelID)
	}
	// Storing the same series again keeps its record.
	again := f.parser.Infer(videos, nil)
	if err := f.store.Save(channelID, true, again); err != nil || again[0].ID != id {
		t.Fatalf("re-storing changed the series id: %q %v", again[0].ID, err)
	}
}

func TestSaveWritesARecordOnlyWhenItsVideosChange(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "one", Title: "Series #1"},
		platform.Video{ID: "two", Title: "Series #2"},
	)
	list := f.parser.Infer(videos, nil)
	if err := f.store.Save(channelID, true, list); err != nil {
		t.Fatal(err)
	}
	updates := countUpdates(f.app, "series")
	for range 3 {
		if err := f.store.Save(channelID, true, f.parser.Infer(videos, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if updates.Load() != 0 {
		t.Fatalf("an unchanged series was saved %d times", updates.Load())
	}
	more, _ := f.seed(t, "UCseries", platform.Video{ID: "three", Title: "Series #3"})
	if err := f.store.Save(channelID, true, f.parser.Infer(append(slices.Clone(videos), more...), nil)); err != nil {
		t.Fatal(err)
	}
	if updates.Load() != 1 || len(f.records(t, channelID)[0].GetStringSlice("videos")) != 3 {
		t.Fatalf("a new member must be one update: %d", updates.Load())
	}
}

func TestSaveIncompleteCatalogNeverRemovesAndCompleteOneDoes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "one", Title: "Series #1"},
		platform.Video{ID: "two", Title: "Series #2"},
		platform.Video{ID: "x1", Title: "Gone #1"},
		platform.Video{ID: "x2", Title: "Gone #2"},
	)
	if err := f.store.Save(channelID, true, f.parser.Infer(videos, nil)); err != nil {
		t.Fatal(err)
	}
	if len(f.records(t, channelID)) != 2 {
		t.Fatalf("series=%+v", f.records(t, channelID))
	}
	// Only part of the catalog is known: one video of the first series, nothing of the second.
	if err := f.store.Save(channelID, false, nil); err != nil {
		t.Fatal(err)
	}
	partial := f.parser.Infer(videos[:3], nil)
	if len(partial) != 1 {
		t.Fatalf("partial=%+v", partial)
	}
	if err := f.store.Save(channelID, false, partial); err != nil {
		t.Fatal(err)
	}
	records := f.records(t, channelID)
	if len(records) != 2 || len(records[1].GetStringSlice("videos")) != 2 {
		t.Fatalf("a partial refresh erased stored membership: %+v", records)
	}
	// The complete catalog says one series went away.
	var keep []platform.Video
	for _, v := range videos {
		if strings.HasPrefix(v.ID, "x") {
			continue
		}
		keep = append(keep, v)
	}
	if err := f.store.Save(channelID, true, f.parser.Infer(keep, nil)); err != nil {
		t.Fatal(err)
	}
	records = f.records(t, channelID)
	if len(records) != 1 || records[0].GetString("name") != "Series" {
		t.Fatalf("a complete refresh retained a removed series: %+v", records)
	}
	// Deleting a series never deletes the shared videos.
	if err := f.store.Save(channelID, true, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.records(t, channelID)) != 0 {
		t.Fatal("complete refresh retained a removed series")
	}
	for _, v := range videos {
		if _, err := f.app.FindRecordById("videos", v.RecordID); err != nil {
			t.Fatalf("deleting a series deleted its shared video %s: %v", v.ID, err)
		}
	}
}

func TestSaveLeavesOutVideosThatAreNotStored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "one", Title: "Series #1"},
		platform.Video{ID: "two", Title: "Series #2"},
	)
	ghost := channelVideos("UCseries", platform.Video{ID: "ghost", Title: "Series #3"})
	list := f.parser.Infer(append(slices.Clone(videos), ghost...), nil)
	if err := f.store.Save(channelID, true, list); err != nil {
		t.Fatal(err)
	}
	if got := f.records(t, channelID)[0].GetStringSlice("videos"); len(got) != 2 {
		t.Fatalf("members=%v", got)
	}
}

func TestSaveResolvesVideosInBatches(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	const count = 2*queryBatch + 50
	batch := make([]platform.Video, count)
	for i := range batch {
		batch[i] = platform.Video{ID: fmt.Sprintf("big%04d", i), Title: fmt.Sprintf("Marathon #%d", i+1)}
	}
	videos, channelID := f.seed(t, "UCbig", batch...)
	list := f.parser.Infer(videos, nil)
	var err error
	queries := countQueries(f.app, "FROM `videos`", func() { err = f.store.Save(channelID, true, list) })
	if err != nil {
		t.Fatal(err)
	}
	if queries < 1 || queries > 4 {
		t.Fatalf("saving %d videos ran %d video queries: not batched", count, queries)
	}
	if got := f.records(t, channelID)[0].GetStringSlice("videos"); len(got) != count {
		t.Fatalf("members=%d", len(got))
	}
	queries = countQueries(f.app, "FROM `videos`", func() {
		var ok bool
		ok, err = f.store.MembershipMatches(channelID, list)
		if err != nil || !ok {
			t.Errorf("stored series should match: %v %v", ok, err)
		}
	})
	if queries < 1 || queries > 4 {
		t.Fatalf("matching %d videos ran %d video queries: not batched", count, queries)
	}
	queries = countQueries(f.app, "FROM `videos`", func() {
		got, err := f.store.ForChannels([]string{channelID})
		if err != nil || len(got[channelID]) != 1 || got[channelID][0].VideoCount != count {
			t.Errorf("read=%v %v", got, err)
		}
	})
	if queries < 1 || queries > 4 {
		t.Fatalf("reading %d videos ran %d video queries: not batched", count, queries)
	}
}

func TestSaveIsOneTransaction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "a1", Title: "Alpha series #1"}, platform.Video{ID: "a2", Title: "Alpha series #2"},
		platform.Video{ID: "b1", Title: "Boom series #1"}, platform.Video{ID: "b2", Title: "Boom series #2"},
	)
	f.app.OnRecordCreate("series").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("name") == "Boom series" {
			return errors.New("refused")
		}
		return e.Next()
	})
	list := f.parser.Infer(videos, nil)
	if err := f.store.Save(channelID, true, list); err == nil {
		t.Fatal("the failure was swallowed")
	}
	if got := f.records(t, channelID); len(got) != 0 {
		t.Fatalf("a failed save left %d series behind", len(got))
	}
	for _, s := range list {
		if s.ID != "" {
			t.Fatalf("a series that was not stored kept an id: %+v", s.Summary)
		}
	}
}

func TestSaveRefusesAnUnknownChannelAndAnonymousSeries(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries", platform.Video{ID: "a1", Title: "Alpha #1"}, platform.Video{ID: "a2", Title: "Alpha #2"})
	list := f.parser.Infer(videos, nil)
	if err := f.store.Save("nosuchchannel01", true, list); !errors.Is(err, sql.ErrNoRows) || list[0].ID != "" {
		t.Fatalf("err=%v id=%q", err, list[0].ID)
	}
	list = append(list, Series{Summary: Summary{Title: "  "}, Items: []Item{}})
	if err := f.store.Save(channelID, true, list); err != nil {
		t.Fatal(err)
	}
	if list[0].ID == "" || list[1].ID != "" {
		t.Fatalf("ids=%q/%q", list[0].ID, list[1].ID)
	}
}

func TestMembershipMatchesFollowsTheStoredSeries(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "one", Title: "Series #1"},
		platform.Video{ID: "two", Title: "Series #2"},
	)
	expected := f.parser.Infer(videos, nil)
	if ok, err := f.store.MembershipMatches(channelID, expected); err != nil || ok {
		t.Fatalf("nothing is stored yet: %v %v", ok, err)
	}
	if err := f.store.Save(channelID, true, expected); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.store.MembershipMatches(channelID, expected); err != nil || !ok {
		t.Fatalf("stored series must match: %v %v", ok, err)
	}
	more, _ := f.seed(t, "UCseries", platform.Video{ID: "three", Title: "Series #3"})
	grown := f.parser.Infer(append(slices.Clone(videos), more...), nil)
	if ok, err := f.store.MembershipMatches(channelID, grown); err != nil || ok {
		t.Fatalf("a new member makes the stored series stale: %v %v", ok, err)
	}
	if ok, err := f.store.MembershipMatches(channelID, nil); err != nil || ok {
		t.Fatalf("a stored series nobody expects is stale: %v %v", ok, err)
	}
}

func TestForChannelsAndGetReadStoredSeriesOrdered(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first, firstChannel := f.seed(t, "UCfirst", balatroVideos()...)
	second, secondChannel := f.seed(t, "@second",
		platform.Video{ID: "w1", Title: "Outer Wilds #1", Published: "2026-01-01"},
		platform.Video{ID: "w2", Title: "Outer Wilds #2", Published: "2026-01-02"},
	)
	_, emptyChannel := f.seed(t, "UCempty", platform.Video{ID: "lonely", Title: "Just a video"})
	for _, save := range []struct {
		channel string
		videos  []platform.Video
	}{{firstChannel, first}, {secondChannel, second}} {
		if err := f.store.Save(save.channel, true, f.parser.Infer(save.videos, nil)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.store.ForChannels([]string{firstChannel, secondChannel, emptyChannel})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[emptyChannel]) != 0 {
		t.Fatalf("read=%+v", got)
	}
	balatroSeries := got[firstChannel][0]
	if balatroSeries.Title != "Balatro" || balatroSeries.ChannelID != "UCfirst" || balatroSeries.ChannelRecordID != firstChannel || balatroSeries.ChannelName != "UCfirst" || balatroSeries.Platform != "youtube" {
		t.Fatalf("summary=%+v", balatroSeries.Summary)
	}
	// The numbering that starts over is read back from the stored titles and dates.
	if balatroSeries.Order != "episode" || balatroSeries.VideoCount != 8 || balatroSeries.LatestPublishedAt != "2024-03-01T00:00:00Z" {
		t.Fatalf("order=%q count=%d latest=%q", balatroSeries.Order, balatroSeries.VideoCount, balatroSeries.LatestPublishedAt)
	}
	if want := []string{"d2", "d3", "d4", "g1", "g2", "g3", "g4", "g5"}; !reflect.DeepEqual(ids(balatroSeries), want) {
		t.Fatalf("ids=%v", ids(balatroSeries))
	}
	if number(balatroSeries.Items[0].Season) != 1 || number(balatroSeries.Items[3].Season) != 2 || balatroSeries.Items[0].Video.RecordID == "" || balatroSeries.Items[0].Video.ChannelRecordID != firstChannel {
		t.Fatalf("items=%+v", balatroSeries.Items)
	}
	handle := got[secondChannel][0]
	if handle.ChannelID != "second" {
		t.Fatalf("a handle is answered in its canonical form: %+v", handle.Summary)
	}

	one, channel, err := f.store.Get(handle.ID)
	if err != nil || channel.Id != secondChannel || one.ID != handle.ID || !reflect.DeepEqual(ids(one), []string{"w1", "w2"}) {
		t.Fatalf("get=%+v %v %v", one, channel, err)
	}
	if _, _, err := f.store.Get("doesnotexist123"); err == nil {
		t.Fatal("unknown series found")
	} else if e, ok := errors.AsType[*apierr.Error](err); !ok || e.Status != http.StatusNotFound {
		t.Fatalf("unknown series error=%v", err)
	}
}

func TestForChannelsAndGetStayOutsideInference(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	videos, channelID := f.seed(t, "UCseries",
		platform.Video{ID: "one", Title: "Series #1"},
		platform.Video{ID: "two", Title: "Series #2"},
	)
	if err := f.store.Save(channelID, true, f.parser.Infer(videos, nil)); err != nil {
		t.Fatal(err)
	}
	// A video that joins the catalog after the series was stored is not part of it until the analysis runs.
	f.seed(t, "UCseries", platform.Video{ID: "three", Title: "Series #3"})
	got, err := f.store.ForChannels([]string{channelID})
	if err != nil || len(got[channelID]) != 1 || got[channelID][0].VideoCount != 2 {
		t.Fatalf("a read inferred something: %+v %v", got, err)
	}
	if got, err := f.store.ForChannels(nil); err != nil || len(got) != 0 {
		t.Fatalf("no channels: %v %v", got, err)
	}
}
