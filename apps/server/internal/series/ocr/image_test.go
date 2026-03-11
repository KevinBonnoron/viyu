package ocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func clientFor(f roundTrip) *http.Client { return &http.Client{Transport: f} }

// textPNG draws text in black on white, the way a thumbnail would carry an episode label.
func textPNG(t testing.TB, text string) []byte {
	t.Helper()
	parsed, err := opentype.Parse(gobold.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 110, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	pixels := image.NewRGBA(image.Rect(0, 0, 800, 450))
	draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: pixels, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(220, 260)}
	drawer.DrawString(text)
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func grayPNG(t testing.TB, width, height int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

const goodURL = "https://i.ytimg.com/vi/id/hqdefault.jpg"

func TestAddressPolicyAcceptsOnlyPublicThumbnails(t *testing.T) {
	t.Parallel()
	f := newFetcher(Options{})
	valid := []string{
		"https://i.ytimg.com/vi/Abc_12-34/hqdefault.jpg",
		"https://i.ytimg.com:443/vi/Abc_12-34/maxresdefault.jpg?sqp=public&rs=signature",
		"https://img.youtube.com/vi/Abc_12-34/0.jpg",
		"https://i2.ytimg.com/vi_webp/Abc_12-34/hqdefault.webp",
	}
	for _, raw := range valid {
		if !f.valid(raw) {
			t.Fatalf("public thumbnail rejected: %s", raw)
		}
	}
	invalid := []string{
		"http://i.ytimg.com/vi/id/hqdefault.jpg", "https://i.ytimg.com:8443/vi/id/hqdefault.jpg",
		"https://i.ytimg.com.evil.test/vi/id/hqdefault.jpg", "https://evil.test/vi/id/hqdefault.jpg",
		"https://127.0.0.1/vi/id/hqdefault.jpg", "https://i.ytimg.com@127.0.0.1/vi/id/hqdefault.jpg",
		"https://name:secret@i.ytimg.com/vi/id/hqdefault.jpg", "file:///etc/passwd",
		"https://i.ytimg.com/../private.jpg", "https://i.ytimg.com/vi/id/%2e%2e.jpg",
		"https://i.ytimg.com/vi/id/hqdefault.jpg#fragment", "https://i.ytimg.com/vi/id/hqdefault.jpg?access_token=secret",
		"https://localhost/vi/id/hqdefault.jpg", "", "https://i.ytimg.com/vi/id/hqdefault.jpg?" + strings.Repeat("sqp=1&", 400),
	}
	for _, raw := range invalid {
		if f.valid(raw) {
			t.Fatalf("unsafe thumbnail accepted: %s", raw)
		}
	}
	for _, raw := range append(valid, invalid...) {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		request := &http.Request{URL: u, Header: http.Header{"Cookie": {"private"}, "Authorization": {"secret"}}}
		err = f.redirect(request, nil)
		if f.valid(raw) {
			if err != nil || request.Header.Get("Cookie") != "" || request.Header.Get("Authorization") != "" {
				t.Fatalf("redirect did not strip credentials: %v", err)
			}
		} else if err == nil {
			t.Fatalf("unsafe redirect accepted: %s", raw)
		}
	}
	u, _ := url.Parse(goodURL)
	if err := f.redirect(&http.Request{URL: u}, make([]*http.Request, 3)); err == nil {
		t.Fatal("unbounded thumbnail redirects")
	}
}

func TestAddressPolicyIsInjected(t *testing.T) {
	t.Parallel()
	f := newFetcher(Options{Hosts: []string{"img.example.test"}})
	if f.valid(goodURL) || !f.valid("https://img.example.test/vi/id/hqdefault.jpg") {
		t.Fatal("the hosts option does not replace the defaults")
	}
}

func TestDefaultClientIsTheBoundedHardenedClient(t *testing.T) {
	t.Parallel()
	f := newFetcher(Options{})
	if f.client.Transport == nil || f.client.Timeout == 0 || f.client.CheckRedirect == nil {
		t.Fatal("the thumbnail client is not the bounded hardened client")
	}
	// An injected client keeps its transport but never its redirect policy, and is not modified.
	own := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("unused") })}
	injected := newFetcher(Options{HTTP: own})
	if injected.client == own || own.CheckRedirect != nil || injected.client.CheckRedirect == nil {
		t.Fatal("the injected client's redirect policy was not replaced on a copy")
	}
}

