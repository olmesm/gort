package data

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const apiKeySelectCols = "id, key_hash, name, role, domain_id, enabled, expires_at, created_at"

func scanApiKeyRow(r rowScanner) (*ApiKeyRow, error) {
	var k ApiKeyRow
	var name sql.NullString
	var domainId sql.NullInt64
	var expiresAt, createdAt NullTime
	if err := r.Scan(&k.Id, &k.KeyHash, &name, &k.Role, &domainId, &k.Enabled, &expiresAt, &createdAt); err != nil {
		return nil, err
	}
	k.Name = strPtr(name)
	k.DomainId = int64Ptr(domainId)
	k.ExpiresAt = expiresAt.Ptr()
	k.CreatedAt = createdAt.Time
	return &k, nil
}

func InsertApiKey(db *Db, keyHash string, name *string, role core.ApiKeyRole, expiresAt *time.Time) (*ApiKeyRow, error) {
	var domainId any
	if role.Kind == core.RoleDomain {
		domainId = role.DomainID.Value()
	}
	var id int64
	err := db.QueryRow(
		`INSERT INTO api_keys (key_hash, name, role, domain_id, enabled, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		keyHash, name, role.Slug(), domainId, true, db.BindTimePtr(expiresAt),
		db.BindTime(time.Now())).Scan(&id)
	if err != nil {
		return nil, err
	}
	return scanApiKeyRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id))
}

func TryFindApiKeyByHash(db *Db, keyHash string) (*ApiKeyRow, error) {
	k, err := scanApiKeyRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM api_keys WHERE key_hash = ?", apiKeySelectCols), keyHash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

func ListApiKeys(db *Db) ([]ApiKeyRow, error) {
	rows, err := db.Query(
		fmt.Sprintf("SELECT %s FROM api_keys ORDER BY created_at DESC", apiKeySelectCols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApiKeyRow
	for rows.Next() {
		k, err := scanApiKeyRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func TryGetApiKeyById(db *Db, id core.ApiKeyID) (*ApiKeyRow, error) {
	k, err := scanApiKeyRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id.Value()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

func SetApiKeyEnabled(db *Db, id core.ApiKeyID, enabled bool) (bool, error) {
	res, err := db.Exec("UPDATE api_keys SET enabled = ? WHERE id = ?", enabled, id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func DeleteApiKey(db *Db, id core.ApiKeyID) (bool, error) {
	res, err := db.Exec("DELETE FROM api_keys WHERE id = ?", id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}
