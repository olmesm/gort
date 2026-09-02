package data

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

// VisitScope says which set of visits a stats query aggregates over.
type VisitScopeKind int

const (
	ScopeGlobal VisitScopeKind = iota
	ScopeShortUrl
	ScopeTag
	ScopeDomain
	ScopeOrphan
)

type VisitScope struct {
	Kind       VisitScopeKind
	ShortUrlId core.ShortUrlID
	TagName    string
	DomainId   core.DomainID
}

func GlobalScope() VisitScope { return VisitScope{Kind: ScopeGlobal} }
func OrphanScope() VisitScope { return VisitScope{Kind: ScopeOrphan} }
func ShortUrlScope(id core.ShortUrlID) VisitScope {
	return VisitScope{Kind: ScopeShortUrl, ShortUrlId: id}
}
func TagScope(name string) VisitScope         { return VisitScope{Kind: ScopeTag, TagName: name} }
func DomainScope(id core.DomainID) VisitScope { return VisitScope{Kind: ScopeDomain, DomainId: id} }

type OverviewRow struct {
	ShortUrlCount    int64
	VisitCount       int64
	OrphanVisitCount int64
	TagCount         int64
	BotVisitCount    int64
}

func scopeWhere(scope VisitScope) (string, []any) {
	switch scope.Kind {
	case ScopeShortUrl:
		return fmt.Sprintf("%s AND vi.short_url_id = ?", IsValidVisit("vi")),
			[]any{scope.ShortUrlId.Value()}
	case ScopeTag:
		return fmt.Sprintf(`%s AND EXISTS (
		     SELECT 1 FROM short_url_tags st JOIN tags t ON t.id = st.tag_id
		     WHERE st.short_url_id = vi.short_url_id AND t.name = ?)`, IsValidVisit("vi")),
			[]any{scope.TagName}
	case ScopeDomain:
		return fmt.Sprintf(`%s AND EXISTS (
		     SELECT 1 FROM short_urls su WHERE su.id = vi.short_url_id AND su.domain_id = ?)`,
				IsValidVisit("vi")),
			[]any{scope.DomainId.Value()}
	case ScopeOrphan:
		return IsOrphanVisit("vi"), nil
	default:
		return IsValidVisit("vi"), nil
	}
}

func rangeWhere(db *Db, startDate, endDate *time.Time) (string, []any) {
	var parts []string
	var args []any
	if startDate != nil {
		parts = append(parts, "vi.visited_at >= ?")
		args = append(args, db.BindTime(*startDate))
	}
	if endDate != nil {
		parts = append(parts, "vi.visited_at <= ?")
		args = append(args, db.BindTime(*endDate))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(parts, " AND "), args
}

type DayCount struct {
	Day   string
	Count int64
}

// VisitsPerDay returns daily visit counts within a range for the given scope.
func VisitsPerDay(db *Db, scope VisitScope, startDate, endDate *time.Time) ([]DayCount, error) {
	scopeSql, scopeArgs := scopeWhere(scope)
	rangeSql, rangeArgs := rangeWhere(db, startDate, endDate)
	dayExpr := db.DayExpr("vi.visited_at")

	rows, err := db.Query(
		fmt.Sprintf(`SELECT %s AS day, COUNT(*) AS count
		             FROM visits vi WHERE %s%s
		             GROUP BY %s ORDER BY day`, dayExpr, scopeSql, rangeSql, dayExpr),
		append(scopeArgs, rangeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayCount
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type LabelCount struct {
	Label *string
	Count int64
}

// Breakdown returns visit counts grouped by an attribute (country, city,
// browser, os, referer, device).
func Breakdown(db *Db, scope VisitScope, column string, startDate, endDate *time.Time, limit int) ([]LabelCount, error) {
	switch column {
	case "country_name", "country_code", "city", "browser", "os", "referer", "device":
		// Whitelisted: this ends up in SQL directly.
	default:
		return nil, fmt.Errorf("unsupported breakdown column: %s", column)
	}

	scopeSql, scopeArgs := scopeWhere(scope)
	rangeSql, rangeArgs := rangeWhere(db, startDate, endDate)
	args := append(append(scopeArgs, rangeArgs...), limit)

	rows, err := db.Query(
		fmt.Sprintf(`SELECT vi.%s AS label, COUNT(*) AS count
		             FROM visits vi WHERE %s%s
		             GROUP BY vi.%s ORDER BY count DESC
		             LIMIT ?`, column, scopeSql, rangeSql, column), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabelCount
	for rows.Next() {
		var label sql.NullString
		var count int64
		if err := rows.Scan(&label, &count); err != nil {
			return nil, err
		}
		out = append(out, LabelCount{Label: strPtr(label), Count: count})
	}
	return out, rows.Err()
}

func VisitCount(db *Db, scope VisitScope, startDate, endDate *time.Time) (int64, error) {
	scopeSql, scopeArgs := scopeWhere(scope)
	rangeSql, rangeArgs := rangeWhere(db, startDate, endDate)
	var count int64
	err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM visits vi WHERE %s%s", scopeSql, rangeSql),
		append(scopeArgs, rangeArgs...)...).Scan(&count)
	return count, err
}

func Overview(db *Db) (OverviewRow, error) {
	var o OverviewRow
	err := db.QueryRow(fmt.Sprintf(
		`SELECT
		   (SELECT COUNT(*) FROM short_urls) AS short_url_count,
		   (SELECT COUNT(*) FROM visits vi WHERE %s) AS visit_count,
		   (SELECT COUNT(*) FROM visits vi WHERE %s) AS orphan_visit_count,
		   (SELECT COUNT(*) FROM tags) AS tag_count,
		   (SELECT COUNT(*) FROM visits vi WHERE %s AND vi.is_bot = %s) AS bot_visit_count`,
		IsValidVisit("vi"), IsOrphanVisit("vi"), IsValidVisit("vi"), db.BoolLiteral(true))).
		Scan(&o.ShortUrlCount, &o.VisitCount, &o.OrphanVisitCount, &o.TagCount, &o.BotVisitCount)
	return o, err
}
