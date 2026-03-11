package ocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg" // registers the decoder
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the decoder

	"github.com/KevinBonnoron/viyu/apps/server/internal/safehttp"
)

const (
	maxBytes  = 4 << 20
	maxWidth  = 1920
	maxHeight = 1080
	minSide   = 32
	// engineWidth and engineHeight cap the pixels the recognition engine is given.
	engineWidth  = 960
	engineHeight = 540
)

var (
	// ErrImage is the failure of fetching a thumbnail: the address was refused, the host did not answer
	// or what it answered is not an acceptable image download.
	ErrImage = errors.New("thumbnail download unavailable")
	// ErrRecognition is the failure of reading a thumbnail: a context that ended, an image that is not
	// acceptable (format, size, pixels) or an engine that failed.
	ErrRecognition = errors.New("thumbnail recognition unavailable")
)

// Default policy: the public image service thumbnails come from. Hosts are matched exactly, so
// "i.ytimg.com.evil.test" is not one of them.
var (
	defaultPath      = regexp.MustCompile(`^/(?:vi|vi_webp)/[A-Za-z0-9_-]{1,64}/[A-Za-z0-9_-]+\.(?:jpg|jpeg|png|webp)$`)
	defaultHosts     = []string{"i.ytimg.com", "i1.ytimg.com", "i2.ytimg.com", "i3.ytimg.com", "i4.ytimg.com", "img.youtube.com"}
	defaultQueryKeys = []string{"sqp", "rs"}
)

// fetcher downloads the thumbnails the policy accepts. Both the initial URL and every redirect must stay
// on the allowed hosts. No source cookies, inbound headers or general-purpose media downloader are
// involved: the client carries nothing but what the request sets.
type fetcher struct {
	client    *http.Client
	hosts     []string
	path      *regexp.Regexp
	queryKeys []string
}

func newFetcher(opts Options) *fetcher {
	f := &fetcher{hosts: opts.Hosts, path: opts.Path, queryKeys: opts.QueryKeys}
	if len(f.hosts) == 0 {
		f.hosts = defaultHosts
	}
	if f.path == nil {
		f.path = defaultPath
	}
	if len(f.queryKeys) == 0 {
		f.queryKeys = defaultQueryKeys
	}
	if opts.HTTP != nil {
		// A copy, so the redirect policy is ours whatever client the caller injected.
		client := *opts.HTTP
		client.CheckRedirect = f.redirect
		f.client = &client
		return f
	}
	f.client = safehttp.New(safehttp.Options{
		Timeout:           12 * time.Second,
		DialTimeout:       4 * time.Second,
		TLSTimeout:        5 * time.Second,
		HeaderTimeout:     5 * time.Second,
		MaxHeaderBytes:    16 << 10,
		DisableKeepAlives: true,
		Ports:             []string{"443"},
		CheckRedirect:     f.redirect,
	})
	return f
}

// valid says whether raw is a thumbnail address the policy accepts: https on the default port, an allowed
// host, no credentials, fragment or opaque form, a plain image path and nothing but the allowed query keys.
func (f *fetcher) valid(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || len(raw) > 2048 {
		return false
	}
	if u.Port() != "" && u.Port() != "443" {
		return false
	}
	if !slices.Contains(f.hosts, u.Hostname()) {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		if !slices.Contains(f.queryKeys, key) {
			return false
		}
	}
	return u.RawPath == "" && f.path.MatchString(u.Path)
}

func (f *fetcher) redirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 3 || !f.valid(request.URL.String()) {
		return ErrImage
	}
	// Even though no credentials are attached initially, keep redirect requests
	// explicit rather than forwarding any unexpected transport-provided headers.
	request.Header = make(http.Header)
	request.Header.Set("Accept", "image/jpeg,image/png,image/webp")
	return nil
}

// download fetches one thumbnail of at most maxBytes. Whatever the host answers (status, body, errors)
// stays out of the error: nothing remote reaches a log or a client.
func (f *fetcher) download(ctx context.Context, raw string) ([]byte, error) {
	if !f.valid(raw) {
		return nil, ErrImage
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, ErrImage
	}
	request.Header.Set("Accept", "image/jpeg,image/png,image/webp")
	response, err := f.client.Do(request)
	if err != nil {
		return nil, ErrImage
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxBytes {
		return nil, ErrImage
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return nil, ErrImage
	}
	return data, nil
}

// normalize validates an untrusted image and re-encodes its pixels as a grayscale PNG of at most
// engineWidth x engineHeight, rather than handing an untrusted container to the native imaging library.
// It answers the size of the result.
func normalize(ctx context.Context, data []byte) (normalized []byte, width, height int, err error) {
	if ctx.Err() != nil || len(data) == 0 || len(data) > maxBytes {
		return nil, 0, 0, ErrRecognition
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") || config.Width < minSide || config.Height < minSide || config.Width > maxWidth || config.Height > maxHeight {
		return nil, 0, 0, ErrRecognition
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || ctx.Err() != nil {
		return nil, 0, 0, ErrRecognition
	}
	width, height = config.Width, config.Height
	if width > engineWidth || height > engineHeight {
		ratio := min(float64(engineWidth)/float64(width), float64(engineHeight)/float64(height))
		width, height = max(1, int(float64(width)*ratio)), max(1, int(float64(height)*ratio))
	}
	pixels := image.NewGray(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(pixels, pixels.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, pixels); err != nil || ctx.Err() != nil {
		return nil, 0, 0, ErrRecognition
	}
	return out.Bytes(), width, height, nil
}
