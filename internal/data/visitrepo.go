package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

type NewVisit struct {
	ShortURLID *core.ShortURLID
	VisitType  core.VisitType
	VisitedAt  time.Time
	Referer    *string
	UserAgent  *string
	Browser    *string
	OS         *string
	Device     core.Device
	IsBot      bool
	RemoteIP   *string
	VisitedURL *string
}

// GeoInfo is the geolocation result for one visit. All nil = lookup found
// nothing.
type GeoInfo struct {
	CountryCode *string
	CountryName *string
	City        *string
	Latitude    *float64
	Longitude   *float64
}

type VisitFilters struct {
	StartDate    *time.Time
	EndDate      *time.Time
	ExcludeBots  bool
	Page         int
	ItemsPerPage int
}

const visitSelectCols = `id, short_url_id, visit_type, visited_at, referer, user_agent, browser, os, device,
	is_bot, remote_ip, country_code, country_name, city, latitude, longitude, visited_url, geo_resolved`

func scanVisitRow(r rowScanner) (*VisitRow, error) {
	var v VisitRow
	err := r.Scan(&v.ID, &v.ShortURLID, &v.VisitType, asTime(&v.VisitedAt), &v.Referer, &v.UserAgent,
		&v.Browser, &v.OS, &v.Device, &v.IsBot, &v.RemoteIP, &v.CountryCode, &v.CountryName, &v.City,
		&v.Latitude, &v.Longitude, &v.VisitedURL, &v.GeoResolved)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func InsertVisit(ctx context.Context, db *DB, v NewVisit) (core.VisitID, error) {
	var shortURLID any
	if v.ShortURLID != nil {
		shortURLID = v.ShortURLID.Value()
	}
	var id int64
	err := db.QueryRow(ctx,
		`INSERT INTO visits
		   (short_url_id, visit_type, visited_at, referer, user_agent, browser, os, device,
		    is_bot, remote_ip, visited_url, geo_resolved)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		shortURLID, v.VisitType.Slug(), db.BindTime(v.VisitedAt), v.Referer, v.UserAgent,
		v.Browser, v.OS, v.Device.Slug(), v.IsBot, v.RemoteIP, v.VisitedURL, false).Scan(&id)
	return core.VisitID(id), err
}

func SetVisitGeo(ctx context.Context, db *DB, visitID core.VisitID, geo GeoInfo) error {
	_, err := db.Exec(ctx,
		`UPDATE visits SET country_code = ?, country_name = ?, city = ?,
		                   latitude = ?, longitude = ?, geo_resolved = ?
		 WHERE id = ?`,
		geo.CountryCode, geo.CountryName, geo.City, geo.Latitude, geo.Longitude, true, visitID.Value())
	return err
}

func MarkGeoResolved(ctx context.Context, db *DB, visitID core.VisitID) error {
	_, err := db.Exec(ctx, "UPDATE visits SET geo_resolved = ? WHERE id = ?", true, visitID.Value())
	return err
}

// PendingGeoRow is a visit still awaiting geolocation (with a usable IP).
type PendingGeoRow struct {
	ID core.VisitID
	IP string
}

func ListPendingGeo(ctx context.Context, db *DB, limit int) ([]PendingGeoRow, error) {
	return queryAll(ctx, db, func(r rowScanner) (*PendingGeoRow, error) {
		var p PendingGeoRow
		if err := r.Scan(&p.ID, &p.IP); err != nil {
			return nil, err
		}
		return &p, nil
	}, fmt.Sprintf(`SELECT id, remote_ip FROM visits
	                WHERE geo_resolved = %s AND remote_ip IS NOT NULL
	                ORDER BY id LIMIT ?`, db.BoolLiteral(false)), limit)
}

func buildVisitFilterSQL(db *DB, filters VisitFilters) ([]string, []any) {
	var conditions []string
	var args []any
	if filters.StartDate != nil {
		conditions = append(conditions, "vi.visited_at >= ?")
		args = append(args, db.BindTime(*filters.StartDate))
	}
	if filters.EndDate != nil {
		conditions = append(conditions, "vi.visited_at <= ?")
		args = append(args, db.BindTime(*filters.EndDate))
	}
	if filters.ExcludeBots {
		conditions = append(conditions, fmt.Sprintf("vi.is_bot = %s", db.BoolLiteral(false)))
	}
	return conditions, args
}

func pageVisitQuery(ctx context.Context, db *DB, scope VisitScope, visitType *core.VisitType, filters VisitFilters) (core.Page[VisitRow], error) {
	baseWhere, baseArgs := scopeWhere(scope)
	if visitType != nil {
		baseWhere += " AND vi.visit_type = ?"
		baseArgs = append(baseArgs, visitType.Slug())
	}
	conditions, extraArgs := buildVisitFilterSQL(db, filters)
	conditions = append([]string{baseWhere}, conditions...)
	args := append(append([]any{}, baseArgs...), extraArgs...)
	return queryPage(ctx, db, scanVisitRow, visitSelectColsAliased(), "visits vi",
		"vi.visited_at DESC, vi.id DESC", conditions, args,
		ListFilters{Page: filters.Page, ItemsPerPage: filters.ItemsPerPage})
}

func visitSelectColsAliased() string {
	cols := strings.Split(visitSelectCols, ",")
	for i, c := range cols {
		cols[i] = "vi." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

func ListVisitsForShortURL(ctx context.Context, db *DB, shortURLID core.ShortURLID, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db, ShortURLScope(shortURLID), nil, filters)
}

// ListNonOrphanVisits lists all non-orphan visits, optionally filtered.
func ListNonOrphanVisits(ctx context.Context, db *DB, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db, GlobalScope(), nil, filters)
}

func ListOrphanVisits(ctx context.Context, db *DB, visitType *core.VisitType, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db, OrphanScope(), visitType, filters)
}

func ListVisitsForTag(ctx context.Context, db *DB, tagName string, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db, TagScope(tagName), nil, filters)
}

func ListVisitsForDomain(ctx context.Context, db *DB, domainID core.DomainID, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db, DomainScope(domainID), nil, filters)
}

func DeleteVisitsForShortURL(ctx context.Context, db *DB, shortURLID core.ShortURLID) (int, error) {
	return execCount(ctx, db, "DELETE FROM visits WHERE short_url_id = ?", shortURLID.Value())
}

func DeleteOrphanVisits(ctx context.Context, db *DB) (int, error) {
	return execCount(ctx, db, fmt.Sprintf("DELETE FROM visits WHERE %s", IsOrphanVisit("visits")))
}
