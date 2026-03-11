package catalog

import (
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// Metadata is the content of the metadata field of a videos record: what is known about a video only
// once its own page was fetched, and when that knowledge expires.
type Metadata struct {
	OriginalLanguage      string `json:"original_language"`
	OriginalTitleVerified bool   `json:"original_title_verified"`
	ChaptersLoaded        bool   `json:"chapters_loaded"`
	// MetadataExpiresAt is in the layout PocketBase stores dates in; empty means expired.
	MetadataExpiresAt string   `json:"metadata_expires_at"`
	LikeCount         *float64 `json:"like_count"`
	DislikeCount      *float64 `json:"dislike_count"`
}

// MetadataOf reads the metadata field of a videos record; absent keys are zero values.
func MetadataOf(r *core.Record) Metadata {
	var m Metadata
	_ = r.UnmarshalJSONField("metadata", &m)
	return m
}

// SetMetadata writes the metadata field of a videos record; the caller saves the record.
func SetMetadata(r *core.Record, m Metadata) {
	r.Set("metadata", m)
}

// Fresh tells whether the metadata is still valid.
func (m Metadata) Fresh() bool {
	expires, err := time.Parse(platform.StoredLayout, m.MetadataExpiresAt)
	return err == nil && expires.After(time.Now())
}

// ExpireMetadata makes the next opening of the video fetch its details again; the caller saves the record.
func ExpireMetadata(r *core.Record) {
	m := MetadataOf(r)
	m.MetadataExpiresAt = ""
	SetMetadata(r, m)
}

// translationLanguage is the language the platforms are asked in: yt-dlp sends Accept-Language: en, so the
// title a listing or page serves that is not the original one is the English one. The titles of a
// videos record are stored under it.
const translationLanguage = "en"

// originalTitleKey is the key of the original title in the titles field of a videos record, beside the
// translations keyed by language.
const originalTitleKey = "original"

// TitlesOf reads the titles field of a videos record: the title as served in translationLanguage (the
// original one when there is no translation) and the original title (the title when none is recorded).
func TitlesOf(r *core.Record) (title, original string) {
	var titles map[string]string
	_ = r.UnmarshalJSONField("titles", &titles)
	title = platform.First(titles[translationLanguage], titles[originalTitleKey])
	return title, platform.First(titles[originalTitleKey], title)
}

// SetTitles stores the titles of a video; the caller saves the record. An empty title is left out, and a
// translation equal to the original is not stored twice.
func SetTitles(r *core.Record, title, original string) {
	titles := map[string]string{}
	if original != "" {
		titles[originalTitleKey] = original
	}
	if title != "" && title != original {
		titles[translationLanguage] = title
	}
	r.Set("titles", titles)
}

// PublishedOf reads the publication of a videos record as Video.Published ("" when unknown).
func PublishedOf(r *core.Record) string {
	return platform.PublishedFromStored(r.GetDateTime("published").Time())
}
