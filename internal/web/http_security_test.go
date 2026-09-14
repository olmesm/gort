package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDashboardRejectsCrossOriginForms(t *testing.T) {
	app := newTestApp(t)
	for _, path := range []string{"/admin/login", "/admin/logout", "/admin/domains"} {
		for _, site := range []string{"cross-site", "same-site", ""} {
			t.Run(path+"/"+site, func(t *testing.T) {
				req := httptest.NewRequest("POST", "http://example.test"+path, strings.NewReader("username=admin&password=test-password-123"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", "https://evil.example.test")
				if site != "" {
					req.Header.Set("Sec-Fetch-Site", site)
				}
				rec := httptest.NewRecorder()
				app.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("cross-origin form returned %d", rec.Code)
				}
				if len(rec.Result().Cookies()) != 0 {
					t.Fatal("cross-origin form changed cookies")
				}
			})
		}
	}
	req := httptest.NewRequest("POST", "http://example.test/admin/login", strings.NewReader("username=admin&password=test-password-123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("same-origin login returned %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing dashboard cache/framing protection")
	}
}

func TestForwardedHeadersRequireTrustedPeer(t *testing.T) {
	cases := []struct{ name, trusted, peer, forwarded, proto, wantIP, wantScheme string }{
		{"untrusted peer", "", "192.0.2.20:1234", "8.8.8.8", "https", "192.0.2.20", "http"},
		{"trusted single proxy", "192.0.2.0/24", "192.0.2.20:1234", "8.8.8.8", "https", "8.8.8.8", "https"},
		{"spoofed leading hop", "192.0.2.0/24", "192.0.2.20:1234", "1.1.1.1, 8.8.8.8", "https", "8.8.8.8", "https"},
		{"trusted proxy chain", "192.0.2.0/24,198.51.100.0/24", "192.0.2.20:1234", "8.8.8.8, 198.51.100.20", "https", "8.8.8.8", "https"},
		{"invalid forwarded", "192.0.2.0/24", "192.0.2.20:1234", "8.8.8.8, garbage", "javascript", "192.0.2.20", "http"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestAppWithConfig(t, map[string]string{"TRUSTED_PROXIES": tc.trusted})
			req := httptest.NewRequest("GET", "http://example.test/admin", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			req.Header.Set("X-Forwarded-Proto", tc.proto)
			app.browserSecurity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := RemoteIP(r); got != tc.wantIP {
					t.Errorf("IP %q, want %q", got, tc.wantIP)
				}
				if got := requestScheme(r); got != tc.wantScheme {
					t.Errorf("scheme %q, want %q", got, tc.wantScheme)
				}
			})).ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}

func TestForwardedIPCannotBypassRateLimit(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"RATE_LIMIT_PER_MINUTE": "1"})
	for i, ip := range []string{"8.8.8.8", "1.1.1.1"} {
		req := httptest.NewRequest("POST", "http://example.test/graphql", strings.NewReader(`{"query":"{__typename}"}`))
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		want := 401
		if i == 1 {
			want = 429
		}
		if rec.Code != want {
			t.Fatalf("request %d: got %d, want %d", i, rec.Code, want)
		}
	}
}

func TestReturnURLRejectsBrowserAuthorityEscapes(t *testing.T) {
	for _, raw := range []string{"https://evil.example", "//evil.example", `/\evil.example`, "/\r\nLocation: https://evil.example", "/\t/evil.example"} {
		if got := safeReturnURL(raw); got != "/admin" {
			t.Errorf("accepted %q", raw)
		}
	}
	valid := "/admin/short-urls?search=" + url.QueryEscape("hello world")
	if got := safeReturnURL(valid); got != valid {
		t.Fatalf("safe return changed: %q", got)
	}
}

func TestInvalidTrustedProxyConfigFailsStartup(t *testing.T) {
	_, err := ConfigFromLookup(func(name string) (string, bool) {
		if name == "TRUSTED_PROXIES" {
			return "not-a-cidr", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("accepted malformed proxy network")
	}
}

func TestPasswordLoginAttemptsAreRateLimited(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"RATE_LIMIT_PER_MINUTE": "1"})
	client := app.client(t)
	if r := client.postForm("/admin/login", "username=admin&password=wrong"); r.Code == 429 {
		t.Fatal("first attempt was blocked")
	}
	client.headers["X-Forwarded-For"] = "203.0.113.42"
	if r := client.postForm("/admin/login", "username=admin&password=wrong"); r.Code != 429 {
		t.Fatalf("repeated login: %d", r.Code)
	}
}

func TestOIDCAccountsCannotUseLocalPasswordLogin(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.DB.Exec(t.Context(), "UPDATE users SET auth_source = 'oidc' WHERE username = 'admin'"); err != nil {
		t.Fatal(err)
	}
	r := app.client(t).postForm("/admin/login", "username=admin&password=test-password-123")
	if r.Code != http.StatusUnauthorized || len(r.Result().Cookies()) != 0 {
		t.Fatalf("OIDC account accepted password: %d", r.Code)
	}
}
