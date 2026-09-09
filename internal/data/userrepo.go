package data

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const userSelectCols = "id, username, password_hash, role, created_at, auth_source, oidc_subject"

func scanUserRow(r rowScanner) (*UserRow, error) {
	var u UserRow
	var createdAt NullTime
	var oidcSubject sql.NullString
	if err := r.Scan(&u.Id, &u.Username, &u.PasswordHash, &u.Role, &createdAt, &u.AuthSource, &oidcSubject); err != nil {
		return nil, err
	}
	u.CreatedAt = createdAt.Time
	u.OidcSubject = strPtr(oidcSubject)
	return &u, nil
}

// InsertUser creates a user. Returns nil if the username is taken.
func InsertUser(db *Db, username, passwordHash string, role core.UserRole) (*UserRow, error) {
	inserted, err := execAffected(db,
		`INSERT INTO users (username, password_hash, role, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (username) DO NOTHING`,
		username, passwordHash, role.Slug(), db.BindTime(time.Now()))
	if err != nil || !inserted {
		return nil, err
	}
	return UserByUsername(db, username)
}

func UserByUsername(db *Db, username string) (*UserRow, error) {
	return queryOne(db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users WHERE username = ?", userSelectCols), username)
}

func UserByID(db *Db, id core.UserID) (*UserRow, error) {
	return queryOne(db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users WHERE id = ?", userSelectCols), id.Value())
}

func ListUsers(db *Db) ([]UserRow, error) {
	return queryAll(db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users ORDER BY username", userSelectCols))
}

func UpdateUserPassword(db *Db, id core.UserID, passwordHash string) (bool, error) {
	return execAffected(db, "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id.Value())
}

func UpdateUserRole(db *Db, id core.UserID, role core.UserRole) (bool, error) {
	return execAffected(db, "UPDATE users SET role = ? WHERE id = ?", role.Slug(), id.Value())
}

func DeleteUser(db *Db, id core.UserID) (bool, error) {
	return execAffected(db, "DELETE FROM users WHERE id = ?", id.Value())
}

func CountUsers(db *Db) (int64, error) {
	return queryScalar[int64](db, "SELECT COUNT(*) FROM users")
}

// UserBySubject looks up an SSO-provisioned user by its stable OIDC subject.
func UserBySubject(db *Db, subject string) (*UserRow, error) {
	return queryOne(db, scanUserRow,
		fmt.Sprintf("SELECT %s FROM users WHERE oidc_subject = ?", userSelectCols), subject)
}

// UpsertOidcUser provisions or refreshes the local shadow row for an OIDC
// user, matching on the stable subject. Password login is impossible for
// these rows (the stored hash is a sentinel no bcrypt hash can equal).
// Returns the current row.
func UpsertOidcUser(db *Db, subject, username string, role core.UserRole) (*UserRow, error) {
	existing, err := UserBySubject(db, subject)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if _, err := db.Exec(
			"UPDATE users SET username = ?, role = ? WHERE id = ?",
			username, role.Slug(), existing.Id); err != nil {
			// A username collision with another account keeps the old name;
			// the role update below still matters, so retry it alone.
			if !IsDuplicateKey(err) {
				return nil, err
			}
			if _, err := db.Exec("UPDATE users SET role = ? WHERE id = ?", role.Slug(), existing.Id); err != nil {
				return nil, err
			}
		}
		return UserBySubject(db, subject)
	}

	insert := func(name string) (bool, error) {
		return execAffected(db,
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
	return UserBySubject(db, subject)
}

func CountAdmins(db *Db) (int64, error) {
	return queryScalar[int64](db, "SELECT COUNT(*) FROM users WHERE role = ?", core.UserAdmin.Slug())
}
