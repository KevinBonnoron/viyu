//go:build !ocr

package ocr

import "github.com/KevinBonnoron/viyu/apps/server/internal/series"

// New answers nil: this build has no recognition engine (build with -tags ocr, which needs CGO and
// Tesseract). A nil series.EpisodeReader means recognition is unavailable.
func New(Options) series.EpisodeReader { return nil }

// Available tells whether this build can read thumbnails.
func Available() bool { return false }
