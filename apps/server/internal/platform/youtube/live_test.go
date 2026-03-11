package youtube

import (
	"context"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const streamsScript = `
case "$*" in
 *channel/UClive0000000000000000000/streams*) printf '%s\n' '{"_type":"playlist","id":"UClive0000000000000000000","entries":[{"id":"oldstream01","title":"Ended","live_status":"was_live","view_count":9},{"id":"livestream1","title":"On air","live_status":"is_live","view_count":55}]}' ;;
 *channel/UCquiet000000000000000000/streams*) printf '%s\n' '{"_type":"playlist","id":"UCquiet000000000000000000","entries":[{"id":"oldstream01","title":"Ended","live_status":"was_live"}]}' ;;
 *channel/UCbadid0000000000000000000/streams*) printf '%s\n' '{"_type":"playlist","id":"x","entries":[{"id":"short","title":"Bad id","live_status":"is_live"}]}' ;;
 *channel/UCnotab000000000000000000/streams*) echo 'ERROR: [youtube:tab] UCnotab: This channel does not have a streams tab' >&2; exit 1 ;;
 *@handle/streams*) printf '%s\n' '{"_type":"playlist","id":"x","entries":[{"id":"handlelive1","title":"By handle","live_status":"is_live","view_count":3}]}' ;;
 *) echo 'unexpected YouTube fixture URL' >&2; exit 9 ;;
esac
`

func TestChannelLiveFindsTheStreamInProgress(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, streamsScript)
	for id, want := range map[string]platform.LiveStream{
		"UClive0000000000000000000":  {Live: true, VideoID: "livestream1", Title: "On air", Viewers: 55},
		"UCquiet000000000000000000":  {},
		"UCnotab000000000000000000":  {},
		"UCbadid0000000000000000000": {},
		"@handle":                    {Live: true, VideoID: "handlelive1", Title: "By handle", Viewers: 3},
		"handle":                     {Live: true, VideoID: "handlelive1", Title: "By handle", Viewers: 3},
	} {
		got, err := p.LiveStream(context.Background(), id)
		if err != nil || got != want {
			t.Errorf("%s: got %+v err=%v, want %+v", id, got, err, want)
		}
	}
	if args := calls(t, log); !strings.Contains(args, "https://www.youtube.com/channel/UClive0000000000000000000/streams --dump-single-json --flat-playlist --playlist-end 5 --no-download") || strings.Contains(args, "--cookies") {
		t.Fatalf("a live lookup is public and reads the Streams tab: %s", args)
	}
}

func TestChannelLiveUnexpectedFailureIsAnErrorNotASilentOffline(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, streamsScript)
	_, err := p.LiveStream(context.Background(), "UCunexpected00000000000")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
}
