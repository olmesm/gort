package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

// VisitScope says which set of visits a stats query aggregates over.
type VisitScopeKind int

const (
	ScopeGlobal VisitScopeKind = iota
	ScopeShortURL
	ScopeTag
	ScopeDomain
	ScopeOrphan
)

type VisitScope struct {
	Kind       VisitScopeKind
	ShortURLID core.ShortURLID
	TagName    string
	DomainID   core.DomainID
}

func GlobalScope() VisitScope { return VisitScope{Kind: ScopeGlobal} }
func OrphanScope() VisitScope { return VisitScope{Kind: ScopeOrphan} }
func ShortURLScope(id core.ShortURLID) VisitScope {
	return VisitScope{Kind: ScopeShortURL, ShortURLID: id}
}
func TagScope(name string) VisitScope         { return VisitScope{Kind: ScopeTag, TagName: name} }
func DomainScope(id core.DomainID) VisitScope { return VisitScope{Kind: ScopeDomain, DomainID: id} }

type OverviewRow struct {
	ShortURLCount    int64
	VisitCount       int64
	OrphanVisitCount int64
	TagCount         int64
	BotVisitCount    int64
}

func scopeWhere(scope VisitScope) (string, []any) {
	switch scope.Kind {
	case ScopeShortURL:
		return fmt.Sprintf("%s AND vi.short_url_id = ?", IsValidVisit("vi")),
			[]any{scope.ShortURLID.Value()}
	case ScopeTag:
		return fmt.Sprintf(`%s AND EXISTS (
		     SELECT 1 FROM short_url_tags st JOIN tags t ON t.id = st.tag_id
		     WHERE st.short_url_id = vi.short_url_id AND t.name = ?)`, IsValidVisit("vi")),
			[]any{scope.TagName}
	case ScopeDomain:
		return fmt.Sprintf(`%s AND EXISTS (
		     SELECT 1 FROM short_urls su WHERE su.id = vi.short_url_id AND su.domain_id = ?)`,
				IsValidVisit("vi")),
			[]any{scope.DomainID.Value()}
	case ScopeOrphan:
		return IsOrphanVisit("vi"), nil
	default:
		return IsValidVisit("vi"), nil
	}
}

func rangeWhere(db *DB, startDate, endDate *time.Time) (string, []any) {
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
func VisitsPerDay(ctx context.Context, db *DB, scope VisitScope, startDate, endDate *time.Time) ([]DayCount, error) {
	scopeSQL, scopeArgs := scopeWhere(scope)
	rangeSQL, rangeArgs := rangeWhere(db, startDate, endDate)
	dayExpr := db.DayExpr("vi.visited_at")

	return queryAll(ctx, db, func(r rowScanner) (*DayCount, error) {
		var d DayCount
		if err := r.Scan(&d.Day, &d.Count); err != nil {
			return nil, err
		}
		return &d, nil
	}, fmt.Sprintf(`SELECT %s AS day, COUNT(*) AS count
	                FROM visits vi WHERE %s%s
	                GROUP BY %s ORDER BY day`, dayExpr, scopeSQL, rangeSQL, dayExpr),
		append(scopeArgs, rangeArgs...)...)
}

type LabelCount struct {
	Label *string
	Count int64
}

// Breakdown returns visit counts grouped by an attribute (country, city,
// browser, os, referer, device).
func Breakdown(ctx context.Context, db *DB, scope VisitScope, column string, startDate, endDate *time.Time, limit int) ([]LabelCount, error) {
	switch column {
	case "country_name", "country_code", "city", "browser", "os", "referer", "device":
		// Whitelisted: this ends up in SQL directly.
	default:
		return nil, fmt.Errorf("unsupported breakdown column: %s", column)
	}

	scopeSQL, scopeArgs := scopeWhere(scope)
	rangeSQL, rangeArgs := rangeWhere(db, startDate, endDate)
	args := append(append(scopeArgs, rangeArgs...), limit)

	return queryAll(ctx, db, func(r rowScanner) (*LabelCount, error) {
		var lc LabelCount
		if err := r.Scan(&lc.Label, &lc.Count); err != nil {
			return nil, err
		}
		return &lc, nil
	}, fmt.Sprintf(`SELECT vi.%s AS label, COUNT(*) AS count
	                FROM visits vi WHERE %s%s
	                GROUP BY vi.%s ORDER BY count DESC
	                LIMIT ?`, column, scopeSQL, rangeSQL, column), args...)
}

func VisitCount(ctx context.Context, db *DB, scope VisitScope, startDate, endDate *time.Time) (int64, error) {
	scopeSQL, scopeArgs := scopeWhere(scope)
	rangeSQL, rangeArgs := rangeWhere(db, startDate, endDate)
	return queryScalar[int64](ctx, db,
		fmt.Sprintf("SELECT COUNT(*) FROM visits vi WHERE %s%s", scopeSQL, rangeSQL),
		append(scopeArgs, rangeArgs...)...)
}

func Overview(ctx context.Context, db *DB) (OverviewRow, error) {
	var o OverviewRow
	err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT
		   (SELECT COUNT(*) FROM short_urls) AS short_url_count,
		   (SELECT COUNT(*) FROM visits vi WHERE %s) AS visit_count,
		   (SELECT COUNT(*) FROM visits vi WHERE %s) AS orphan_visit_count,
		   (SELECT COUNT(*) FROM tags) AS tag_count,
		   (SELECT COUNT(*) FROM visits vi WHERE %s AND vi.is_bot = %s) AS bot_visit_count`,
		IsValidVisit("vi"), IsOrphanVisit("vi"), IsValidVisit("vi"), db.BoolLiteral(true))).
		Scan(&o.ShortURLCount, &o.VisitCount, &o.OrphanVisitCount, &o.TagCount, &o.BotVisitCount)
	return o, err
}
