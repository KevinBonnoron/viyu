package stats

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

const (
	alpha = "youtube"
	beta  = "twitch"
)

// fakePlatform is the part of platform.Platform the activations use.
type fakePlatform struct {
	platform.Platform
	id string
}

func (p fakePlatform) ID() string           { return p.id }
func (p fakePlatform) DefaultEnabled() bool { return true }

type fixture struct {
	t       *testing.T
	app     core.App
	svc     *Service
	sources *sources.Service
	user    *core.Record
	profile *core.Record
}

func newFixture(t *testing.T, email string) *fixture {
	t.Helper()
	app := testutil.NewApp(t)
	registry := platform.NewRegistry(fakePlatform{id: alpha}, fakePlatform{id: beta})
	runner := ytdlp.New(ytdlp.Options{CookieDir: filepath.Join(app.DataDir(), "cookies")})
	accounts, err := sources.New(app, registry, runner, sources.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, app: app, sources: accounts, svc: New(app, catalog.New(app, registry, catalog.Options{}), accounts)}
	f.user = f.newUser(email)
	f.profile = f.save("profiles", map[string]any{"user": f.user.Id, "name": "Stats"})
	return f
}

func (f *fixture) newUser(email string) *core.Record {
	f.t.Helper()
	collection, err := f.app.FindCollectionByNameOrId("users")
	if err != nil {
		f.t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("stats-fixture-password-123")
	record.Set("role", "user")
	if err := f.app.Save(record); err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *fixture) save(collection string, values map[string]any) *core.Record {
	f.t.Helper()
	target, err := f.app.FindCollectionByNameOrId(collection)
	if err != nil {
		f.t.Fatal(err)
	}
	record := core.NewRecord(target)
	record.Load(values)
	if err := f.app.Save(record); err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *fixture) video(platformID, id, channelID, category string, tags []string) *core.Record {
	f.t.Helper()
	return f.save("videos", map[string]any{"platform": platformID, "platform_id": id, "titles": map[string]string{"original": id}, "tags": tags, "category": category, "channel": channelID, "expires_at": "2030-01-01 00:00:00.000Z"})
}

func (f *fixture) watch(video *core.Record, seconds float64, watched time.Time) {
	f.t.Helper()
	f.save("watch_history", map[string]any{"profile": f.profile.Id, "video": video.Id, "watched_at": watched.UTC().Format(platform.StoredLayout), "duration_watched": seconds})
}

func (f *fixture) compute(period string) Stats {
	f.t.Helper()
	stats, err := f.svc.Compute(f.user.Id, f.profile.Id, period)
	if err != nil {
		f.t.Fatal(err)
	}
	return stats
}

func TestStatsRankWhatTheProfileWatchedByTimeAndCountVideosOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "stats-rank@example.test")
	studio := f.save("channels", map[string]any{"platform": alpha, "platform_id": "UCstudio", "name": "Studio", "thumbnail": "https://img.example/studio.jpg", "expires_at": "2030-01-01 00:00:00.000Z"})
	streamer := f.save("channels", map[string]any{"platform": beta, "platform_id": "streamer", "name": "Streamer", "expires_at": "2030-01-01 00:00:00.000Z"})
	// Tags are free text: spelling and spacing differ, a tag may repeat inside one video, and a sentence is no tag.
	a := f.video(alpha, "a", studio.Id, "Gaming", []string{"Speedrun", " speed  run ", "speedrun", "Minecraft"})
	b := f.video(alpha, "b", studio.Id, "gaming", []string{"SPEEDRUN", "a very long tag that is really a sentence typed into the tag field"})
	tw := f.video(beta, "streamer", streamer.Id, "World of Warcraft", nil)
	// A video without a channel or tags still counts for time and platform.
	bare := f.video(alpha, "bare", "", "", nil)
	now := time.Now()
	f.watch(a, 600, now)
	f.watch(b, 1800, now)
	f.watch(tw, 300, now)
	f.watch(bare, 100, now)

	stats := f.compute("")
	if stats.Period != "all" {
		t.Fatalf("period=%q", stats.Period)
	}
	if stats.Totals.Videos != 4 || stats.Totals.Seconds != 2800 || stats.Totals.Channels != 2 {
		t.Fatalf("totals: %+v", stats.Totals)
	}
	if len(stats.TopTags) != 3 || stats.TopTags[0] != (Entry{Key: "speedrun", Name: "SPEEDRUN", Seconds: 2400, Count: 2}) {
		t.Fatalf("a tag counts once per video whatever its spelling: %+v", stats.TopTags)
	}
	if stats.TopTags[1].Key != "minecraft" || stats.TopTags[2].Key != "speed run" {
		t.Fatalf("tag order and normalization: %+v", stats.TopTags)
	}
	for _, tag := range stats.TopTags {
		if len(tag.Key) > tagLength {
			t.Fatalf("a sentence was ranked as a tag: %+v", tag)
		}
	}
	if len(stats.TopCategories) != 2 || stats.TopCategories[0].Key != "gaming" || stats.TopCategories[0].Seconds != 2400 || stats.TopCategories[0].Count != 2 || stats.TopCategories[0].Name != "Gaming" {
		t.Fatalf("categories merge by key and keep the common spelling: %+v", stats.TopCategories)
	}
	if len(stats.TopChannels) != 2 || stats.TopChannels[0].Name != "Studio" || stats.TopChannels[0].ChannelID != "UCstudio" || stats.TopChannels[0].Seconds != 2400 || stats.TopChannels[0].Thumbnail != "https://img.example/studio.jpg" || stats.TopChannels[1].Platform != beta {
		t.Fatalf("channels: %+v", stats.TopChannels)
	}
	if len(stats.ByPlatform) != 2 || stats.ByPlatform[0].Platform != alpha || stats.ByPlatform[0].Count != 3 || stats.ByPlatform[0].Seconds != 2500 {
		t.Fatalf("platforms: %+v", stats.ByPlatform)
	}
	if len(stats.ByMonth) != 1 || stats.ByMonth[0].Month != now.UTC().Format("2006-01") || stats.ByMonth[0].Count != 4 {
		t.Fatalf("months: %+v", stats.ByMonth)
	}
}

