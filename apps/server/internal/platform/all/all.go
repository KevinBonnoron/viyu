// Package all lists every platform Viyu supports, in display order (the first is the default). It is the one
// place a platform is added to the server: the schema's platform selects (internal/migrations) are tested
// against this registry, so a platform added here without its schema value, or the other way around, fails
// the build's tests.
package all

import (
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/peertube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/twitch"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/youtube"
)

// Options configure the platforms (endpoints, clients and the shared downloader runner).
type Options struct {
	YouTube  youtube.Options
	Twitch   twitch.Options
	PeerTube peertube.Options
}

// Registry builds the registry of every supported platform.
func Registry(o Options) *platform.Registry {
	return platform.NewRegistry(youtube.New(o.YouTube), twitch.New(o.Twitch), peertube.New(o.PeerTube))
}
