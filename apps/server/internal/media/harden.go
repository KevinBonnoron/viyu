package media

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// harden applies to everything the proxy answers: the bytes come from third parties, so the browser must
// never interpret them as a document, whatever the upstream claims.
func harden(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox; default-src 'none'")
}

// requestHeaders chooses the headers of the manifest that may accompany a request for remote. They are
// the downloader's: some carry a session. They go in full only to the host of the manifest itself, without
// credentials to the platform's own media hosts a playlist may point at, and not at all to any other host
// a playlist names.
func requestHeaders(trusted func(host string) bool, manifest platform.Manifest, remote *url.URL) map[string]string {
	host := strings.ToLower(remote.Hostname())
	if own, err := url.Parse(manifest.URL); err == nil && strings.ToLower(own.Hostname()) == host {
		return manifest.Headers
	}
	if !trusted(host) {
		return nil
	}
	headers := make(map[string]string, len(manifest.Headers))
	for name, value := range manifest.Headers {
		switch strings.ToLower(name) {
		case "cookie", "authorization", "proxy-authorization":
		default:
			headers[name] = value
		}
	}
	return headers
}

// contentType decides what the browser is told about a proxied resource. Only streaming media types
// pass; anything else (HTML, SVG, scripts, XML) is served as an opaque download, so a hostile upstream
// cannot turn the API origin into a page that runs script with the viewer's session.
func contentType(raw string) (safe string, media bool) {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "application/octet-stream", false
	}
	switch {
	case mediaType == "text/vtt":
		return "text/vtt", true
	case strings.HasPrefix(mediaType, "video/"), strings.HasPrefix(mediaType, "audio/"):
		return raw, true
	}
	switch mediaType {
	case "application/octet-stream", "application/mp4", "application/mpeg4-iod", "application/x-mpegurl", "application/vnd.apple.mpegurl", "binary/octet-stream":
		return raw, true
	}
	return "application/octet-stream", false
}
