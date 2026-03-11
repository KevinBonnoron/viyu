package youtube

import "github.com/KevinBonnoron/viyu/apps/server/internal/platform"

// ResolveURL reads the shapes of YouTube video links the client accepts: watch, youtu.be, shorts, live,
// embed, nocookie, mobile and music.
func (*Platform) ResolveURL(link platform.Link) (string, bool) {
	switch link.Host {
	case "youtu.be", "www.youtu.be":
		if len(link.Parts) == 1 && videoIDPattern.MatchString(link.Parts[0]) {
			return link.Parts[0], true
		}
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
		if link.URL.Path == "/watch" {
			if id := link.URL.Query().Get("v"); videoIDPattern.MatchString(id) {
				return id, true
			}
		} else if len(link.Parts) == 2 && (link.Parts[0] == "shorts" || link.Parts[0] == "live" || link.Parts[0] == "embed") && videoIDPattern.MatchString(link.Parts[1]) {
			return link.Parts[1], true
		}
	}
	return "", false
}
