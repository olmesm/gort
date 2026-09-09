package data

import (
	"database/sql"
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

func ApiKeyByHash(db *Db, keyHash string) (*ApiKeyRow, error) {
	return queryOne(db, scanApiKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE key_hash = ?", apiKeySelectCols), keyHash)
}

func ListApiKeys(db *Db) ([]ApiKeyRow, error) {
	return queryAll(db, scanApiKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys ORDER BY created_at DESC", apiKeySelectCols))
}

func ApiKeyByID(db *Db, id core.ApiKeyID) (*ApiKeyRow, error) {
	return queryOne(db, scanApiKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id.Value())
}

func SetApiKeyEnabled(db *Db, id core.ApiKeyID, enabled bool) (bool, error) {
	return execAffected(db, "UPDATE api_keys SET enabled = ? WHERE id = ?", enabled, id.Value())
}

func DeleteApiKey(db *Db, id core.ApiKeyID) (bool, error) {
	return execAffected(db, "DELETE FROM api_keys WHERE id = ?", id.Value())
}
