//go:build ocr

package ocr

import (
	"context"
	"os"

	"github.com/otiai10/gosseract/v2"

	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
)

// New returns the reader that recognizes thumbnails with Tesseract.
func New(opts Options) series.EpisodeReader { return newReader(opts, tesseract{}) }

// Available tells whether this build can read thumbnails.
func Available() bool { return true }

// tesseract is the native engine. Each call owns its client: the library is not shared between calls.
type tesseract struct{}

func (tesseract) recognize(ctx context.Context, normalized []byte) ([]Word, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The legacy English classifier handles slashed zeros in compact labels that the default LSTM
	// classifier reads as the letter O. Gosseract exposes engine selection only through an
	// initialization config file.
	engineConfig, err := os.CreateTemp("", "viyu-thumbnail-ocr-*.conf")
	if err != nil {
		return nil, ErrRecognition
	}
	engineConfigPath := engineConfig.Name()
	defer os.Remove(engineConfigPath)
	if _, err := engineConfig.WriteString("tessedit_ocr_engine_mode 0\n"); err != nil {
		engineConfig.Close()
		return nil, ErrRecognition
	}
	if err := engineConfig.Close(); err != nil {
		return nil, ErrRecognition
	}
	client := gosseract.NewClient()
	defer client.Close()
	if err := client.SetConfigFile(engineConfigPath); err != nil {
		return nil, ErrRecognition
	}
	if err := client.DisableOutput(); err != nil {
		return nil, ErrRecognition
	}
	if err := client.SetLanguage("eng"); err != nil {
		return nil, ErrRecognition
	}
	if err := client.SetPageSegMode(gosseract.PSM_SPARSE_TEXT); err != nil {
		return nil, ErrRecognition
	}
	if err := client.SetImageFromBytes(normalized); err != nil {
		return nil, ErrRecognition
	}
	boxes, err := client.GetBoundingBoxesVerbose()
	if err != nil {
		return nil, ErrRecognition
	}
	words := make([]Word, len(boxes))
	for i, box := range boxes {
		words[i] = Word{Text: box.Word, Confidence: box.Confidence, Block: box.BlockNum, Paragraph: box.ParNum, Line: box.LineNum, Box: box.Box}
	}
	return words, nil
}