func TestStatsOfAnEmptyProfileAreZeroedNotNull(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "stats-empty@example.test")
	encoded, err := json.Marshal(f.compute("all"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"topTags", "topCategories", "topChannels", "byMonth", "byPlatform"} {
		if list, ok := raw[field].([]any); !ok || len(list) != 0 {
			t.Fatalf("%s must be an empty list, not %v", field, raw[field])
		}
	}
	for _, field := range []string{"period", "totals"} {
		if _, ok := raw[field]; !ok {
			t.Fatalf("field %s is missing from %s", field, encoded)
		}
	}
}

func TestAChannelIsAddressedByItsRecordIDOnTheWire(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(Channel{Platform: alpha, ChannelID: "UCnative", RecordID: "record0000001", Name: "Studio"})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["channelId"] != "record0000001" || wire["recordId"] != "record0000001" || strings.Contains(string(encoded), "UCnative") {
		t.Fatalf("the native channel id is on the wire: %s", encoded)
	}
}

func TestStatsPeriodKeepsOnlyRecentEntriesAndMonthsAreChronological(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "stats-period@example.test")
	old := f.video(alpha, "old", "", "Music", []string{"jazz"})
	recent := f.video(alpha, "new", "", "Gaming", []string{"rpg"})
	now := time.Now()
	f.watch(old, 500, now.AddDate(0, 0, -200))
	f.watch(recent, 700, now.AddDate(0, 0, -5))

	if stats := f.compute("30d"); stats.Period != "30d" || stats.Totals.Videos != 1 || stats.Totals.Seconds != 700 || len(stats.TopTags) != 1 || stats.TopTags[0].Key != "rpg" {
		t.Fatalf("30 days: %+v", stats)
	}
	if stats := f.compute("90d"); stats.Totals.Videos != 1 {
		t.Fatalf("90 days: %+v", stats.Totals)
	}
	all := f.compute("all")
	if all.Totals.Videos != 2 || len(all.ByMonth) < 2 || all.ByMonth[0].Month >= all.ByMonth[len(all.ByMonth)-1].Month {
		t.Fatalf("months run from the oldest to the newest: %+v", all.ByMonth)
	}
	if year := f.compute("365d"); year.Totals.Videos != 2 {
		t.Fatalf("a year: %+v", year.Totals)
	}
}

func TestStatsRefuseAnUnknownPeriod(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "stats-unknown@example.test")
	_, err := f.svc.Compute(f.user.Id, f.profile.Id, "forever")
	var safe *apierr.Error
	if !errors.As(err, &safe) || safe.Code != apierr.CodeInvalidRequest || safe.Status != 400 {
		t.Fatalf("unknown period: %v", err)
	}
}

func TestStatsOnlyKeepTheLastTwelveMonths(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "stats-months@example.test")
	for month := range 15 {
		video := f.video(alpha, string(rune('a'+month)), "", "", nil)
		f.watch(video, 60, time.Date(2026, time.Month(1+month%12), 15, 12, 0, 0, 0, time.UTC).AddDate(month/12, 0, 0))
	}
	stats := f.compute("")
	if len(stats.ByMonth) != monthsKept || stats.ByMonth[len(stats.ByMonth)-1].Month != "2027-03" {
		t.Fatalf("the series ends with the newest month and holds %d: %+v", monthsKept, stats.ByMonth)
	}
}

func TestStatsLeaveOutDisabledPlatformsAndOtherProfiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "stats-scope@example.test")
	otherProfile := f.save("profiles", map[string]any{"user": f.user.Id, "name": "Other"})
	yt := f.video(alpha, "yt", "", "Gaming", []string{"rpg"})
	tw := f.video(beta, "tw", "", "Just Chatting", []string{"talk"})
	now := time.Now()
	f.watch(yt, 100, now)
	f.watch(tw, 200, now)
	f.save("watch_history", map[string]any{"profile": otherProfile.Id, "video": yt.Id, "watched_at": now.UTC().Format(platform.StoredLayout), "duration_watched": 9000})

	if stats := f.compute(""); stats.Totals.Videos != 2 || stats.Totals.Seconds != 300 {
		t.Fatalf("another profile's history leaked in: %+v", stats.Totals)
	}
	if err := f.sources.SetEnabled(f.user.Id, beta, false); err != nil {
		t.Fatal(err)
	}
	if stats := f.compute(""); stats.Totals.Videos != 1 || stats.Totals.Seconds != 100 || len(stats.ByPlatform) != 1 || len(stats.TopTags) != 1 || stats.TopTags[0].Key != "rpg" {
		t.Fatalf("a platform the account turned off must not count: %+v", stats)
	}
}

func TestNormalizeTopicIsTrimmedCollapsedAndLowerCasedButKeepsAccents(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{"  Série   TV ": "série tv", "Speed\tRun": "speed run", "": "", "   ": "", "ÉCOLE": "école"} {
		if got := normalizeTopic(input); got != want {
			t.Errorf("normalizeTopic(%q) = %q, want %q", input, got, want)
		}
	}
}
