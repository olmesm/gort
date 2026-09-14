package web

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type requestNetwork struct {
	ip     string
	scheme string
}
type requestNetworkKey struct{}

func (a *App) trustedProxy(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, prefix := range a.Cfg.TrustedProxies {
		if prefix.Contains(addr.Unmap()) {
			return true
		}
	}
	return false
}

func (a *App) requestNetwork(r *http.Request) requestNetwork {
	info := requestNetwork{ip: peerIP(r), scheme: "http"}
	if r.TLS != nil {
		info.scheme = "https"
	}
	if !a.trustedProxy(info.ip) {
		return info
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" || proto == "http" {
		info.scheme = proto
	}
	// Strip only trusted hops from the right. Taking the first entry lets a
	// client supply its own address before the proxy appends the real one.
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0 && a.trustedProxy(info.ip); i-- {
		ip := strings.TrimSpace(hops[i])
		if net.ParseIP(ip) == nil {
			info.ip = peerIP(r)
			break
		}
		info.ip = ip
	}
	return info
}

// browserSecurity protects cookie-authenticated forms without requiring token
// fields in each template. API-key clients keep their existing HTTP behavior.
func (a *App) browserSecurity(next http.Handler) http.Handler {
	csrf := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			w.Header().Set("Cache-Control", "no-store")
			if err := csrf.Check(r); err != nil {
				http.Error(w, "Cross-origin form submission is not allowed.", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		info := a.requestNetwork(r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestNetworkKey{}, info)))
	})
}
