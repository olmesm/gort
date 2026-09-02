package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

// The public-facing side: short URL redirects, base URL, robots.txt, QR
// codes.

func notFoundPage(message string) string {
	return h.Document(h.E("html", []h.Attr{h.A("lang", "en")},
		h.E("head", nil,
			h.E("meta", []h.Attr{h.A("charset", "utf-8")}),
			h.E("title", nil, h.Text("Not found")),
			h.E("style", nil, h.Raw(
				"body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#0b0d13;color:#e7eaf3}"+
					"main{text-align:center;padding:2rem}h1{font-size:4rem;margin:0}p{color:#6b7385}"))),
		h.E("body", nil,
			h.E("main", nil,
				h.E("h1", nil, h.Text("404")),
				h.E("p", nil, h.Text(message))))))
}

func respondNotFound(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(notFoundPage(message)))
}

// redirectWith redirects with an arbitrary 3xx status code.
func redirectWith(w http.ResponseWriter, status core.RedirectStatus, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(status.Code())
}

func landingPage() string {
	return h.Document(h.E("html", []h.Attr{h.A("lang", "en")},
		h.E("head", nil,
			h.E("meta", []h.Attr{h.A("charset", "utf-8")}),
			h.E("title", nil, h.Text("Gort")),
			h.E("style", nil, h.Raw(
				"body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#0b0d13;color:#e7eaf3}"+
					"main{text-align:center;padding:2rem}a{color:#818cf8}"))),
		h.E("body", nil,
			h.E("main", nil,
				h.E("h1", nil, h.Text("Gort")),
				h.E("p", nil, h.Text("A self-hosted URL shortener.")),
				h.E("p", nil, h.E("a", []h.Attr{h.A("href", "/admin")}, h.Text("Open the dashboard")))))))
}

// GET /rest/health — no auth; checks database connectivity.
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	const version = "1.0.0"
	var one int64
	if err := a.Db.QueryRow("SELECT 1").Scan(&one); err != nil {
		RespondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "fail", "version": version})
		return
	}
	RespondJSON(w, http.StatusOK, map[string]string{"status": "pass", "version": version})
}

// GET / — orphan-tracked; redirects when a base-url redirect is configured.
func (a *App) handleBaseUrl(w http.ResponseWriter, r *http.Request) {
	domain, err := a.ResolveRequestDomain(r.Host)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.RecordVisit(r, core.VisitOrphanBaseUrl, nil, nil)

	target := ""
	if domain.BaseUrlRedirect != nil {
		target = *domain.BaseUrlRedirect
	} else if a.Cfg.BaseUrlRedirect != "" {
		target = a.Cfg.BaseUrlRedirect
	}
	if target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(landingPage()))
}

// GET /robots.txt — disallow everything except crawlable short URLs and the
// base URL.
func (a *App) handleRobots(w http.ResponseWriter, r *http.Request) {
	crawlable, err := data.ListCrawlable(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	lines := []string{"User-agent: *"}
	for _, code := range crawlable {
		lines = append(lines, "Allow: /"+code)
	}
	lines = append(lines, "Allow: /$", "Disallow: /")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
}

// GET /{code}/qr-code — public QR code for an existing short URL.
func (a *App) handleQrCode(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	domain, err := a.ResolveRequestDomain(r.Host)
	if err != nil {
		a.serverError(w, err)
		return
	}
	shortUrl, err := data.TryGetByCode(a.Db, core.DomainID(domain.Id), code)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if shortUrl == nil {
		respondNotFound(w, "There is no short URL to encode.")
		return
	}
	q := r.URL.Query()
	opts := ParseQrOptions(queryInt(q, "size"), queryInt(q, "margin"),
		q.Get("errorCorrection"), q.Get("format"))
	content := ShortUrlFor(a.Cfg, domain.Authority, shortUrl.ShortCode)
	RespondQr(w, content, opts)
}

func visitorContextOf(r *http.Request) core.VisitorContext {
	query := map[string]string{}
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		} else {
			query[key] = ""
		}
	}
	return core.VisitorContext{
		UserAgent:      r.Header.Get("User-Agent"),
		AcceptLanguage: r.Header.Get("Accept-Language"),
		Query:          query,
		RemoteIP:       RemoteIP(r),
	}
}

