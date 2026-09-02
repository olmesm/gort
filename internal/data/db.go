package data

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	sqlite3 "modernc.org/sqlite"

	"github.com/olmesm/gort/internal/core"
)

type Dialect int

const (
	Sqlite Dialect = iota
	Postgres
)

// Db is a connection pool plus dialect-specific SQL fragments. Repositories
// write SQL with `?` placeholders; Postgres rebinding happens in Exec/Query.
type Db struct {
	Dialect Dialect
	Pool    *sql.DB
}

// Open creates the pool. SQLite connections get their pragmas via the DSN so
// every pooled connection is configured identically.
func Open(dialect Dialect, connectionString string) (*Db, error) {
	switch dialect {
	case Sqlite:
		sep := "?"
		if strings.Contains(connectionString, "?") {
			sep = "&"
		}
		dsn := connectionString + sep +
			"_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
		pool, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		// A single writer connection sidesteps SQLITE_BUSY under concurrency;
		// WAL still serves readers through the same handle fine at this scale.
		pool.SetMaxOpenConns(1)
		return &Db{Dialect: Sqlite, Pool: pool}, nil
	case Postgres:
		pool, err := sql.Open("pgx", connectionString)
		if err != nil {
			return nil, err
		}
		return &Db{Dialect: Postgres, Pool: pool}, nil
	default:
		return nil, fmt.Errorf("unknown dialect %d", dialect)
	}
}

func (db *Db) Close() error { return db.Pool.Close() }

// rebind converts `?` placeholders to `$1..$n` for Postgres.
func (db *Db) rebind(query string) string {
	if db.Dialect != Postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Exec / Query / QueryRow run against the pool with placeholder rebinding.
func (db *Db) Exec(query string, args ...any) (sql.Result, error) {
	return db.Pool.Exec(db.rebind(query), args...)
}

func (db *Db) Query(query string, args ...any) (*sql.Rows, error) {
	return db.Pool.Query(db.rebind(query), args...)
}

func (db *Db) QueryRow(query string, args ...any) *sql.Row {
	return db.Pool.QueryRow(db.rebind(query), args...)
}

// WithTx runs several statements atomically. The transaction commits when
// work returns nil and rolls back otherwise.
func (db *Db) WithTx(work func(tx *Tx) error) error {
	raw, err := db.Pool.Begin()
	if err != nil {
		return err
	}
	tx := &Tx{db: db, raw: raw}
	if err := work(tx); err != nil {
		_ = raw.Rollback()
		return err
	}
	return raw.Commit()
}

// Tx wraps sql.Tx with the same rebinding as Db.
type Tx struct {
	db  *Db
	raw *sql.Tx
}

func (tx *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return tx.raw.Exec(tx.db.rebind(query), args...)
}

func (tx *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return tx.raw.Query(tx.db.rebind(query), args...)
}

func (tx *Tx) QueryRow(query string, args ...any) *sql.Row {
	return tx.raw.QueryRow(tx.db.rebind(query), args...)
}

// ---- Dialect-specific SQL fragments ----

// BoolLiteral is the SQL literal for a boolean value in this dialect.
func (db *Db) BoolLiteral(value bool) string {
	if db.Dialect == Sqlite {
		if value {
			return "1"
		}
		return "0"
	}
	if value {
		return "TRUE"
	}
	return "FALSE"
}

// DayExpr is a SQL expression grouping a timestamp column by calendar day
// (UTC), as 'YYYY-MM-DD'.
func (db *Db) DayExpr(column string) string {
	if db.Dialect == Sqlite {
		return fmt.Sprintf("strftime('%%Y-%%m-%%d', %s)", column)
	}
	return fmt.Sprintf("to_char(%s AT TIME ZONE 'UTC', 'YYYY-MM-DD')", column)
}

// ILike is a case-insensitive LIKE comparison.
func (db *Db) ILike(column, param string) string {
	return fmt.Sprintf("lower(%s) LIKE lower(%s)", column, param)
}

// InList builds a membership test with one `?` per value and returns the
// values as bind arguments. Empty lists yield a predicate that matches
// nothing.
func InList[T any](column string, values []T) (string, []any) {
	if len(values) == 0 {
		return "1 = 0", nil
	}
	placeholders := strings.Repeat("?,", len(values))
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return fmt.Sprintf("%s IN (%s)", column, placeholders[:len(placeholders)-1]), args
}

// ---- Domain-owned SQL fragments ----

// IsValidVisit: the visit row is a real short-URL visit (not orphan traffic).
func IsValidVisit(alias string) string {
	return fmt.Sprintf("%s.visit_type = '%s'", alias, core.VisitValidShortUrl.Slug())
}

// IsOrphanVisit: the visit row is orphan traffic of any kind.
func IsOrphanVisit(alias string) string {
	return fmt.Sprintf("%s.visit_type <> '%s'", alias, core.VisitValidShortUrl.Slug())
}

// ---- Time handling ----

// sqliteTimeFormat is fixed-width UTC ISO-8601 so lexicographic string
// comparison equals chronological comparison.
const sqliteTimeFormat = "2006-01-02T15:04:05.000Z"

// BindTime converts a timestamp for storage: TEXT for SQLite, native
// timestamptz for Postgres. All stored timestamps are UTC.
func (db *Db) BindTime(t time.Time) any {
	t = t.UTC()
	if db.Dialect == Sqlite {
		return t.Format(sqliteTimeFormat)
	}
	return t
}

func (db *Db) BindTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return db.BindTime(*t)
}

// NullTime scans a nullable timestamp from either dialect.
type NullTime struct {
	Time  time.Time
	Valid bool
}

func (n *NullTime) Scan(value any) error {
	n.Valid = false
	switch v := value.(type) {
	case nil:
		return nil
	case time.Time:
		n.Time, n.Valid = v.UTC(), true
		return nil
	case string:
		t, err := parseDbTime(v)
		if err != nil {
			return err
		}
		n.Time, n.Valid = t, true
		return nil
	case []byte:
		t, err := parseDbTime(string(v))
		if err != nil {
			return err
		}
		n.Time, n.Valid = t, true
		return nil
	default:
		return fmt.Errorf("cannot scan %T into NullTime", value)
	}
}

func (n NullTime) Ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

func parseDbTime(s string) (time.Time, error) {
	for _, layout := range []string{
		sqliteTimeFormat,
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q", s)
}

// ---- Nullable scan helpers ----

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func int64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

// IsDuplicateKey reports whether an error is a unique-constraint violation in
// either dialect.
func IsDuplicateKey(err error) bool {
	var se *sqlite3.Error
	if errors.As(err, &se) {
		return se.Code()&0xff == 19 // SQLITE_CONSTRAINT (incl. extended codes)
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23505"
	}
	return false
}
