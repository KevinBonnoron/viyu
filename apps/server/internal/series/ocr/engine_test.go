//go:build ocr

package ocr

import (
	"context"
	"testing"
)

// This is an actual native engine test, not a recognition mock: the image is drawn, validated, re-encoded
// and read by Tesseract. Only the HTTP boundary is replaced, so the test needs no network.
func TestTesseractReadsDrawnEpisodeLabels(t *testing.T) {
	t.Parallel()
	if !Available() {
		t.Fatal("the ocr build must offer recognition")
	}
	r := newReader(Options{}, tesseract{})
	episode, err := r.recognize(context.Background(), textPNG(t, "EP07"))
	if err != nil || episode != 7 {
		t.Fatalf("actual Tesseract EP07 result=%d error=%v", episode, err)
	}
	blank, err := r.recognize(context.Background(), textPNG(t, ""))
	if err != nil || blank != 0 {
		t.Fatalf("blank image manufactured an episode: %d %v", blank, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.recognize(cancelled, textPNG(t, "EP07")); err == nil {
		t.Fatal("cancelled recognition succeeded")
	}
	if New(Options{}) == nil {
		t.Fatal("New returned no reader")
	}
}