// looksLikeShortCode: does a missed path look like a short code the visitor
// mistyped (as opposed to a scanner probing /wp-admin/setup.php and the
// like)? Single path segment, code-like length, no dots.
func looksLikeShortCode(slug string) bool {
	return !strings.Contains(slug, "/") &&
		!strings.Contains(slug, ".") &&
		len(slug) <= 64 &&
		core.IsValidSlug(slug)
}

// handleInvalid handles a missing/inactive short URL: orphan tracking +
// configured fallbacks.
func (a *App) handleInvalid(w http.ResponseWriter, r *http.Request, slug string) {
	domain, err := a.ResolveRequestDomain(r.Host)
	if err != nil {
		a.serverError(w, err)
		return
	}
	isCodeLike := looksLikeShortCode(slug)

	visitType := core.VisitOrphanRegular404
	if isCodeLike {
		visitType = core.VisitOrphanInvalidShort
	}
	a.RecordVisit(r, visitType, nil, nil)

	target := ""
	if isCodeLike {
		if domain.InvalidShortUrlRedirect != nil {
			target = *domain.InvalidShortUrlRedirect
		} else {
			target = a.Cfg.InvalidShortUrlRedirect
		}
	} else {
		if domain.Regular404Redirect != nil {
			target = *domain.Regular404Redirect
		} else {
			target = a.Cfg.Regular404Redirect
		}
	}
	if target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	respondNotFound(w, "This short URL does not exist.")
}

// GET /{...} — the redirect hot path.
func (a *App) handleShortUrl(w http.ResponseWriter, r *http.Request) {
	slug := strings.Trim(r.URL.Path, "/")
	if slug == "" {
		a.handleBaseUrl(w, r)
		return
	}

	domain, err := a.ResolveRequestDomain(r.Host)
	if err != nil {
		a.serverError(w, err)
		return
	}
	found, err := data.TryGetByCode(a.Db, core.DomainID(domain.Id), slug)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if found == nil {
		a.handleInvalid(w, r, slug)
		return
	}

	id := core.ShortUrlID(found.Id)
	lifetime := LifetimeOfRow(found)

	var visitCount int64
	if lifetime.MaxVisits != nil {
		visitCount, err = data.CountValidVisits(a.Db, id)
		if err != nil {
			a.serverError(w, err)
			return
		}
	}

	if active, _ := lifetime.CheckActive(time.Now().UTC(), visitCount); !active {
		a.handleInvalid(w, r, slug)
		return
	}

	visitor := visitorContextOf(r)
	rules, err := data.GetRules(a.Db, id)
	if err != nil {
		a.serverError(w, err)
		return
	}
	target := core.ResolveTarget(found.LongUrl, rules, visitor)

	finalUrl := target
	if found.ForwardQuery {
		var incoming [][2]string
		for _, key := range queryKeysInOrder(r.URL.RawQuery) {
			if a.Cfg.TrackSkipParam != "" && strings.EqualFold(key, a.Cfg.TrackSkipParam) {
				continue
			}
			for _, value := range r.URL.Query()[key] {
				incoming = append(incoming, [2]string{key, value})
			}
		}
		finalUrl = core.ForwardQuery(target, incoming)
	}

	visited := &VisitedShortUrl{
		ShortCode: found.ShortCode,
		Domain:    domain.Authority,
		LongUrl:   found.LongUrl,
	}
	a.RecordVisit(r, core.VisitValidShortUrl, &id, visited)

	status, ok := core.RedirectStatusOfCode(found.RedirectStatus)
	if !ok {
		status = a.Cfg.DefaultRedirectStatus
	}
	redirectWith(w, status, finalUrl)
}

// queryKeysInOrder preserves the query string's parameter order (url.Values
// is an unordered map).
func queryKeysInOrder(rawQuery string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		key := pair
		if idx := strings.IndexByte(pair, '='); idx >= 0 {
			key = pair[:idx]
		}
		decoded := key
		if unescaped, err := url.QueryUnescape(key); err == nil {
			decoded = unescaped
		}
		if !seen[decoded] {
			seen[decoded] = true
			keys = append(keys, decoded)
		}
	}
	return keys
}
