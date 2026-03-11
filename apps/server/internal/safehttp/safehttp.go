// Package safehttp builds the hardened HTTP clients every outbound fetch made on behalf of a user goes
// through.
package safehttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Every outbound fetch the server makes on behalf of a user goes through a client built here. The
// destination address is checked when the connection is made (after name resolution, for the initial
// request and for every redirect alike), so neither a hostname that resolves to a private address, nor
// a redirect to one, nor a DNS answer that changes between a check and the connect can reach the
// server's own network. Proxy environment variables are ignored: a proxy would be connected to
// instead of the destination and defeat the check.

var errUnsafeDestination = errors.New("destination is not a public address")

// safeNetworks are the IPv4/IPv6 ranges that are not public Internet destinations: private, loopback,
// link-local, shared (CGNAT), benchmarking, documentation, multicast, reserved and transition ranges
// that embed or translate to other addresses (NAT64, 6to4, Teredo).
var safeNetworks = func() []netip.Prefix {
	var prefixes []netip.Prefix
	for _, raw := range []string{
		// IPv4
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		// IPv6
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16",
		"3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	} {
		prefixes = append(prefixes, netip.MustParsePrefix(raw))
	}
	return prefixes
}()

// globalUnicastV6 is the only IPv6 block currently allocated for public use.
var globalUnicastV6 = netip.MustParsePrefix("2000::/3")

// publicIP reports whether ip is a routable address of the public Internet. An IPv4-mapped IPv6 address
// is judged as the IPv4 address it wraps.
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || ip.IsUnspecified() {
		return false
	}
	if ip.Is6() && !globalUnicastV6.Contains(ip) {
		return false
	}
	for _, prefix := range safeNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// Options tune a hardened client; the zero value is a strict general-purpose client.
type Options struct {
	// Timeout bounds a whole exchange including the body; zero means no overall bound (streaming).
	Timeout           time.Duration
	DialTimeout       time.Duration
	TLSTimeout        time.Duration
	HeaderTimeout     time.Duration
	MaxHeaderBytes    int64
	MaxRedirects      int
	DisableKeepAlives bool
	// Ports restricts the destination ports; empty allows any.
	Ports []string
	// CheckRedirect further validates each redirect target (the address is checked at connect time anyway).
	CheckRedirect func(request *http.Request, via []*http.Request) error

	// lookup and dial replace the system resolver and dialer; tests use them to stay offline.
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	dial   func(ctx context.Context, network, address string) (net.Conn, error)
}

// New builds a client from options.
func New(options Options) *http.Client {
	if options.DialTimeout == 0 {
		options.DialTimeout = 10 * time.Second
	}
	if options.TLSTimeout == 0 {
		options.TLSTimeout = 10 * time.Second
	}
	if options.HeaderTimeout == 0 {
		options.HeaderTimeout = 20 * time.Second
	}
	if options.MaxHeaderBytes == 0 {
		options.MaxHeaderBytes = 32 << 10
	}
	if options.MaxRedirects == 0 {
		options.MaxRedirects = 3
	}
	if options.lookup == nil {
		options.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	if options.dial == nil {
		dialer := &net.Dialer{Timeout: options.DialTimeout}
		options.dial = dialer.DialContext
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            options.safeDial,
		DisableKeepAlives:      options.DisableKeepAlives,
		DisableCompression:     true,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    options.TLSTimeout,
		ResponseHeaderTimeout:  options.HeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		IdleConnTimeout:        30 * time.Second,
		MaxIdleConns:           32,
		MaxIdleConnsPerHost:    4,
		MaxResponseHeaderBytes: options.MaxHeaderBytes,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   options.Timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > options.MaxRedirects {
				return errors.New("too many redirects")
			}
			if request.URL.Scheme != "https" && request.URL.Scheme != "http" {
				return errUnsafeDestination
			}
			if options.CheckRedirect != nil {
				return options.CheckRedirect(request, via)
			}
			return nil
		},
	}
}

// safeDial resolves the host itself and connects only to public addresses, trying them in order.
func (o Options) safeDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if len(o.Ports) != 0 {
		allowed := false
		for _, candidate := range o.Ports {
			allowed = allowed || candidate == port
		}
		if !allowed {
			return nil, errUnsafeDestination
		}
	}
	var candidates []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		candidates = []netip.Addr{literal}
	} else if candidates, err = o.lookup(ctx, host); err != nil {
		return nil, err
	}
	var last error = errUnsafeDestination
	for _, candidate := range candidates {
		if !publicIP(candidate) {
			continue
		}
		connection, err := o.dial(ctx, network, net.JoinHostPort(candidate.Unmap().WithZone("").String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	return nil, last
}

// Media returns the client for the media of the streams the server proxies: no overall deadline, since a
// segment or a progressive download is streamed to the viewer, but bounded connect and header waits.
func Media() *http.Client { return New(Options{MaxRedirects: 5, HeaderTimeout: 30 * time.Second}) }

// Public returns the client for small documents of public services (JSON APIs, RSS feeds, PeerTube).
func Public() *http.Client { return New(Options{Timeout: 25 * time.Second}) }
