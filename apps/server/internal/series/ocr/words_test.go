package ocr

import (
	"image"
	"testing"
)

func testWord(text string, confidence float64, line, x, y, size int) Word {
	return Word{Text: text, Confidence: confidence, Block: 1, Paragraph: 1, Line: line, Box: image.Rect(x, y, x+max(20, len(text)*size/2), y+size)}
}

func TestParseEpisodeIsConservative(t *testing.T) {
	t.Parallel()
	word := func(text string) Word { return testWord(text, 97, 1, 100, 60, 64) }
	second := func(text string) Word { return testWord(text, 97, 2, 100, 150, 64) }
	cases := []struct {
		name  string
		words []Word
		want  int
	}{
		{"explicit compact", []Word{word("EP07")}, 7},
		{"explicit word", []Word{word("Episode"), testWord("07", 97, 1, 340, 60, 64)}, 7},
		{"explicit hash", []Word{word("#07")}, 7},
		{"explicit colon", []Word{word("EP:07")}, 7},
		{"label confidence matters", []Word{testWord("EP", 40, 1, 100, 60, 64), testWord("07", 98, 1, 220, 60, 64)}, 0},
		{"weak digits", []Word{testWord("EP07", 60, 1, 100, 60, 64)}, 0},
		{"isolated large digit", []Word{word("07")}, 7},
		{"small digit", []Word{testWord("7", 99, 1, 100, 60, 20)}, 0},
		{"uncertain digit", []Word{testWord("7", 91, 1, 100, 60, 64)}, 0},
		{"duration corner", []Word{testWord("7", 99, 1, 500, 280, 64)}, 0},
		{"clock", []Word{word("07:30")}, 0},
		{"clock after label", []Word{word("EP07:30")}, 0},
		{"clock with repeated digits after label", []Word{word("EP07:07")}, 0},
		{"decimal after label", []Word{word("EP7.7")}, 0},
		{"year", []Word{word("2024")}, 0},
		{"duration", []Word{word("7m")}, 0},
		{"duration word", []Word{word("7 minutes")}, 0},
		{"quality", []Word{word("4K")}, 0},
		{"conflicting explicit", []Word{word("EP07"), second("EP08")}, 0},
		{"conflicting same line", []Word{word("EP07 EP08")}, 0},
		{"conflicting bare", []Word{word("7"), second("8")}, 0},
		{"unrelated number before label", []Word{word("8"), second("EP07")}, 0},
		{"unrelated number after label", []Word{word("EP07"), second("8")}, 0},
		{"label plus clock", []Word{word("EP07"), second("12:34")}, 7},
		{"label plus year", []Word{word("EP07"), second("2024")}, 7},
		{"bare plus clock", []Word{word("7"), second("12:34")}, 0},
		{"bare plus year", []Word{word("7"), second("2024")}, 0},
		{"not isolated", []Word{word("TOP"), testWord("7", 99, 1, 250, 60, 64)}, 0},
		{"separate ambiguous caption", []Word{word("7"), second("TOP")}, 0},
		{"zero", []Word{word("EP00")}, 0},
		{"no guessed substitution", []Word{word("EPO7")}, 0},
		{"no number", []Word{word("HELLO")}, 0},
		{"non-breaking space is blank", []Word{word("\u00a0")}, 0},
		{"empty", nil, 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseEpisode(test.words, 640, 360); got != test.want {
				t.Fatalf("episode=%d want=%d", got, test.want)
			}
		})
	}
}
