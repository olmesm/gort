package web

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/olmesm/gort/internal/core"
)

func TestOutboundRejectsNonPublicAddresses(t *testing.T) {
	for _, raw := range []string{"0.0.0.0", "0.1.2.3", "10.1.2.3", "100.64.0.1", "127.0.0.1", "169.254.169.254", "168.63.129.16", "172.16.0.1", "192.168.1.2", "192.0.2.1", "198.18.0.1", "224.0.0.1", "255.255.255.255", "::", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "ff02::1", "64:ff9b::a00:1", "2002:7f00:1::", "2001:db8::1"} {
		t.Run(raw, func(t *testing.T) {
			if publicOutboundIP(netip.MustParseAddr(raw)) {
				t.Fatal("accepted non-public address")
			}
		})
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2001:4860:4860::8888"} {
		if !publicOutboundIP(netip.MustParseAddr(raw)) {
			t.Errorf("rejected public address %s", raw)
		}
	}
}

func TestOutboundPinsDNSResultAndRejectsMixedAnswers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	lookups := 0
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	conn, err := outboundDialer(true, lookup)(t.Context(), "tcp", net.JoinHostPort("does-not-resolve.invalid", port))
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if lookups != 1 {
		t.Fatalf("resolved %d times", lookups)
	}
	mixed := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	if conn, err := outboundDialer(false, mixed)(t.Context(), "tcp", "mixed.invalid:80"); err == nil {
		conn.Close()
		t.Fatal("accepted mixed public/private DNS")
	}
}

type outboundTestTransport func(*http.Request) (*http.Response, error)

func (f outboundTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOutboundBlocksLoopbackAndRedirectsWithoutProxyEscape(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<title>internal title</title>")
	}))
	defer target.Close()
	t.Setenv("HTTP_PROXY", target.URL)
	client := newOutboundClient(time.Second, false)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, target.URL, strings.NewReader("event"))
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			t.Fatal("accessed loopback")
		}
	}
	realTransport := client.Transport
	client.Transport = outboundTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "public.example" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{target.URL}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return realTransport.RoundTrip(r)
	})
	if resp, err := client.Get("http://public.example/start"); err == nil {
		resp.Body.Close()
		t.Fatal("followed redirect to loopback")
	}
	if calls.Load() != 0 {
		t.Fatalf("internal server received %d requests", calls.Load())
	}
	allowed := newOutboundClient(time.Second, true)
	resp, err := allowed.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls.Load() != 1 {
		t.Fatal("private-network opt-in did not permit request")
	}
}

func TestTitleClientBlocksPrivateDestinationsByDefault(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<title>internal title</title>")
	}))
	defer target.Close()
	longURL, err := core.NewLongURL(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, allow := range []string{"false", "true"} {
		t.Run(allow, func(t *testing.T) {
			app := newTestAppWithConfig(t, map[string]string{"ALLOW_PRIVATE_OUTBOUND": allow})
			got := app.TryFetchTitle(t.Context(), longURL)
			if allow == "false" && got != "" {
				t.Fatalf("private title leaked: %q", got)
			}
			if allow == "true" && got != "internal title" {
				t.Fatalf("opt-in title %q", got)
			}
		})
	}
}
