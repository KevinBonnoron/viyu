package platform

import "strings"

// ChannelImage tells whether raw is an address a channel avatar may come from: an absolute web address
// with a path, on a host that is not a site's own pages or video artwork (those are banners and
// thumbnails, never avatars).
func ChannelImage(raw string) bool {
	address, ok := strings.CutPrefix(raw, "https://")
	if !ok {
		address, ok = strings.CutPrefix(raw, "http://")
	}
	if !ok {
		address, ok = strings.CutPrefix(raw, "//")
	}
	if !ok {
		return false
	}
	host, _, hasPath := strings.Cut(address, "/")
	return hasPath && host != "" && host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") && host != "ytimg.com" && !strings.HasSuffix(host, ".ytimg.com")
}

// ChannelImageURL returns the avatar address to store for raw: absolute, on the host that serves it
// reliably, or "" when raw is no avatar.
func ChannelImageURL(image string) string {
	if strings.HasPrefix(image, "//") {
		image = "https:" + image
	}
	if suffix, ok := strings.CutPrefix(image, "https://yt3.ggpht.com/"); ok {
		image = "https://yt3.googleusercontent.com/" + suffix
	}
	if ChannelImage(image) {
		return image
	}
	return ""
}

// First returns the first value that is not empty.
func First(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
