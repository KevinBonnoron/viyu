package httpapi

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/accounts"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/chat"
	"github.com/KevinBonnoron/viyu/apps/server/internal/comments"
	"github.com/KevinBonnoron/viyu/apps/server/internal/companion"
	"github.com/KevinBonnoron/viyu/apps/server/internal/feed"
	"github.com/KevinBonnoron/viyu/apps/server/internal/jobs"
	"github.com/KevinBonnoron/viyu/apps/server/internal/live"
	"github.com/KevinBonnoron/viyu/apps/server/internal/media"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
	"github.com/KevinBonnoron/viyu/apps/server/internal/search"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
	"github.com/KevinBonnoron/viyu/apps/server/internal/sources"
	"github.com/KevinBonnoron/viyu/apps/server/internal/stats"
	"github.com/KevinBonnoron/viyu/apps/server/internal/subscriptions"
)

// Deps is everything the handlers call. The application composes it once; handlers never build a service.
type Deps struct {
	App           core.App
	Group         *jobs.Group
	Platforms     *platform.Registry
	Store         *catalog.Store
	Sources       *sources.Service
	Media         *media.Proxy
	Chat          *chat.Service
	Comments      *comments.Service
	Live          *live.Service
	Search        *search.Service
	Accounts      *accounts.Service
	Subscriptions *subscriptions.Service
	Companion     *companion.Service
	Feed          *feed.Service
	Stats         *stats.Service
	Refresher     *jobs.Refresher
	Catalogs      *jobs.Catalogs
	Analysis      *jobs.Analysis
	Series        *series.Store
	// OCR reads episode numbers from thumbnails; nil when this build has no recognition engine.
	OCR series.EpisodeReader
}
