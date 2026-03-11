package youtube

import (
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// ResolveURL reads the shapes of YouTube links the client accepts. Videos: watch, youtu.be, shorts, live,
// embed, nocookie, mobile and music. Channels: /channel/<UC id> and /@<handle>, optionally on one of the
// channel's tabs (/@handle/videos).
func (*Platform) ResolveURL(link platform.Link) (platform.Target, bool) {
	switch link.Host {
	case "youtu.be", "www.youtu.be":
		if len(link.Parts) == 1 && videoIDPattern.MatchString(link.Parts[0]) {
			return platform.Target{ID: link.Parts[0]}, true
		}
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
		if link.URL.Path == "/watch" {
			if id := link.URL.Query().Get("v"); videoIDPattern.MatchString(id) {
				return platform.Target{ID: id}, true
			}
		} else if len(link.Parts) == 2 && (link.Parts[0] == "shorts" || link.Parts[0] == "live" || link.Parts[0] == "embed") && videoIDPattern.MatchString(link.Parts[1]) {
			return platform.Target{ID: link.Parts[1]}, true
		} else if id, ok := channelOfPath(link.Parts); ok {
			return platform.Target{ID: id, Channel: true}, true
		}
	}
	return platform.Target{}, false
}

// channelOfPath reads a channel page path: /channel/<UC id> or /@<handle>, and at most one tab after it.
func channelOfPath(parts []string) (string, bool) {
	switch {
	case len(parts) >= 2 && len(parts) <= 3 && parts[0] == "channel" && channelIDPattern.MatchString(parts[1]):
		return parts[1], true
	case len(parts) >= 1 && len(parts) <= 2 && strings.HasPrefix(parts[0], "@") && handlePattern.MatchString(parts[0]):
		return parts[0], true
	}
	return "", false
}
