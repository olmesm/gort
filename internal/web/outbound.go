package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// These special-use networks can route to internal services or translate an
// apparently public address into a private one. Private, local and multicast
// addresses are rejected separately below.
var specialOutboundNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	// Azure's platform virtual address is globally numbered but reaches host
	// infrastructure from inside an Azure network.
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func publicOutboundIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range specialOutboundNetworks {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

type outboundResolver func(context.Context, string, string) ([]netip.Addr, error)

// Resolve once and dial that exact IP: validation before a second DNS lookup
// would allow DNS rebinding. The transport applies this to every redirect too.
func outboundDialer(allowPrivate bool, lookup outboundResolver) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("no addresses for outbound host %q", host)
		}
		for _, addr := range addresses {
			if !allowPrivate && !publicOutboundIP(addr) {
				return nil, fmt.Errorf("outbound destination %q resolves to a non-public address", host)
			}
		}
		for _, addr := range addresses {
			var conn net.Conn
			conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, err
	}
}

func newOutboundClient(timeout time.Duration, allowPrivate bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy would resolve and dial the destination itself, bypassing these
	// checks. User-controlled destinations must use the checked direct dialer.
	transport.Proxy = nil
	transport.DialContext = outboundDialer(allowPrivate, net.DefaultResolver.LookupNetIP)
	return &http.Client{Transport: transport, Timeout: timeout}
}
