package data

import (
	"context"
	"database/sql"
	"errors"
)

// rowScanner abstracts sql.Row and sql.Rows so one scan function serves both
// single-row and multi-row queries.
type rowScanner interface{ Scan(dest ...any) error }

// queryOne runs a single-row query; a missing row is (nil, nil), not an error.
func queryOne[T any](ctx context.Context, db *DB, scan func(rowScanner) (*T, error), query string, args ...any) (*T, error) {
	v, err := scan(db.QueryRow(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// queryAll runs a multi-row query and scans every row.
func queryAll[T any](ctx context.Context, db *DB, scan func(rowScanner) (*T, error), query string, args ...any) ([]T, error) {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// queryScalar reads a single value (count, existence flag, …).
func queryScalar[T any](ctx context.Context, db *DB, query string, args ...any) (T, error) {
	var v T
	err := db.QueryRow(ctx, query, args...).Scan(&v)
	return v, err
}

// queryStrings reads a single string column from every row.
func queryStrings(ctx context.Context, db *DB, query string, args ...any) ([]string, error) {
	return queryAll(ctx, db, func(r rowScanner) (*string, error) {
		var s string
		if err := r.Scan(&s); err != nil {
			return nil, err
		}
		return &s, nil
	}, query, args...)
}

// execCount runs a statement and reports how many rows it touched.
func execCount(ctx context.Context, db *DB, query string, args ...any) (int, error) {
	res, err := db.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// execAffected runs a statement and reports whether it touched any row.
func execAffected(ctx context.Context, db *DB, query string, args ...any) (bool, error) {
	n, err := execCount(ctx, db, query, args...)
	return n > 0, err
}
