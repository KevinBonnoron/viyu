package series

import (
	"context"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// EpisodeReader reads an episode number off the thumbnails of a video (the images of one video, the main
// one first). It answers 0 when no number was found, and an error when the images could not be read at all.
// A nil reader means thumbnail recognition is unavailable in this build.
type EpisodeReader interface {
	Episode(ctx context.Context, thumbnailURLs []string) (int, error)
}

// OCREnabled tells whether an administrator allows episode numbers to be read from thumbnails.
func OCREnabled(app core.App) (bool, error) {
	var enabled bool
	err := app.DB().NewQuery("SELECT [[enabled]] FROM {{playlist_ocr_settings}} WHERE [[id]] = 1").Row(&enabled)
	return enabled, err
}

// SetOCREnabled stores the setting; changed tells whether it differs from what was stored. Pass a
// transaction to make the read and the write atomic with the caller's other work.
func SetOCREnabled(app core.App, enabled bool) (changed bool, err error) {
	err = app.RunInTransaction(func(tx core.App) error {
		previous, err := OCREnabled(tx)
		if err != nil {
			return err
		}
		if previous == enabled {
			return nil
		}
		changed = true
		_, err = tx.DB().NewQuery("UPDATE {{playlist_ocr_settings}} SET [[enabled]] = {:enabled} WHERE [[id]] = 1").Bind(map[string]any{"enabled": enabled}).Execute()
		return err
	})
	return changed && err == nil, err
}
