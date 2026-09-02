package data

import (
	"database/sql"
	"errors"
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

func TryGetDomainByAuthority(db *Db, authority string) (*DomainRow, error) {
	d, err := scanDomainRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM domains WHERE authority = ?", domainSelectCols), authority))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func TryGetDomainById(db *Db, id core.DomainID) (*DomainRow, error) {
	d, err := scanDomainRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM domains WHERE id = ?", domainSelectCols), id.Value()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func GetDefaultDomain(db *Db) (*DomainRow, error) {
	return scanDomainRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM domains WHERE is_default = ? LIMIT 1", domainSelectCols), true))
}

func ListDomains(db *Db) ([]DomainRow, error) {
	rows, err := db.Query(
		fmt.Sprintf("SELECT %s FROM domains ORDER BY is_default DESC, authority", domainSelectCols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainRow
	for rows.Next() {
		d, err := scanDomainRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func ListDomainsWithStats(db *Db) ([]DomainStatsRow, error) {
	rows, err := db.Query(
		`SELECT d.id, d.authority, d.base_url_redirect, d.regular_404_redirect,
		        d.invalid_short_url_redirect, d.is_default, d.created_at,
		        (SELECT COUNT(*) FROM short_urls su WHERE su.domain_id = d.id) AS short_url_count,
		        (SELECT COUNT(*) FROM visits v
		           JOIN short_urls su ON su.id = v.short_url_id
		          WHERE su.domain_id = d.id) AS visit_count
		 FROM domains d
		 ORDER BY d.is_default DESC, d.authority`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainStatsRow
	for rows.Next() {
		var d DomainStatsRow
		var baseUrl, regular404, invalid sql.NullString
		var createdAt NullTime
		err := rows.Scan(&d.Id, &d.Authority, &baseUrl, &regular404, &invalid, &d.IsDefault,
			&createdAt, &d.ShortUrlCount, &d.VisitCount)
		if err != nil {
			return nil, err
		}
		d.BaseUrlRedirect = strPtr(baseUrl)
		d.Regular404Redirect = strPtr(regular404)
		d.InvalidShortUrlRedirect = strPtr(invalid)
		d.CreatedAt = createdAt.Time
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateDomain creates a non-default domain. Returns nil if the authority
// already exists.
func CreateDomain(db *Db, authority core.DomainAuthority) (*DomainRow, error) {
	res, err := db.Exec(
		`INSERT INTO domains (authority, is_default, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (authority) DO NOTHING`,
		authority.Value(), false, db.BindTime(time.Now()))
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, nil
	}
	return TryGetDomainByAuthority(db, authority.Value())
}

func UpdateDomainRedirects(db *Db, id core.DomainID, baseUrlRedirect, regular404Redirect, invalidShortUrlRedirect *string) (bool, error) {
	res, err := db.Exec(
		`UPDATE domains
		 SET base_url_redirect = ?, regular_404_redirect = ?, invalid_short_url_redirect = ?
		 WHERE id = ?`,
		baseUrlRedirect, regular404Redirect, invalidShortUrlRedirect, id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

// DeleteDomain deletes a domain (cascades to its short URLs). The default
// domain cannot be deleted.
func DeleteDomain(db *Db, id core.DomainID) (bool, error) {
	res, err := db.Exec("DELETE FROM domains WHERE id = ? AND is_default = ?", id.Value(), false)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}
