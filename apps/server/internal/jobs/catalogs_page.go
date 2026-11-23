package jobs

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"io"
	"slices"
	"strings"
	"unicode"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"golang.org/x/text/unicode/norm"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

type CatalogPage struct {
	Catalog
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
	HasMore  bool   `json:"hasMore"`
	Total    int    `json:"total"`
	Revision string `json:"revision"`
}

func revisionFields(h hash.Hash, fields ...string) {
	var length [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = h.Write(length[:])
		_, _ = io.WriteString(h, field)
	}
}

func revisionHash(key CatalogKey) hash.Hash {
	h := sha256.New()
	revisionFields(h, key.Platform, key.ChannelID, key.UserID, key.Revision)
	return h
}

func revisionString(h hash.Hash) string {
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Catalogs) publicRevision(key CatalogKey, catalogID string) (string, error) {
	h := revisionHash(key)
	rows, err := c.app.DB().NewQuery(`SELECT v.id, COALESCE(v.published, '') AS published, COALESCE(v.titles, '') AS titles
		FROM channel_catalog_entries e JOIN videos v ON v.id = e.video
		WHERE e.catalog = {:id} AND json_array_length(COALESCE(NULLIF(v.owners, ''), '[]')) = 0
		ORDER BY v.id`).Bind(dbx.Params{"id": catalogID}).Rows()
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, published, titles string
		if err := rows.Scan(&id, &published, &titles); err != nil {
			return "", err
		}
		revisionFields(h, id, published, titles)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return revisionString(h), nil
}

func foldTitle(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func titleMatches(title, original, query string) bool {
	return strings.Contains(foldTitle(title), query) || strings.Contains(foldTitle(original), query)
}

// PageVideos selects a page from an already loaded listing.
func PageVideos(videos []platform.Video, page, limit int, query string, ascending bool) ([]platform.Video, int) {
	slices.SortFunc(videos, func(a, b platform.Video) int {
		if a.Published == "" && b.Published != "" {
			return 1
		}
		if a.Published != "" && b.Published == "" {
			return -1
		}
		date := platform.ComparePublished(a.Published, b.Published)
		if !ascending {
			date = -date
		}
		if date != 0 {
			return date
		}
		if id := cmp.Compare(a.ID, b.ID); id != 0 {
			return id
		}
		return cmp.Compare(a.RecordID, b.RecordID)
	})
	query = foldTitle(strings.TrimSpace(query))
	out := make([]platform.Video, 0, limit)
	total := 0
	start := (page - 1) * limit
	for _, video := range videos {
		if query != "" && !titleMatches(video.Title, video.OriginalTitle, query) {
			continue
		}
		if total >= start && len(out) < limit {
			out = append(out, video)
		}
		total++
	}
	return out, total
}

// Snapshot starts synchronization and reads a bounded page of the catalog.
func (c *Catalogs) Snapshot(key CatalogKey, opts SnapshotOptions) (CatalogPage, error) {
	page, limit, query := opts.Page, opts.Limit, opts.Query
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}
	key, _, err := c.resolve(key)
	if err != nil {
		return CatalogPage{}, err
	}
	if key.Connected() {
		all, err := c.snapshotConnected(key, opts.Retry)
		if err != nil {
			return CatalogPage{}, err
		}
		h := revisionHash(key)
		for _, video := range all.Videos {
			revisionFields(h, video.RecordID, video.ID, video.Published, video.Title, video.OriginalTitle)
		}
		revision := revisionString(h)
		videos, total := PageVideos(all.Videos, page, limit, query, opts.Ascending)
		all.Videos = videos
		return CatalogPage{Catalog: all, Page: page, Limit: limit, Total: total, HasMore: page <= total/limit && page*limit < total, Revision: revision}, nil
	}
	admitted := c.ensurePublic(key, opts.Retry)
	row, err := c.row(c.app, key)
	if err != nil {
		return CatalogPage{}, apierr.Internal("Failed to get channel data", err)
	}
	out := CatalogPage{Catalog: Catalog{Channel: c.channelOf(key, row), Videos: []platform.Video{}, Sync: Sync{Status: reported(c.statusOf(key, row))}}, Page: page, Limit: limit}
	if !admitted {
		out.Sync.Status = Failed
	}
	if row == nil {
		out.Revision = revisionString(revisionHash(key))
		return out, nil
	}
	out.Revision, err = c.publicRevision(key, row.Id)
	if err != nil {
		return CatalogPage{}, apierr.Internal("Failed to get channel data", err)
	}
	var count int
	err = c.app.DB().NewQuery(`SELECT COUNT(*) FROM channel_catalog_entries e JOIN videos v ON v.id = e.video WHERE e.catalog = {:id} AND json_array_length(COALESCE(NULLIF(v.owners, ''), '[]')) = 0`).Bind(dbx.Params{"id": row.Id}).Row(&count)
	if err != nil {
		return CatalogPage{}, apierr.Internal("Failed to get channel data", err)
	}
	out.Sync.Loaded = count
	if strings.TrimSpace(query) != "" {
		records, err := c.records(row)
		if err != nil {
			return CatalogPage{}, apierr.Internal("Failed to get channel data", err)
		}
		if opts.Ascending {
			slices.SortFunc(records, func(a, b *core.Record) int {
				first, second := catalog.PublishedOf(a), catalog.PublishedOf(b)
				if first == "" && second != "" {
					return 1
				}
				if first != "" && second == "" {
					return -1
				}
				if date := platform.ComparePublished(first, second); date != 0 {
					return date
				}
				return cmp.Compare(a.GetString("platform_id"), b.GetString("platform_id"))
			})
		}
		needle := foldTitle(strings.TrimSpace(query))
		selected := make([]*core.Record, 0, limit)
		start := (page - 1) * limit
		for _, r := range records {
			title, original := catalog.TitlesOf(r)
			if !titleMatches(title, original, needle) {
				continue
			}
			if out.Total >= start && len(selected) < limit {
				selected = append(selected, r)
			}
			out.Total++
		}
		out.Videos = c.store.VideosOf(selected)
	} else {
		out.Total = count
		records := []*core.Record{}
		order := "videos.published DESC"
		if opts.Ascending {
			order = "videos.published ASC"
		}
		if (page - 1) <= count/limit {
			err = c.app.RecordQuery("videos").InnerJoin(entriesCollection, dbx.NewExp("[[channel_catalog_entries.video]] = [[videos.id]]")).
				AndWhere(dbx.HashExp{"channel_catalog_entries.catalog": row.Id}).
				AndWhere(dbx.NewExp("json_array_length(COALESCE(NULLIF([[videos.owners]], ''), '[]')) = 0")).
				OrderBy("CASE WHEN [[videos.published]] IS NULL OR [[videos.published]] = '' THEN 1 ELSE 0 END", order, "videos.platform_id").Limit(int64(limit)).Offset(int64((page - 1) * limit)).All(&records)
			if err != nil {
				return CatalogPage{}, apierr.Internal("Failed to get channel data", err)
			}
		}
		out.Videos = c.store.VideosOf(records)
	}
	revision, err := c.publicRevision(key, row.Id)
	if err != nil {
		return CatalogPage{}, apierr.Internal("Failed to get channel data", err)
	}
	if revision != out.Revision {
		out.Revision = ""
	}
	out.HasMore = page <= out.Total/limit && page*limit < out.Total
	return out, nil
}
