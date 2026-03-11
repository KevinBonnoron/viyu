package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// What a connected account fetched is stored for that account alone: another account is told the record does
// not exist, on every route that addresses it, while the account itself reads it.
func TestRecordsAnotherAccountCachedPrivatelyAreNotServed(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	first := testUser(t, app, "private-first@example.test")
	second := testUser(t, app, "private-second@example.test")
	secret := platform.Video{ID: "secretvid01", Platform: "youtube", ChannelID: "UCsecret", ChannelName: "Secret", Title: "Secret"}
	if err := newStore(app).StoreVideo(&secret, first.Id); err != nil {
		t.Fatal(err)
	}
	if secret.RecordID == "" || secret.ChannelRecordID == "" {
		t.Fatalf("the private video has no record ids: %+v", secret)
	}
	channel := secret.ChannelRecordID
	otherToken, ownerToken := tokenOf(t, second), tokenOf(t, first)

	for _, path := range []string{
		"/api/video/" + secret.RecordID,
		"/api/video/" + secret.RecordID + "/url",
		"/api/video/" + secret.RecordID + "/comments",
		"/api/channel/" + channel,
		"/api/channel/" + channel + "/live",
	} {
		res := request(t, handler, path, otherToken)
		if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), `"code":"NOT_FOUND"`) {
			t.Errorf("%s as another account: status=%d body=%s", path, res.Code, res.Body)
		}
	}

	// The owner reads its own record.
	res := request(t, handler, "/api/video/"+secret.RecordID+"/url", ownerToken)
	var page map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil || res.Code != http.StatusOK || !strings.HasPrefix(page["url"], "https://") {
		t.Fatalf("the owner cannot read its own private video: %d %s", res.Code, res.Body)
	}

	// The store agrees: only the owner lists the channel's cached videos.
	store := newStore(app)
	if videos, err := store.CachedChannelVideos(channel, second.Id); err != nil || len(videos) != 0 {
		t.Errorf("another account's listing: %v %v", videos, err)
	}
	if videos, err := store.CachedChannelVideos(channel, first.Id); err != nil || len(videos) != 1 {
		t.Errorf("the owner's listing: %v %v", videos, err)
	}
}
