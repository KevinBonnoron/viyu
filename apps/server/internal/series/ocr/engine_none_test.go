//go:build !ocr

package ocr

import "testing"

func TestWithoutTheEngineRecognitionIsUnavailable(t *testing.T) {
	t.Parallel()
	if Available() || New(Options{}) != nil {
		t.Fatal("the default build must offer no reader")
	}
}
