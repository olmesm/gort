package data

import (
	"context"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const apiKeySelectCols = "id, key_hash, name, role, domain_id, enabled, expires_at, created_at"

func scanApiKeyRow(r rowScanner) (*ApiKeyRow, error) {
	var k ApiKeyRow
	if err := r.Scan(&k.Id, &k.KeyHash, &k.Name, &k.Role, &k.DomainId, &k.Enabled,
		asTimePtr(&k.ExpiresAt), asTime(&k.CreatedAt)); err != nil {
		return nil, err
	}
	return &k, nil
}

func InsertApiKey(ctx context.Context, db *Db, keyHash string, name *string, role core.ApiKeyRole, expiresAt *time.Time) (*ApiKeyRow, error) {
	var domainId any
	if role.Kind == core.RoleDomain {
		domainId = role.DomainID.Value()
	}
	var id int64
	err := db.QueryRow(ctx,
		`INSERT INTO api_keys (key_hash, name, role, domain_id, enabled, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		keyHash, name, role.Slug(), domainId, true, db.BindTimePtr(expiresAt),
		db.BindTime(time.Now())).Scan(&id)
	if err != nil {
		return nil, err
	}
	return scanApiKeyRow(db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id))
}

func ApiKeyByHash(ctx context.Context, db *Db, keyHash string) (*ApiKeyRow, error) {
	return queryOne(ctx, db, scanApiKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE key_hash = ?", apiKeySelectCols), keyHash)
}

func ListApiKeys(ctx context.Context, db *Db) ([]ApiKeyRow, error) {
	return queryAll(ctx, db, scanApiKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys ORDER BY created_at DESC", apiKeySelectCols))
}

func ApiKeyByID(ctx context.Context, db *Db, id core.ApiKeyID) (*ApiKeyRow, error) {
	return queryOne(ctx, db, scanApiKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id.Value())
}

func SetApiKeyEnabled(ctx context.Context, db *Db, id core.ApiKeyID, enabled bool) (bool, error) {
	return execAffected(ctx, db, "UPDATE api_keys SET enabled = ? WHERE id = ?", enabled, id.Value())
}

func DeleteApiKey(ctx context.Context, db *Db, id core.ApiKeyID) (bool, error) {
	return execAffected(ctx, db, "DELETE FROM api_keys WHERE id = ?", id.Value())
}
