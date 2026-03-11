package youtube

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

func bare() *Platform { return New(Options{Runner: ytdlp.New(ytdlp.Options{})}) }

func TestResolveURLShapes(t *testing.T) {
	t.Parallel()
	p := bare()
	const id = "dQw4w9WgXcQ"
	for raw, want := range map[string]string{
		"https://www.youtube.com/watch?v=" + id:                   id,
		"https://youtube.com/watch?v=" + id + "&t=42":             id,
		"https://m.youtube.com/watch?v=" + id:                     id,
		"https://music.youtube.com/watch?v=" + id:                 id,
		"https://youtu.be/" + id:                                  id,
		"https://www.youtu.be/" + id + "?si=x":                    id,
		"https://www.youtube.com/shorts/" + id:                    id,
		"https://www.youtube.com/live/" + id:                      id,
		"https://www.youtube.com/embed/" + id:                     id,
		"https://www.youtube-nocookie.com/embed/" + id:            id,
		"https://www.youtube.com/watch?v=short":                   "",
		"https://www.youtube.com/watch":                           "",
		"https://www.youtube.com/channel/UCfixture":               "",
		"https://www.youtube.com/shorts/" + id + "/extra":         "",
		"https://youtu.be/" + id + "/extra":                       "",
		"https://www.youtube.com.evil.test/watch?v=" + id:         "",
		"https://example.test/watch?v=" + id:                      "",
		"https://www.youtube.com/playlist?list=PLabcdefghijklmno": "",
	} {
		address, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, part := range strings.Split(strings.Trim(address.Path, "/"), "/") {
			if part != "" {
				parts = append(parts, part)
			}
		}
		got, ok := p.ResolveURL(platform.Link{URL: address, Host: strings.ToLower(address.Hostname()), Parts: parts})
		if ok != (want != "") || got != want {
			t.Errorf("%s: got %q %v, want %q", raw, got, ok, want)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	t.Parallel()
	p := bare()
	if p.ID() != "youtube" || !p.DefaultEnabled() || p.SingleTitle() || p.CommentsMode() != platform.CommentsList || p.Chat() != nil {
		t.Fatalf("unexpected platform traits: %q %v %v %q %v", p.ID(), p.DefaultEnabled(), p.SingleTitle(), p.CommentsMode(), p.Chat())
	}
	if p.Connector() == nil || p.CatalogSync() == nil {
		t.Fatal("YouTube is connectable by cookies and its catalog can be synchronized")
	}
	descriptor := p.Descriptor()
	if descriptor.Platform != "youtube" || !slices.Equal(descriptor.ConnectionMethods, []string{"cookies"}) {
		t.Fatalf("descriptor: %+v", descriptor)
	}
	for id, valid := range map[string]bool{"dQw4w9WgXcQ": true, "-proxy": false, "http:127.0.0.1": false, "dQw4w9WgXcQx": false, "": false} {
		if p.ValidVideoID(id) != valid || p.ClaimsVideoID(id) != valid {
			t.Errorf("video id %q: valid=%v", id, valid)
		}
	}
	if got := p.OriginalURL("dQw4w9WgXcQ"); got != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" || p.OriginalURL("nope") != "" {
		t.Fatalf("original url = %q", got)
	}
	for id, valid := range map[string]bool{"UC" + strings.Repeat("a", 22): true, "@handle.name": true, "@日本語": true, "UCshort": false, "bad/channel": false, "handle": false} {
		if p.ValidChannelID(id) != valid {
			t.Errorf("channel id %q: valid=%v", id, !valid)
		}
	}
	for id, valid := range map[string]bool{"UCfixture": true, "@handle": true, "handle": true, "bad/channel": false, "": false} {
		if p.ValidLiveChannel(id) != valid {
			t.Errorf("live channel %q: valid=%v", id, !valid)
		}
	}
}

func TestChannelIDAliasesAndEndLive(t *testing.T) {
	t.Parallel()
	p := bare()
	want := []string{"handle", "@handle"}
	for _, id := range []string{"handle", "@handle"} {
		if got := p.ChannelIDAliases(id); !slices.Equal(got, want) {
			t.Errorf("aliases of %q = %v", id, got)
		}
		if end := p.EndLive(id); !slices.Equal(end.ChannelIDs, want) || end.StubVideoID != "" {
			t.Errorf("end of live of %q = %+v", id, end)
		}
	}
}

func TestTrustedMediaHostAndLiveThumbnail(t *testing.T) {
	t.Parallel()
	p := bare()
	for host, trusted := range map[string]bool{
		"googlevideo.com": true, "rr1---sn-abc.googlevideo.com": true, "www.youtube.com": true, "youtube.com": true,
		"evilgooglevideo.com": false, "googlevideo.com.evil.test": false, "example.test": false, "ttvnw.net": false,
	} {
		if p.TrustedMediaHost(host) != trusted {
			t.Errorf("host %q trusted=%v", host, !trusted)
		}
	}
	if got := p.LiveThumbnail(platform.LiveStream{VideoID: "abc"}); got != "https://i.ytimg.com/vi/abc/hqdefault_live.jpg" {
		t.Fatalf("live thumbnail = %q", got)
	}
}
