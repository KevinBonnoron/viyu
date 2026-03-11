package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

func TestVideoReactionsAreExclusivePerProfileAndPrivateToTheAccount(t *testing.T) {
	t.Parallel()
	app, handler := libApp(t, libDownloaderFailing(t))
	owner, other := testUser(t, app, "reactions-owner@example.test"), testUser(t, app, "reactions-other@example.test")
	profile := profileOf(t, app, owner)
	token, otherToken := tokenOf(t, owner), tokenOf(t, other)
	video := storedVideo(t, app, platform.Video{ID: "fixturevid1", ChannelID: libChannelID("fixture"), Title: "Fixture"})
	body := map[string]string{"profile": profile.Id, "video": video, "reaction": "like"}

	created := libDo(t, handler, http.MethodPost, "/api/collections/video_reactions/records", token, body)
	record := libOK[struct {
		ID       string `json:"id"`
		Reaction string `json:"reaction"`
	}](t, created)
	if record.Reaction != "like" || record.ID == "" {
		t.Fatalf("a positive reaction failed: %s", created.Body)
	}
	path := "/api/collections/video_reactions/records/" + record.ID
	reactionOf := func(token string) string {
		t.Helper()
		return libOK[struct {
			Reaction string `json:"reaction"`
		}](t, request(t, handler, path, token)).Reaction
	}

	// Switching is an update of the same row, to any of the three values.
	libFail(t, libDo(t, handler, http.MethodPatch, path, token, map[string]string{"reaction": "dislike"}), http.StatusOK, "")
	if got := reactionOf(token); got != "dislike" {
		t.Fatalf("a negative reaction was not persisted: %q", got)
	}
	libFail(t, libDo(t, handler, http.MethodPatch, path, token, map[string]string{"reaction": "not_interested"}), http.StatusOK, "")
	if got := reactionOf(token); got != "not_interested" {
		t.Fatalf("not interested was not persisted: %q", got)
	}
	// One reaction per profile and video, and only the values the schema knows.
	libFail(t, libDo(t, handler, http.MethodPost, "/api/collections/video_reactions/records", token, body), http.StatusBadRequest, "")
	libFail(t, libDo(t, handler, http.MethodPatch, path, token, map[string]string{"reaction": "invalid"}), http.StatusBadRequest, "")
	if n, err := app.CountRecords("video_reactions"); err != nil || n != 1 {
		t.Fatalf("two reactions for one profile and video: %d %v", n, err)
	}
	// A reaction is private: another account sees nothing, and can neither change nor remove it.
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		if res := libDo(t, handler, method, path, otherToken, map[string]string{"reaction": "like"}); res.Code != http.StatusNotFound {
			t.Fatalf("another account could %s a private reaction: %d %s", method, res.Code, res.Body)
		}
	}
	if got := reactionOf(token); got != "not_interested" {
		t.Fatalf("another account's request changed the reaction: %q", got)
	}

	libFail(t, libDo(t, handler, http.MethodDelete, path, token, nil), http.StatusNoContent, "")
	libFail(t, request(t, handler, path, token), http.StatusNotFound, "")
}

// libOnceDownloader answers one video page with the payload, once; a second run fails and is recorded, which
// proves a later answer came from the cache.
func libOnceDownloader(t *testing.T, payload string) (func(*Options), func() int) {
	t.Helper()
	binary := testutil.Script(t, "yt-dlp", `dir=$(dirname "$0")
echo run >> "$dir/runs"
if [ -f "$dir/answered" ]; then echo 'ERROR: asked twice' >&2; exit 1; fi
touch "$dir/answered"
printf '%s\n' '`+payload+`'`)
	runs := func() int {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "runs"))
		if err != nil {
			return 0
		}
		return len(data) / len("run\n")
	}
	return libBinary(binary), runs
}

func TestProviderReactionCountsDistinguishUnavailableFromZero(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name, payload     string
		likes             float64
		dislikesAvailable bool
	}{
		{"hidden dislikes", `{"id":"counts00001","title":"Video","channel_id":"UCfixture","channel":"Creator","like_count":12993}`, 12993, false},
		{"known zero counters", `{"id":"counts00001","title":"Video","channel_id":"UCfixture","channel":"Creator","like_count":0,"dislike_count":0}`, 0, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			option, runs := libOnceDownloader(t, fixture.payload)
			app, handler := libApp(t, option)
			id := storedVideo(t, app, platform.Video{ID: "counts00001", ChannelID: "UCfixture", ChannelName: "Creator"})
			for _, phase := range []string{"extracted", "cached"} {
				res := request(t, handler, "/api/video/"+id, "")
				video := libOK[platform.Video](t, res)
				if video.LikeCount == nil || *video.LikeCount != fixture.likes || (video.DislikeCount != nil) != fixture.dislikesAvailable {
					t.Fatalf("%s lost provider counter availability: %+v", phase, video)
				}
				if video.DislikeCount != nil && *video.DislikeCount != 0 {
					t.Fatalf("%s changed an explicit zero dislike count", phase)
				}
			}
			if runs() != 1 {
				t.Fatalf("the second answer was not served from the cache: %d downloader runs", runs())
			}
		})
	}
}
