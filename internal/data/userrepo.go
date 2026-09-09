package data

import (
	"context"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const userSelectCols = "id, username, password_hash, role, created_at, auth_source, oidc_subject"

func scanUserRow(r rowScanner) (*UserRow, error) {
	var u UserRow
	if err := r.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, asTime(&u.CreatedAt), &u.AuthSource, &u.OIDCSubject); err != nil {
		return nil, err
	}
	return &u, nil
}

// InsertUser creates a user. Returns nil if the username is taken.
func InsertUser(ctx context.Context, db *DB, username, passwordHash string, role core.UserRole) (*UserRow, error) {
	inserted, err := execAffected(ctx, db,
		`INSERT INTO users (username, password_hash, role, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (username) DO NOTHING`,
		username, passwordHash, role.Slug(), db.BindTime(time.Now()))
	if err != nil || !inserted {
		return nil, err
	}
	return UserByUsername(ctx, db, username)
}

func UserByUsername(ctx context.Context, db *DB, username string) (*UserRow, error) {
	return queryOne(ctx, db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users WHERE username = ?", userSelectCols), username)
}

func UserByID(ctx context.Context, db *DB, id core.UserID) (*UserRow, error) {
	return queryOne(ctx, db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users WHERE id = ?", userSelectCols), id.Value())
}

func ListUsers(ctx context.Context, db *DB) ([]UserRow, error) {
	return queryAll(ctx, db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users ORDER BY username", userSelectCols))
}

func UpdateUserPassword(ctx context.Context, db *DB, id core.UserID, passwordHash string) (bool, error) {
	return execAffected(ctx, db, "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id.Value())
}

func UpdateUserRole(ctx context.Context, db *DB, id core.UserID, role core.UserRole) (bool, error) {
	return execAffected(ctx, db, "UPDATE users SET role = ? WHERE id = ?", role.Slug(), id.Value())
}

func DeleteUser(ctx context.Context, db *DB, id core.UserID) (bool, error) {
	return execAffected(ctx, db, "DELETE FROM users WHERE id = ?", id.Value())
}

func CountUsers(ctx context.Context, db *DB) (int64, error) {
	return queryScalar[int64](ctx, db, "SELECT COUNT(*) FROM users")
}

// UserBySubject looks up an SSO-provisioned user by its stable OIDC subject.
func UserBySubject(ctx context.Context, db *DB, subject string) (*UserRow, error) {
	return queryOne(ctx, db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users WHERE oidc_subject = ?", userSelectCols), subject)
}

// UpsertOidcUser provisions or refreshes the local shadow row for an OIDC
// user, matching on the stable subject. Password login is impossible for
// these rows (the stored hash is a sentinel no bcrypt hash can equal).
// Returns the current row.
func UpsertOIDCUser(ctx context.Context, db *DB, subject, username string, role core.UserRole) (*UserRow, error) {
	existing, err := UserBySubject(ctx, db, subject)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if _, err := db.Exec(ctx,
			"UPDATE users SET username = ?, role = ? WHERE id = ?",
			username, role.Slug(), existing.ID); err != nil {
			// A username collision with another account keeps the old name;
			// the role update below still matters, so retry it alone.
			if !IsDuplicateKey(err) {
				return nil, err
			}
			if _, err := db.Exec(ctx, "UPDATE users SET role = ? WHERE id = ?", role.Slug(), existing.ID); err != nil {
				return nil, err
			}
		}
		return UserBySubject(ctx, db, subject)
	}

	insert := func(name string) (bool, error) {
		return execAffected(ctx, db,
			`INSERT INTO users (username, password_hash, role, created_at, auth_source, oidc_subject)
			 VALUES (?, '!oidc', ?, ?, 'oidc', ?)
			 ON CONFLICT (username) DO NOTHING`,
			name, role.Slug(), db.BindTime(time.Now()), subject)
	}

	inserted, err := insert(username)
	if err != nil {
		return nil, err
	}
	if !inserted {
		// The username belongs to a different account (e.g. a local user);
		// fall back to a subject-suffixed name to keep usernames unique.
		suffix := subject
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		if _, err := insert(username + "-" + suffix); err != nil {
			return nil, err
		}
	}
	return UserBySubject(ctx, db, subject)
}

func CountAdmins(ctx context.Context, db *DB) (int64, error) {
	return queryScalar[int64](ctx, db, "SELECT COUNT(*) FROM users WHERE role = ?", core.UserAdmin.Slug())
}
