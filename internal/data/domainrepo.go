package data

import (
	"context"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const domainSelectCols = "id, authority, base_url_redirect, regular_404_redirect, invalid_short_url_redirect, is_default, created_at"

func scanDomainRow(r rowScanner) (*DomainRow, error) {
	var d DomainRow
	err := r.Scan(&d.ID, &d.Authority, &d.BaseURLRedirect, &d.Regular404Redirect,
		&d.InvalidShortURLRedirect, &d.IsDefault, asTime(&d.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// EnsureDefaultDomain makes sure the configured default domain exists and is
// flagged default.
func EnsureDefaultDomain(ctx context.Context, db *DB, authority core.DomainAuthority) (*DomainRow, error) {
	if _, err := db.Exec(ctx,
		`INSERT INTO domains (authority, is_default, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (authority) DO NOTHING`,
		authority.Value(), true, db.BindTime(time.Now())); err != nil {
		return nil, err
	}
	if _, err := db.Exec(ctx,
		"UPDATE domains SET is_default = (authority = ?)", authority.Value()); err != nil {
		return nil, err
	}
	return scanDomainRow(db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM domains WHERE authority = ?", domainSelectCols),
		authority.Value()))
}

func DomainByAuthority(ctx context.Context, db *DB, authority string) (*DomainRow, error) {
	return queryOne(ctx, db, scanDomainRow,
		fmt.Sprintf("SELECT %s FROM domains WHERE authority = ?", domainSelectCols), authority)
}

func DomainByID(ctx context.Context, db *DB, id core.DomainID) (*DomainRow, error) {
	return queryOne(ctx, db, scanDomainRow,
		fmt.Sprintf("SELECT %s FROM domains WHERE id = ?", domainSelectCols), id.Value())
}

func DefaultDomain(ctx context.Context, db *DB) (*DomainRow, error) {
	return scanDomainRow(db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM domains WHERE is_default = ? LIMIT 1", domainSelectCols), true))
}

func ListDomains(ctx context.Context, db *DB) ([]DomainRow, error) {
	return queryAll(ctx, db, scanDomainRow,
		fmt.Sprintf("SELECT %s FROM domains ORDER BY is_default DESC, authority", domainSelectCols))
}

func scanDomainStatsRow(r rowScanner) (*DomainStatsRow, error) {
	var d DomainStatsRow
	err := r.Scan(&d.ID, &d.Authority, &d.BaseURLRedirect, &d.Regular404Redirect,
		&d.InvalidShortURLRedirect, &d.IsDefault, asTime(&d.CreatedAt), &d.ShortURLCount, &d.VisitCount)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func ListDomainsWithStats(ctx context.Context, db *DB) ([]DomainStatsRow, error) {
	return queryAll(ctx, db, scanDomainStatsRow,
		`SELECT d.id, d.authority, d.base_url_redirect, d.regular_404_redirect,
		        d.invalid_short_url_redirect, d.is_default, d.created_at,
		        (SELECT COUNT(*) FROM short_urls su WHERE su.domain_id = d.id) AS short_url_count,
		        (SELECT COUNT(*) FROM visits v
		           JOIN short_urls su ON su.id = v.short_url_id
		          WHERE su.domain_id = d.id) AS visit_count
		 FROM domains d
		 ORDER BY d.is_default DESC, d.authority`)
}

// CreateDomain creates a non-default domain. Returns nil if the authority
// already exists.
func CreateDomain(ctx context.Context, db *DB, authority core.DomainAuthority) (*DomainRow, error) {
	inserted, err := execAffected(ctx, db,
		`INSERT INTO domains (authority, is_default, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (authority) DO NOTHING`,
		authority.Value(), false, db.BindTime(time.Now()))
	if err != nil || !inserted {
		return nil, err
	}
	return DomainByAuthority(ctx, db, authority.Value())
}

func UpdateDomainRedirects(ctx context.Context, db *DB, id core.DomainID, baseURLRedirect, regular404Redirect, invalidShortURLRedirect *string) (bool, error) {
	return execAffected(ctx, db,
		`UPDATE domains
		 SET base_url_redirect = ?, regular_404_redirect = ?, invalid_short_url_redirect = ?
		 WHERE id = ?`,
		baseURLRedirect, regular404Redirect, invalidShortURLRedirect, id.Value())
}

// DeleteDomain deletes a domain (cascades to its short URLs). The default
// domain cannot be deleted.
func DeleteDomain(ctx context.Context, db *DB, id core.DomainID) (bool, error) {
	return execAffected(ctx, db, "DELETE FROM domains WHERE id = ? AND is_default = ?", id.Value(), false)
}
