package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// A video only a connected account's session could see is private to that account: nobody else may open its
// stream, whatever the platform would answer a public lookup.
func TestAPrivateVideoCannotBeStreamedByAnotherAccount(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	owner := testUser(t, app, "stream-owner@example.test")
	other := testUser(t, app, "stream-other@example.test")
	video := platform.Video{ID: "private-stream", Platform: testPlatforms().Default().ID(), Title: "Private", ChannelID: "UCprivate", ChannelName: "Private channel"}
	if err := newStore(app).StoreVideo(&video, owner.Id); err != nil {
		t.Fatal(err)
	}
	res := request(t, handler, "/api/stream/"+video.RecordID, tokenOf(t, other))
	if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), `"code":"STREAM_NOT_FOUND"`) {
		t.Fatalf("another account reached a private stream: %d %s", res.Code, res.Body)
	}
	if res = request(t, handler, "/api/stream/"+video.RecordID+"/resource/abc", tokenOf(t, other)); res.Code != http.StatusNotFound {
		t.Fatalf("another account reached a private resource: %d %s", res.Code, res.Body)
	}
}
