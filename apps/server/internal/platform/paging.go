package platform

import (
	"math"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

const (
	// SearchPageSize is the number of results of one search page.
	SearchPageSize = 20
	// MaxListPage caps the page number of the paginated routes: each page deepens the upstream query, and
	// no viewer scrolls further.
	MaxListPage = 50
	// MaxPageSize caps the items of one feed page.
	MaxPageSize = 50
	// CookieLimit is the largest cookie file an account may import.
	CookieLimit = 256 << 10
)

// PageRange turns a page and a page size into the 1-based first and one-past-last positions of the page in
// the upstream listing.
func PageRange(page, limit int) (start, end int, err error) {
	if page < 1 || page > MaxListPage || limit < 1 || limit > MaxPageSize || page-1 > (math.MaxInt-limit-1)/limit {
		return 0, 0, apierr.InvalidInput("Use a page between 1 and 50 and a limit between 1 and 50.")
	}
	start = (page-1)*limit + 1
	return start, start + limit, nil
}