func TestUnsafeAddressesNeverReachTheTransport(t *testing.T) {
	t.Parallel()
	f := newFetcher(Options{HTTP: clientFor(func(*http.Request) (*http.Response, error) {
		t.Error("unsafe URL reached the thumbnail transport")
		return nil, ErrImage
	})})
	for _, raw := range []string{"https://localhost/vi/id/hqdefault.jpg", "http://i.ytimg.com/vi/id/hqdefault.jpg", "https://i.ytimg.com:8080/vi/id/hqdefault.jpg", "https://user:password@i.ytimg.com/vi/id/hqdefault.jpg"} {
		if _, err := f.download(context.Background(), raw); !errors.Is(err, ErrImage) {
			t.Fatalf("unsafe input accepted: %s %v", raw, err)
		}
	}
}

func TestDownloadCarriesNoCredentialsAndLeaksNoDiagnostics(t *testing.T) {
	t.Parallel()
	fixture := textPNG(t, "EP07")
	f := newFetcher(Options{HTTP: clientFor(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" || request.Method != http.MethodGet {
			t.Error("thumbnail request forwarded credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(fixture)), Header: make(http.Header), Request: request}, nil
	})})
	data, err := f.download(context.Background(), goodURL)
	if err != nil || !bytes.Equal(data, fixture) {
		t.Fatalf("download=%d bytes err=%v", len(data), err)
	}
	for name, response := range map[string]*http.Response{
		"status":          {StatusCode: 403, Body: io.NopCloser(strings.NewReader("remote secret diagnostics"))},
		"declared length": {StatusCode: 200, ContentLength: maxBytes + 1, Body: io.NopCloser(strings.NewReader(""))},
		"streamed body":   {StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxBytes+1)))},
	} {
		f := newFetcher(Options{HTTP: clientFor(func(*http.Request) (*http.Response, error) { return response, nil })})
		_, err := f.download(context.Background(), goodURL)
		if !errors.Is(err, ErrImage) || strings.Contains(err.Error(), "remote") {
			t.Fatalf("%s: remote diagnostics leaked: %v", name, err)
		}
	}
	failing := newFetcher(Options{HTTP: clientFor(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp 10.0.0.1: secret remote error")
	})})
	if _, err := failing.download(context.Background(), goodURL); !errors.Is(err, ErrImage) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("transport diagnostics leaked: %v", err)
	}
}

func TestNormalizeBoundsAndValidatesImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	normalized, width, height, err := normalize(ctx, textPNG(t, "EP07"))
	if err != nil || width != 800 || height != 450 {
		t.Fatalf("normalize=%d %dx%d %v", len(normalized), width, height, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(normalized))
	if err != nil || format != "png" || config.Width != width || config.Height != height {
		t.Fatalf("re-encoded=%+v %q %v", config, format, err)
	}
	// A large thumbnail is scaled down to what the engine is given, keeping its proportions.
	_, width, height, err = normalize(ctx, grayPNG(t, maxWidth, maxHeight))
	if err != nil || width != engineWidth || height != engineHeight {
		t.Fatalf("scaled to %dx%d: %v", width, height, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, test := range map[string]struct {
		ctx  context.Context
		data []byte
	}{
		"cancelled":  {cancelled, textPNG(t, "EP07")},
		"empty":      {ctx, nil},
		"not image":  {ctx, []byte("not an image")},
		"too wide":   {ctx, grayPNG(t, maxWidth+1, minSide)},
		"too tall":   {ctx, grayPNG(t, minSide, maxHeight+1)},
		"too small":  {ctx, grayPNG(t, minSide-1, minSide)},
		"too large":  {ctx, bytes.Repeat([]byte{'x'}, maxBytes+1)},
		"gif format": {ctx, []byte("GIF89a\x20\x00\x20\x00\x00\x00\x00;")},
	} {
		if _, _, _, err := normalize(test.ctx, test.data); !errors.Is(err, ErrRecognition) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
}
