package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// scopeFromQuery builds a stats scope from query params
// (?shortCode=&domain=&tag=&orphan=true). On failure it writes the error
// response and returns nil.
func (a *App) scopeFromQuery(w http.ResponseWriter, key *AuthenticatedKey, q url.Values) *data.VisitScope {
	if queryBool(q, "orphan") {
		if key.Role.Kind != core.RoleAdmin {
			Forbidden(w, "Only admin keys can query orphan visit stats.")
			return nil
		}
		scope := data.OrphanScope()
		return &scope
	}

	if code := q.Get("shortCode"); code != "" {
		detail := a.findAccessibleShortUrl(w, key, code, q.Get("domain"))
		if detail == nil {
			return nil
		}
		scope := data.ShortUrlScope(core.ShortUrlID(detail.Id))
		return &scope
	}
	if tag := q.Get("tag"); tag != "" {
		scope := data.TagScope(tag)
		return &scope
	}
	if authority := q.Get("domain"); authority != "" {
		d, err := data.TryGetDomainByAuthority(a.Db, strings.ToLower(authority))
		if err != nil {
			a.serverError(w, err)
			return nil
		}
		if d == nil {
			NotFound(w, fmt.Sprintf("Domain '%s' is not registered.", authority))
			return nil
		}
		scope := data.DomainScope(core.DomainID(d.Id))
		return &scope
	}

	switch key.Role.Kind {
	case core.RoleAdmin:
		scope := data.GlobalScope()
		return &scope
	case core.RoleDomain:
		scope := data.DomainScope(key.Role.DomainID)
		return &scope
	default:
		Forbidden(w, "Author keys must scope stats to a shortCode.")
		return nil
	}
}

// GET /rest/v1/visits — global counters.
func (a *App) apiVisitsOverview(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	if key.Role.Kind != core.RoleAdmin {
		Forbidden(w, "Only admin keys can view the global visit summary.")
		return
	}
	o, err := data.Overview(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	RespondJSON(w, http.StatusOK, map[string]int64{
		"visitsCount":       o.VisitCount,
		"orphanVisitsCount": o.OrphanVisitCount,
		"shortUrlsCount":    o.ShortUrlCount,
		"tagsCount":         o.TagCount,
		"botVisitsCount":    o.BotVisitCount,
	})
}

// GET /rest/v1/visits/non-orphan
func (a *App) apiListNonOrphanVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	if key.Role.Kind != core.RoleAdmin {
		Forbidden(w, "Only admin keys can list all visits.")
		return
	}
	page, err := data.ListNonOrphanVisits(a.Db, visitFiltersFromQuery(r.URL.Query()))
	if err != nil {
		a.serverError(w, err)
		return
	}
	RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}

// GET /rest/v1/visits/orphan?type=
func (a *App) apiListOrphanVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	if key.Role.Kind != core.RoleAdmin {
		Forbidden(w, "Only admin keys can list orphan visits.")
		return
	}
	q := r.URL.Query()
	var visitType *core.VisitType
	if vt, ok := core.VisitTypeOfSlug(q.Get("type")); ok {
		visitType = &vt
	}
	page, err := data.ListOrphanVisits(a.Db, visitType, visitFiltersFromQuery(q))
	if err != nil {
		a.serverError(w, err)
		return
	}
	RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}

// DELETE /rest/v1/visits/orphan
func (a *App) apiDeleteOrphanVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	if key.Role.Kind != core.RoleAdmin {
		Forbidden(w, "Only admin keys can delete orphan visits.")
		return
	}
	deleted, err := data.DeleteOrphanVisits(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	RespondJSON(w, http.StatusOK, map[string]int{"deletedVisits": deleted})
}

// GET /rest/v1/stats/visits-per-day
func (a *App) apiVisitsPerDay(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope := a.scopeFromQuery(w, key, q)
	if scope == nil {
		return
	}
	series, err := data.VisitsPerDay(a.Db, *scope, queryDate(q, "startDate"), queryDate(q, "endDate"))
	if err != nil {
		a.serverError(w, err)
		return
	}
	type dayDto struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}
	days := make([]dayDto, len(series))
	for i, d := range series {
		days[i] = dayDto{Date: d.Day, Count: d.Count}
	}
	RespondJSON(w, http.StatusOK, map[string]any{"data": days})
}

// GET /rest/v1/stats/breakdown?by=country|city|browser|os|referer|device
func (a *App) apiBreakdown(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var column string
	switch strings.ToLower(q.Get("by")) {
	case "country":
		column = "country_name"
	case "countrycode":
		column = "country_code"
	case "city":
		column = "city"
	case "browser":
		column = "browser"
	case "os":
		column = "os"
	case "referer", "referrer":
		column = "referer"
	case "device":
		column = "device"
	default:
		BadRequest(w, "Provide ?by= one of: country, countryCode, city, browser, os, referer, device.")
		return
	}

	scope := a.scopeFromQuery(w, key, q)
	if scope == nil {
		return
	}

	limit := queryIntDefault(q, "limit", 25)
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := data.Breakdown(a.Db, *scope, column, queryDate(q, "startDate"), queryDate(q, "endDate"), limit)
	if err != nil {
		a.serverError(w, err)
		return
	}
	type breakdownDto struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	items := make([]breakdownDto, len(rows))
	for i, row := range rows {
		value := "Unknown"
		if row.Label != nil {
			value = *row.Label
		}
		items[i] = breakdownDto{Value: value, Count: row.Count}
	}
	RespondJSON(w, http.StatusOK, map[string]any{"data": items})
}
