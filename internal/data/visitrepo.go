package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

type NewVisit struct {
	ShortUrlId *core.ShortUrlID
	VisitType  core.VisitType
	VisitedAt  time.Time
	Referer    *string
	UserAgent  *string
	Browser    *string
	Os         *string
	Device     core.Device
	IsBot      bool
	RemoteIp   *string
	VisitedUrl *string
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

func EmptyVisitFilters() VisitFilters {
	return VisitFilters{Page: 1, ItemsPerPage: core.DefaultPageSize}
}

const visitSelectCols = `id, short_url_id, visit_type, visited_at, referer, user_agent, browser, os, device,
	is_bot, remote_ip, country_code, country_name, city, latitude, longitude, visited_url, geo_resolved`

func scanVisitRow(r rowScanner) (*VisitRow, error) {
	var v VisitRow
	err := r.Scan(&v.Id, &v.ShortUrlId, &v.VisitType, asTime(&v.VisitedAt), &v.Referer, &v.UserAgent,
		&v.Browser, &v.Os, &v.Device, &v.IsBot, &v.RemoteIp, &v.CountryCode, &v.CountryName, &v.City,
		&v.Latitude, &v.Longitude, &v.VisitedUrl, &v.GeoResolved)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func InsertVisit(ctx context.Context, db *Db, v NewVisit) (core.VisitID, error) {
	var shortUrlId any
	if v.ShortUrlId != nil {
		shortUrlId = v.ShortUrlId.Value()
	}
	var id int64
	err := db.QueryRow(ctx,
		`INSERT INTO visits
		   (short_url_id, visit_type, visited_at, referer, user_agent, browser, os, device,
		    is_bot, remote_ip, visited_url, geo_resolved)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		shortUrlId, v.VisitType.Slug(), db.BindTime(v.VisitedAt), v.Referer, v.UserAgent,
		v.Browser, v.Os, v.Device.Slug(), v.IsBot, v.RemoteIp, v.VisitedUrl, false).Scan(&id)
	return core.VisitID(id), err
}

func SetVisitGeo(ctx context.Context, db *Db, visitId core.VisitID, geo GeoInfo) error {
	_, err := db.Exec(ctx,
		`UPDATE visits SET country_code = ?, country_name = ?, city = ?,
		                   latitude = ?, longitude = ?, geo_resolved = ?
		 WHERE id = ?`,
		geo.CountryCode, geo.CountryName, geo.City, geo.Latitude, geo.Longitude, true, visitId.Value())
	return err
}

func MarkGeoResolved(ctx context.Context, db *Db, visitId core.VisitID) error {
	_, err := db.Exec(ctx, "UPDATE visits SET geo_resolved = ? WHERE id = ?", true, visitId.Value())
	return err
}

// PendingGeoRow is a visit still awaiting geolocation (with a usable IP).
type PendingGeoRow struct {
	Id core.VisitID
	Ip string
}

func ListPendingGeo(ctx context.Context, db *Db, limit int) ([]PendingGeoRow, error) {
	return queryAll(ctx, db, func(r rowScanner) (*PendingGeoRow, error) {
		var p PendingGeoRow
		if err := r.Scan(&p.Id, &p.Ip); err != nil {
			return nil, err
		}
		return &p, nil
	}, fmt.Sprintf(`SELECT id, remote_ip FROM visits
	                WHERE geo_resolved = %s AND remote_ip IS NOT NULL
	                ORDER BY id LIMIT ?`, db.BoolLiteral(false)), limit)
}

func buildVisitFilterSql(db *Db, filters VisitFilters) ([]string, []any) {
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

func pageVisitQuery(ctx context.Context, db *Db, baseWhere string, baseArgs []any, filters VisitFilters) (core.Page[VisitRow], error) {
	empty := core.Page[VisitRow]{}
	page, size := core.NormalizePaging(filters.Page, filters.ItemsPerPage)
	extra, extraArgs := buildVisitFilterSql(db, filters)

	whereClause := baseWhere
	if len(extra) > 0 {
		whereClause += " AND " + strings.Join(extra, " AND ")
	}
	args := append(append([]any{}, baseArgs...), extraArgs...)

	total, err := queryScalar[int64](ctx, db,
		fmt.Sprintf("SELECT COUNT(*) FROM visits vi WHERE %s", whereClause), args...)
	if err != nil {
		return empty, err
	}

	listArgs := append(append([]any{}, args...), size, core.PageOffset(page, size))
	items, err := queryAll(ctx, db, scanVisitRow,
		fmt.Sprintf(`SELECT %s FROM visits vi WHERE %s
		             ORDER BY vi.visited_at DESC, vi.id DESC
		             LIMIT ? OFFSET ?`, visitSelectColsAliased(), whereClause), listArgs...)
	if err != nil {
		return empty, err
	}
	return core.Page[VisitRow]{Items: items, CurrentPage: page, ItemsPerPage: size, TotalItems: total}, nil
}

func visitSelectColsAliased() string {
	cols := strings.Split(visitSelectCols, ",")
	for i, c := range cols {
		cols[i] = "vi." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

func ListVisitsForShortUrl(ctx context.Context, db *Db, shortUrlId core.ShortUrlID, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db,
		fmt.Sprintf("vi.short_url_id = ? AND %s", IsValidVisit("vi")),
		[]any{shortUrlId.Value()}, filters)
}

// ListNonOrphanVisits lists all non-orphan visits, optionally filtered.
func ListNonOrphanVisits(ctx context.Context, db *Db, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db, IsValidVisit("vi"), nil, filters)
}

func ListOrphanVisits(ctx context.Context, db *Db, visitType *core.VisitType, filters VisitFilters) (core.Page[VisitRow], error) {
	if visitType != nil {
		return pageVisitQuery(ctx, db,
			fmt.Sprintf("vi.visit_type = ? AND %s", IsOrphanVisit("vi")),
			[]any{visitType.Slug()}, filters)
	}
	return pageVisitQuery(ctx, db, IsOrphanVisit("vi"), nil, filters)
}

func ListVisitsForTag(ctx context.Context, db *Db, tagName string, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db,
		fmt.Sprintf(`%s AND EXISTS (
		     SELECT 1 FROM short_url_tags st JOIN tags t ON t.id = st.tag_id
		     WHERE st.short_url_id = vi.short_url_id AND t.name = ?)`, IsValidVisit("vi")),
		[]any{tagName}, filters)
}

func ListVisitsForDomain(ctx context.Context, db *Db, domainId core.DomainID, filters VisitFilters) (core.Page[VisitRow], error) {
	return pageVisitQuery(ctx, db,
		fmt.Sprintf(`%s AND EXISTS (
		     SELECT 1 FROM short_urls su WHERE su.id = vi.short_url_id AND su.domain_id = ?)`,
			IsValidVisit("vi")),
		[]any{domainId.Value()}, filters)
}

func DeleteVisitsForShortUrl(ctx context.Context, db *Db, shortUrlId core.ShortUrlID) (int, error) {
	return execCount(ctx, db, "DELETE FROM visits WHERE short_url_id = ?", shortUrlId.Value())
}

func DeleteOrphanVisits(ctx context.Context, db *Db) (int, error) {
	return execCount(ctx, db, fmt.Sprintf("DELETE FROM visits WHERE %s", IsOrphanVisit("visits")))
}
