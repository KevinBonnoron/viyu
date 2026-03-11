package safehttp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPublicIPRejectsEveryReservedRange(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "0.1.2.3", "10.0.0.1", "100.64.0.1", "100.100.100.200", "100.127.255.255", "127.0.0.1", "127.255.255.254",
		"169.254.169.254", "172.16.0.1", "172.31.255.255", "192.0.0.1", "192.0.2.1", "192.88.99.1", "192.168.1.1",
		"198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "224.0.0.1", "239.255.255.250", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::127.0.0.1",
		"64:ff9b::7f00:1", "64:ff9b:1::1", "100::1", "2001::1", "2001:0:4136:e378::1", "2001:db8::1", "2002:7f00:1::1", "2002:a00:1::",
		"fc00::1", "fd12:3456::1", "fe80::1", "fec0::1", "ff02::1", "3fff::1", "5f00::1",
	} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("non-public address accepted: %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "142.250.1.1", "1.1.1.1", "100.63.255.255", "198.17.255.255", "223.255.255.255", "2606:4700:4700::1111", "2a00:1450:4007:80b::200e", "::ffff:8.8.8.8"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("public address rejected: %s", raw)
		}
	}
}

// loopbackServer counts what reaches it: a hardened client must never get here.
func loopbackServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte("internal"))
	}))
	t.Cleanup(server.Close)
	return server, &reached
}

func TestSafeClientRefusesPrivateDestinations(t *testing.T) {
	server, reached := loopbackServer(t)
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	client := New(Options{})
	for _, target := range []string{
		server.URL,
		"http://localhost:" + port + "/",
		"http://[::1]:" + port + "/",
		"http://[::ffff:127.0.0.1]:" + port + "/",
		"http://127.1:" + port + "/",
		"http://2130706433:" + port + "/",
		"http://169.254.169.254/latest/meta-data/",
		"http://100.100.100.200/latest/meta-data/",
	} {
		response, err := client.Get(target)
		if err == nil {
			response.Body.Close()
			t.Errorf("%s was fetched", target)
		}
	}
	if reached.Load() != 0 {
		t.Fatalf("%d request(s) reached a private address", reached.Load())
	}
}

func TestSafeClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	transport, ok := New(Options{}).Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("the client would send its requests through a proxy, which the destination check cannot see")
	}
}

// fakeNetwork resolves two names, one public and one private, and connects every dial to one local server
// while recording which address the client believed it was dialing.
type fakeNetwork struct {
	target string
	mu     sync.Mutex
	dialed []string
}

func (n *fakeNetwork) options() Options {
	return Options{
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			switch host {
			case "public.test":
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			case "private.test":
				return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
			case "mixed.test":
				return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("93.184.216.35")}, nil
			}
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			n.mu.Lock()
			n.dialed = append(n.dialed, address)
			n.mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, n.target)
		},
	}
}

func (n *fakeNetwork) dials() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.dialed...)
}

func TestSafeClientRevalidatesEveryRedirectTarget(t *testing.T) {
	var secret atomic.Int32
	var port string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "http://private.test:"+port+"/secret", http.StatusFound)
		case "/literal":
			http.Redirect(w, r, "http://127.0.0.1:"+port+"/secret", http.StatusFound)
		case "/secret":
			secret.Add(1)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	_, port, _ = net.SplitHostPort(server.Listener.Addr().String())
	network := &fakeNetwork{target: server.Listener.Addr().String()}
	client := New(network.options())

	// The public name is reachable...
	response, err := client.Get("http://public.test:" + port + "/plain")
	if err != nil {
		t.Fatalf("the public destination was refused: %v", err)
	}
	response.Body.Close()
	if dials := network.dials(); len(dials) != 1 || dials[0] != "93.184.216.34:"+port {
		t.Fatalf("dialed %v", dials)
	}
	// ...but not the private host it redirects to, whether named or a literal address.
	for _, path := range []string{"/start", "/literal"} {
		response, err = client.Get("http://public.test:" + port + path)
		if err == nil {
			response.Body.Close()
			t.Fatalf("the redirect from %s to a private address was followed", path)
		}
	}
	if secret.Load() != 0 {
		t.Fatal("a request reached the private target after a redirect")
	}
	for _, dial := range network.dials() {
		if dial == "10.0.0.5:"+port || dial == "127.0.0.1:"+port {
			t.Fatalf("a private address was dialed: %s", dial)
		}
	}
	// A name with a private and a public address connects to the public one only.
	network.dialed = nil
	response, err = client.Get("http://mixed.test:" + port + "/plain")
	if err != nil {
		t.Fatalf("a name with a public address was refused: %v", err)
	}
	response.Body.Close()
	if dials := network.dials(); len(dials) != 1 || dials[0] != "93.184.216.35:"+port {
		t.Fatalf("mixed name dialed %v", dials)
	}
}

func TestSafeClientBoundsRedirectsAndPorts(t *testing.T) {
	var port string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://public.test:"+port+"/again", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	_, port, _ = net.SplitHostPort(server.Listener.Addr().String())
	network := &fakeNetwork{target: server.Listener.Addr().String()}
	options := network.options()
	options.MaxRedirects = 2
	if response, err := New(options).Get("http://public.test:" + port + "/"); err == nil {
		response.Body.Close()
		t.Fatal("an endless redirect chain was followed")
	}
	options = network.options()
	options.Ports = []string{"443"}
	network.dialed = nil
	if response, err := New(options).Get("http://public.test:" + port + "/"); err == nil {
		response.Body.Close()
		t.Fatal("a port outside the allowed list was dialed")
	}
	if len(network.dials()) != 0 {
		t.Fatalf("dialed %v", network.dials())
	}
}
