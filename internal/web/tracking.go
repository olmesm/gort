package web

import (
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	ua "github.com/mileusna/useragent"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// Visit capture: privacy-aware extraction of request data plus event
// publishing.

var botRegex = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|curl|wget|python-requests|httpclient|headless|preview|scan|monitor|facebookexternalhit|whatsapp|telegrambot|skypeuripreview|bingpreview`)

func IsBot(userAgent string) bool {
	return userAgent != "" && botRegex.MatchString(userAgent)
}

func headerValue(r *http.Request, name string) *string {
	if v := r.Header.Get(name); strings.TrimSpace(v) != "" {
		return &v
	}
	return nil
}

// RemoteIP is the client address, honoring X-Forwarded-For (first hop) the
// way the reverse-proxy middleware resolved it.
func RemoteIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if net.ParseIP(first) != nil {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if net.ParseIP(r.RemoteAddr) != nil {
			return r.RemoteAddr
		}
		return ""
	}
	return host
}

// requestScheme honors X-Forwarded-Proto behind a reverse proxy.
func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// shouldSkipTracking says whether this request is excluded from tracking
// because of the configured skip param (e.g. ?no-track).
func (a *App) shouldSkipTracking(r *http.Request) bool {
	if a.Cfg.TrackSkipParam == "" {
		return false
	}
	return r.URL.Query().Has(a.Cfg.TrackSkipParam)
}

func parseUserAgentFamilies(userAgent string) (browser, osName *string) {
	parsed := ua.Parse(userAgent)
	family := func(s string) *string {
		if strings.TrimSpace(s) == "" || s == "Other" {
			return nil
		}
		return &s
	}
	return family(parsed.Name), family(parsed.OS)
}

// RecordVisit records a visit (if tracking settings allow it) and publishes
// the matching event. The only synchronous work is one INSERT; geolocation
// and webhook fan-out run on background workers.
func (a *App) RecordVisit(r *http.Request, visitType core.VisitType, shortUrlId *core.ShortUrlID, visited *VisitedShortUrl) {
	skip := a.Cfg.DisableTracking ||
		(visitType.IsOrphan() && !a.Cfg.TrackOrphanVisits) ||
		a.shouldSkipTracking(r)
	if skip {
		return
	}

	userAgent := headerValue(r, "User-Agent")
	referer := headerValue(r, "Referer")

	var browser, osName *string
	if userAgent != nil {
		browser, osName = parseUserAgentFamilies(*userAgent)
	}

	var ip *string
	if !a.Cfg.DisableIpTracking {
		if remote := RemoteIP(r); remote != "" {
			if a.Cfg.AnonymizeIps {
				if anonymized := core.AnonymizeIP(remote); anonymized != "" {
					ip = &anonymized
				}
			} else {
				ip = &remote
			}
		}
	}

	var visitedUrl *string
	if visitType.IsOrphan() {
		full := requestScheme(r) + "://" + r.Host + r.URL.Path
		if r.URL.RawQuery != "" {
			full += "?" + r.URL.RawQuery
		}
		visitedUrl = &full
	}

	uaValue := ""
	if userAgent != nil {
		uaValue = *userAgent
	}

	visit := data.NewVisit{
		ShortUrlId: shortUrlId,
		VisitType:  visitType,
		VisitedAt:  time.Now().UTC(),
		Referer:    referer,
		UserAgent:  userAgent,
		Browser:    browser,
		Os:         osName,
		Device:     core.DetectDevice(uaValue),
		IsBot:      IsBot(uaValue),
		RemoteIp:   ip,
		VisitedUrl: visitedUrl,
	}

	visitId, err := data.InsertVisit(a.Db, visit)
	if err != nil {
		a.Logger.Warn("Failed to record visit", "error", err)
		return
	}

	if ip != nil {
		a.Queues.enqueueGeo(visitId, *ip)
	}

	payload := VisitEventPayload{
		VisitType:    visitType.Slug(),
		ShortUrl:     visited,
		VisitedUrl:   visitedUrl,
		Referer:      referer,
		UserAgent:    userAgent,
		PotentialBot: visit.IsBot,
	}
	if visitType.IsOrphan() {
		a.Queues.PublishEvent(OrphanVisitRecordedEvent(payload))
	} else {
		a.Queues.PublishEvent(VisitRecordedEvent(payload))
	}
}
