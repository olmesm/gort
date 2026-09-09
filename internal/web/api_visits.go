package web

import (
	"context"
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
func (a *App) scopeFromQuery(ctx context.Context, key *AuthenticatedKey, q url.Values) (data.VisitScope, error) {
	if queryBool(q, "orphan") {
		if key.Role.Kind != core.RoleAdmin {
			return data.VisitScope{}, Forbidden("Only admin keys can query orphan visit stats.")
		}
		return data.OrphanScope(), nil
	}

	if code := q.Get("shortCode"); code != "" {
		detail, err := a.findAccessibleShortUrl(ctx, key, code, q.Get("domain"))
		if err != nil {
			return data.VisitScope{}, err
		}
		return data.ShortUrlScope(detail.Id), nil
	}
	if tag := q.Get("tag"); tag != "" {
		return data.TagScope(tag), nil
	}
	if authority := q.Get("domain"); authority != "" {
		d, err := data.DomainByAuthority(ctx, a.Db, strings.ToLower(authority))
		if err != nil {
			return data.VisitScope{}, err
		}
		if d == nil {
			return data.VisitScope{}, NotFound(fmt.Sprintf("Domain '%s' is not registered.", authority))
		}
		return data.DomainScope(d.Id), nil
	}

	switch key.Role.Kind {
	case core.RoleAdmin:
		return data.GlobalScope(), nil
	case core.RoleDomain:
		return data.DomainScope(key.Role.DomainID), nil
	default:
		return data.VisitScope{}, Forbidden("Author keys must scope stats to a shortCode.")
	}
}

// GET /rest/v1/visits — global counters.
func (a *App) apiVisitsOverview(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	if key.Role.Kind != core.RoleAdmin {
		return Forbidden("Only admin keys can view the global visit summary.")
	}
	o, err := data.Overview(r.Context(), a.Db)
	if err != nil {
		return err
	}
	RespondJSON(w, http.StatusOK, map[string]int64{
		"visitsCount":       o.VisitCount,
		"orphanVisitsCount": o.OrphanVisitCount,
		"shortUrlsCount":    o.ShortUrlCount,
		"tagsCount":         o.TagCount,
		"botVisitsCount":    o.BotVisitCount,
	})
	return nil
}

// GET /rest/v1/visits/non-orphan
func (a *App) apiListNonOrphanVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	if key.Role.Kind != core.RoleAdmin {
		return Forbidden("Only admin keys can list all visits.")
	}
	page, err := data.ListNonOrphanVisits(r.Context(), a.Db, visitFiltersFromQuery(r.URL.Query()))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}

// GET /rest/v1/visits/orphan?type=
func (a *App) apiListOrphanVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	if key.Role.Kind != core.RoleAdmin {
		return Forbidden("Only admin keys can list orphan visits.")
	}
	q := r.URL.Query()
	var visitType *core.VisitType
	if vt, ok := core.VisitTypeOfSlug(q.Get("type")); ok {
		visitType = &vt
	}
	page, err := data.ListOrphanVisits(r.Context(), a.Db, visitType, visitFiltersFromQuery(q))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}

// DELETE /rest/v1/visits/orphan
func (a *App) apiDeleteOrphanVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	if key.Role.Kind != core.RoleAdmin {
		return Forbidden("Only admin keys can delete orphan visits.")
	}
	deleted, err := data.DeleteOrphanVisits(r.Context(), a.Db)
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, map[string]int{"deletedVisits": deleted})
}

// GET /rest/v1/stats/visits-per-day
func (a *App) apiVisitsPerDay(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	scope, err := a.scopeFromQuery(r.Context(), key, q)
	if err != nil {
		return err
	}
	series, err := data.VisitsPerDay(r.Context(), a.Db, scope, queryDate(q, "startDate"), queryDate(q, "endDate"))
	if err != nil {
		return err
	}
	type dayDto struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}
	days := make([]dayDto, len(series))
	for i, d := range series {
		days[i] = dayDto{Date: d.Day, Count: d.Count}
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"data": days})
}

// GET /rest/v1/stats/breakdown?by=country|city|browser|os|referer|device
func (a *App) apiBreakdown(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
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
		return BadRequest("Provide ?by= one of: country, countryCode, city, browser, os, referer, device.")
	}

	scope, err := a.scopeFromQuery(r.Context(), key, q)

	if err != nil {

		return err

	}

	limit := queryIntDefault(q, "limit", 25)
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := data.Breakdown(r.Context(), a.Db, scope, column, queryDate(q, "startDate"), queryDate(q, "endDate"), limit)
	if err != nil {
		return err
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
	return RespondJSON(w, http.StatusOK, map[string]any{"data": items})
}
