package data

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const domainSelectCols = "id, authority, base_url_redirect, regular_404_redirect, invalid_short_url_redirect, is_default, created_at"

func scanDomainRow(r rowScanner) (*DomainRow, error) {
	var d DomainRow
	var baseUrl, regular404, invalid sql.NullString
	var createdAt NullTime
	err := r.Scan(&d.Id, &d.Authority, &baseUrl, &regular404, &invalid, &d.IsDefault, &createdAt)
	if err != nil {
		return nil, err
	}
	d.BaseUrlRedirect = strPtr(baseUrl)
	d.Regular404Redirect = strPtr(regular404)
	d.InvalidShortUrlRedirect = strPtr(invalid)
	d.CreatedAt = createdAt.Time
	return &d, nil
}

// EnsureDefaultDomain makes sure the configured default domain exists and is
// flagged default.
func EnsureDefaultDomain(db *Db, authority core.DomainAuthority) (*DomainRow, error) {
	if _, err := db.Exec(
		`INSERT INTO domains (authority, is_default, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (authority) DO NOTHING`,
		authority.Value(), true, db.BindTime(time.Now())); err != nil {
		return nil, err
	}
	if _, err := db.Exec(
		"UPDATE domains SET is_default = (authority = ?)", authority.Value()); err != nil {
		return nil, err
	}
	return scanDomainRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM domains WHERE authority = ?", domainSelectCols),
		authority.Value()))
}

func DomainByAuthority(db *Db, authority string) (*DomainRow, error) {
	return queryOne(db, scanDomainRow,
		fmt.Sprintf("SELECT %s FROM domains WHERE authority = ?", domainSelectCols), authority)
}

func DomainByID(db *Db, id core.DomainID) (*DomainRow, error) {
	return queryOne(db, scanDomainRow,
		fmt.Sprintf("SELECT %s FROM domains WHERE id = ?", domainSelectCols), id.Value())
}

func DefaultDomain(db *Db) (*DomainRow, error) {
	return scanDomainRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM domains WHERE is_default = ? LIMIT 1", domainSelectCols), true))
}

func ListDomains(db *Db) ([]DomainRow, error) {
	return queryAll(db, scanDomainRow,
		fmt.Sprintf("SELECT %s FROM domains ORDER BY is_default DESC, authority", domainSelectCols))
}

func scanDomainStatsRow(r rowScanner) (*DomainStatsRow, error) {
	var d DomainStatsRow
	var baseUrl, regular404, invalid sql.NullString
	var createdAt NullTime
	err := r.Scan(&d.Id, &d.Authority, &baseUrl, &regular404, &invalid, &d.IsDefault,
		&createdAt, &d.ShortUrlCount, &d.VisitCount)
	if err != nil {
		return nil, err
	}
	d.BaseUrlRedirect = strPtr(baseUrl)
	d.Regular404Redirect = strPtr(regular404)
	d.InvalidShortUrlRedirect = strPtr(invalid)
	d.CreatedAt = createdAt.Time
	return &d, nil
}

func ListDomainsWithStats(db *Db) ([]DomainStatsRow, error) {
	return queryAll(db, scanDomainStatsRow,
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
func CreateDomain(db *Db, authority core.DomainAuthority) (*DomainRow, error) {
	inserted, err := execAffected(db,
		`INSERT INTO domains (authority, is_default, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (authority) DO NOTHING`,
		authority.Value(), false, db.BindTime(time.Now()))
	if err != nil || !inserted {
		return nil, err
	}
	return DomainByAuthority(db, authority.Value())
}

func UpdateDomainRedirects(db *Db, id core.DomainID, baseUrlRedirect, regular404Redirect, invalidShortUrlRedirect *string) (bool, error) {
	return execAffected(db,
		`UPDATE domains
		 SET base_url_redirect = ?, regular_404_redirect = ?, invalid_short_url_redirect = ?
		 WHERE id = ?`,
		baseUrlRedirect, regular404Redirect, invalidShortUrlRedirect, id.Value())
}

// DeleteDomain deletes a domain (cascades to its short URLs). The default
// domain cannot be deleted.
func DeleteDomain(db *Db, id core.DomainID) (bool, error) {
	return execAffected(db, "DELETE FROM domains WHERE id = ? AND is_default = ?", id.Value(), false)
}
