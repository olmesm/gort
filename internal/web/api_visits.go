package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// statsScope resolves the requested scope and enforces API-key permissions.
func (a *App) statsScope(ctx context.Context, key *AuthenticatedKey, in *statsOptions) (data.VisitScope, error) {
	if in.Orphan {
		if key.Role.Kind != core.RoleAdmin {
			return data.VisitScope{}, Forbidden("Only admin keys can query orphan visit stats.")
		}
		return data.OrphanScope(), nil
	}

	if code := in.ShortCode; code != "" {
		detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
		if err != nil {
			return data.VisitScope{}, err
		}
		return data.ShortURLScope(detail.ID), nil
	}
	if tag := in.Tag; tag != "" {
		if key.Role.Kind != core.RoleAdmin {
			return data.VisitScope{}, Forbidden("Only admin keys can query tag statistics.")
		}
		return data.TagScope(tag), nil
	}
	if authority := in.Domain; authority != "" {
		d, err := data.DomainByAuthority(ctx, a.DB, strings.ToLower(authority))
		if err != nil {
			return data.VisitScope{}, err
		}
		if d == nil {
			return data.VisitScope{}, NotFound(fmt.Sprintf("Domain '%s' is not registered.", authority))
		}
		if key.Role.Kind != core.RoleAdmin && (key.Role.Kind != core.RoleDomain || key.Role.DomainID != d.ID) {
			return data.VisitScope{}, Forbidden("This API key cannot view statistics for this domain.")
		}
		return data.DomainScope(d.ID), nil
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
func (a *App) opVisitsOverview(ctx context.Context, key *AuthenticatedKey, in *Empty) (*VisitOverviewDTO, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can view the global visit summary.")
	}
	o, err := data.Overview(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	return result(VisitOverviewDTO{
		VisitsCount:       o.VisitCount,
		OrphanVisitsCount: o.OrphanVisitCount,
		ShortURLsCount:    o.ShortURLCount,
		TagsCount:         o.TagCount,
		BotVisitsCount:    o.BotVisitCount,
	})
}

// GET /rest/v1/visits/non-orphan
func (a *App) opListNonOrphanVisits(ctx context.Context, key *AuthenticatedKey, in *data.VisitFilters) (*PageDTO[VisitDTO], error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can list all visits.")
	}
	page, err := data.ListNonOrphanVisits(ctx, a.DB, *in)
	if err != nil {
		return nil, err
	}
	return result(NewPageDTO(page, NewVisitDTO))
}

// GET /rest/v1/visits/orphan?type=
func (a *App) opListOrphanVisits(ctx context.Context, key *AuthenticatedKey, in *orphanVisitOptions) (*PageDTO[VisitDTO], error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can list orphan visits.")
	}
	var visitType *core.VisitType
	if vt, ok := core.VisitTypeOfSlug(in.Type); ok {
		visitType = &vt
	}
	page, err := data.ListOrphanVisits(ctx, a.DB, visitType, in.VisitFilters)
	if err != nil {
		return nil, err
	}
	return result(NewPageDTO(page, NewVisitDTO))
}

// DELETE /rest/v1/visits/orphan
func (a *App) opDeleteOrphanVisits(ctx context.Context, key *AuthenticatedKey, in *Empty) (*DeletedVisits, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can delete orphan visits.")
	}
	deleted, err := data.DeleteOrphanVisits(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	return result(DeletedVisits{DeletedVisits: deleted})
}

// GET /rest/v1/stats/visits-per-day
func (a *App) opVisitsPerDay(ctx context.Context, key *AuthenticatedKey, in *statsOptions) (*DataList[dayDTO], error) {
	scope, err := a.statsScope(ctx, key, in)
	if err != nil {
		return nil, err
	}
	series, err := data.VisitsPerDay(ctx, a.DB, scope, in.StartDate, in.EndDate)
	if err != nil {
		return nil, err
	}
	days := make([]dayDTO, len(series))
	for i, d := range series {
		days[i] = dayDTO{Date: d.Day, Count: d.Count}
	}
	return result(DataList[dayDTO]{Data: days})
}

// GET /rest/v1/stats/breakdown?by=country|city|browser|os|referer|device
func (a *App) opBreakdown(ctx context.Context, key *AuthenticatedKey, in *breakdownOptions) (*DataList[breakdownDTO], error) {
	var column string
	switch strings.ToLower(in.By) {
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
		return nil, BadRequest("Provide ?by= one of: country, countryCode, city, browser, os, referer, device.")
	}

	scope, err := a.statsScope(ctx, key, &in.statsOptions)
	if err != nil {
		return nil, err
	}

	limit := in.Limit
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := data.Breakdown(ctx, a.DB, scope, column, in.StartDate, in.EndDate, limit)
	if err != nil {
		return nil, err
	}
	items := make([]breakdownDTO, len(rows))
	for i, row := range rows {
		value := "Unknown"
		if row.Label != nil {
			value = *row.Label
		}
		items[i] = breakdownDTO{Value: value, Count: row.Count}
	}
	return result(DataList[breakdownDTO]{Data: items})
}

type dayDTO struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type breakdownDTO struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}
