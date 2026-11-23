package media

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestResourceTokensRoundTripAndScope(t *testing.T) {
	var tokens, other resourceTokens
	want := resourceToken{
		URL:        "https://example.com/segment?signature=secret-value",
		Parent:     "https://example.com/playlist?token=parent-secret",
		Semantic:   "bytes=100-199",
		Generation: 42,
	}
	id, err := tokens.seal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "s.") || strings.Contains(id, want.URL) || strings.Contains(id, "secret-value") {
		t.Fatalf("token exposes or misidentifies its payload: %q", id)
	}
	if got, ok := tokens.open(id); !ok || got != want {
		t.Fatalf("round trip: got %#v, %v; want %#v", got, ok, want)
	}
	if _, ok := other.open(id); ok {
		t.Fatal("another scope accepted the token")
	}
	second, err := tokens.seal(want)
	if err != nil {
		t.Fatal(err)
	}
	if second == id {
		t.Fatal("repeated payload reused its token")
	}
}

func TestResourceTokensRejectInvalid(t *testing.T) {
	var tokens resourceTokens
	id, err := tokens.seal(resourceToken{URL: "https://example.com/a", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(id[2:])
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	tampered := "s." + base64.RawURLEncoding.EncodeToString(sealed)
	for _, bad := range []string{"", "s.", "x." + id[2:], "s.!", tampered, "s." + strings.Repeat("a", maxResourceTokenSize)} {
		if _, ok := tokens.open(bad); ok {
			t.Fatalf("accepted invalid token %q", bad[:min(len(bad), 80)])
		}
	}
	for _, payload := range []string{`{}`, `{"u":"https://example.com"}`, `{"g":1}`, `{"u":"file:///etc/passwd","g":1}`, `{"u":"https:///missing-host","g":1}`, `not-json`} {
		nonce := make([]byte, tokens.aead.NonceSize())
		ciphertext := tokens.aead.Seal(nonce, nonce, []byte(payload), nil)
		if _, ok := tokens.open("s." + base64.RawURLEncoding.EncodeToString(ciphertext)); ok {
			t.Fatalf("accepted invalid payload %q", payload)
		}
	}
	for _, value := range []resourceToken{
		{},
		{URL: "https://example.com"},
		{Generation: 1},
		{URL: "file:///etc/passwd", Generation: 1},
		{URL: "https:///missing-host", Generation: 1},
		{URL: strings.Repeat("a", maxResourceTokenSize), Generation: 1},
	} {
		if _, err := tokens.seal(value); err == nil {
			t.Fatal("accepted invalid or oversized payload")
		}
	}
}

func TestResourceTokensConcurrent(t *testing.T) {
	var tokens resourceTokens
	const count = 64
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := resourceToken{URL: fmt.Sprintf("https://example.com/%d", i), Generation: 1}
			id, err := tokens.seal(want)
			if err != nil {
				errs <- err
				return
			}
			if got, ok := tokens.open(id); !ok || got != want {
				errs <- fmt.Errorf("round trip: got %#v, %v; want %#v", got, ok, want)
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Error(err)
	}
	seen := make(map[string]bool, count)
	for id := range ids {
		if seen[id] {
			t.Error("duplicate token")
		}
		seen[id] = true
	}
}
