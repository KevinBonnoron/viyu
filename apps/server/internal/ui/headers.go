package ui

import "ganigeorgiev.com/pocketbase/pocketbase/core"

// contentSecurityPolicy is what the served client needs and nothing more: its own scripts and styles,
// images from any https host (video and channel artwork, emotes), media from itself and from MSE blob
// URLs (hls.js, whose transmuxing worker is also a blob), and the two community APIs it queries from the
// browser (SponsorBlock and Return YouTube Dislike). Nothing can frame the app or inject a base/object.
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' https: data: blob:; " +
	"media-src 'self' blob:; " +
	"connect-src 'self' https://sponsor.ajay.app https://returnyoutubedislikeapi.com; " +
	"style-src 'self' 'unsafe-inline'; " +
	"font-src 'self' data:; " +
	"worker-src 'self' blob:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"object-src 'none'; " +
	"form-action 'self'"

// SecurityHeaders is the middleware of the route that serves the embedded client.
func SecurityHeaders(e *core.RequestEvent) error {
	header := e.Response.Header()
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	header.Set("X-Frame-Options", "DENY")
	header.Set("X-Content-Type-Options", "nosniff")
	return e.Next()
}
