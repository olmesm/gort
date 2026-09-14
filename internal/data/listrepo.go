package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/olmesm/gort/internal/core"
)

type ListFilters struct {
	Search       string
	Page         int
	ItemsPerPage int
}

// queryPage applies the same predicates to the count and the bounded query.
// SQL fragments come from repository code; user values are always arguments.
func queryPage[T any](ctx context.Context, db *DB, scan func(rowScanner) (*T, error), columns, from, order string, conditions []string, args []any, filters ListFilters) (core.Page[T], error) {
	page, size := core.NormalizePaging(filters.Page, filters.ItemsPerPage)
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	total, err := queryScalar[int64](ctx, db, "SELECT COUNT(*) FROM "+from+where, args...)
	if err != nil {
		return core.Page[T]{}, err
	}
	page = clampListPage(page, size, total)
	listArgs := append(append([]any{}, args...), size, core.PageOffset(page, size))
	items, err := queryAll(ctx, db, scan, fmt.Sprintf("SELECT %s FROM %s%s ORDER BY %s LIMIT ? OFFSET ?", columns, from, where, order), listArgs...)
	if err != nil {
		return core.Page[T]{}, err
	}
	return core.Page[T]{Items: items, CurrentPage: page, ItemsPerPage: size, TotalItems: total}, nil
}

func searchCondition(db *DB, search string, columns ...string) ([]string, []any) {
	search = strings.TrimSpace(search)
	if search == "" {
		return nil, nil
	}
	var parts []string
	var args []any
	for _, column := range columns {
		parts = append(parts, db.ILike(column, "?"))
		args = append(args, "%"+search+"%")
	}
	return []string{"(" + strings.Join(parts, " OR ") + ")"}, args
}

// Called after NormalizePaging and counting, before calculating an offset.
func clampListPage(page, size int, total int64) int {
	return min(page, max(1, int((total+int64(size)-1)/int64(size))))
}
