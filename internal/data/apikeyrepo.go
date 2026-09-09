package data

import (
	"context"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const apiKeySelectCols = "id, key_hash, name, role, domain_id, enabled, expires_at, created_at"

func scanAPIKeyRow(r rowScanner) (*APIKeyRow, error) {
	var k APIKeyRow
	if err := r.Scan(&k.ID, &k.KeyHash, &k.Name, &k.Role, &k.DomainID, &k.Enabled,
		asTimePtr(&k.ExpiresAt), asTime(&k.CreatedAt)); err != nil {
		return nil, err
	}
	return &k, nil
}

func InsertAPIKey(ctx context.Context, db *DB, keyHash string, name *string, role core.APIKeyRole, expiresAt *time.Time) (*APIKeyRow, error) {
	var domainID any
	if role.Kind == core.RoleDomain {
		domainID = role.DomainID.Value()
	}
	var id int64
	err := db.QueryRow(ctx,
		`INSERT INTO api_keys (key_hash, name, role, domain_id, enabled, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		keyHash, name, role.Slug(), domainID, true, db.BindTimePtr(expiresAt),
		db.BindTime(time.Now())).Scan(&id)
	if err != nil {
		return nil, err
	}
	return scanAPIKeyRow(db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id))
}

func APIKeyByHash(ctx context.Context, db *DB, keyHash string) (*APIKeyRow, error) {
	return queryOne(ctx, db, scanAPIKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE key_hash = ?", apiKeySelectCols), keyHash)
}

func ListAPIKeys(ctx context.Context, db *DB) ([]APIKeyRow, error) {
	return queryAll(ctx, db, scanAPIKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys ORDER BY created_at DESC", apiKeySelectCols))
}

func APIKeyByID(ctx context.Context, db *DB, id core.APIKeyID) (*APIKeyRow, error) {
	return queryOne(ctx, db, scanAPIKeyRow,
		fmt.Sprintf("SELECT %s FROM api_keys WHERE id = ?", apiKeySelectCols), id.Value())
}

func SetAPIKeyEnabled(ctx context.Context, db *DB, id core.APIKeyID, enabled bool) (bool, error) {
	return execAffected(ctx, db, "UPDATE api_keys SET enabled = ? WHERE id = ?", enabled, id.Value())
}

func DeleteAPIKey(ctx context.Context, db *DB, id core.APIKeyID) (bool, error) {
	return execAffected(ctx, db, "DELETE FROM api_keys WHERE id = ?", id.Value())
}
