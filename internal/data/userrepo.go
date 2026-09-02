package data

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const userSelectCols = "id, username, password_hash, role, created_at"

func scanUserRow(r rowScanner) (*UserRow, error) {
	var u UserRow
	var createdAt NullTime
	if err := r.Scan(&u.Id, &u.Username, &u.PasswordHash, &u.Role, &createdAt); err != nil {
		return nil, err
	}
	u.CreatedAt = createdAt.Time
	return &u, nil
}

// InsertUser creates a user. Returns nil if the username is taken.
func InsertUser(db *Db, username, passwordHash string, role core.UserRole) (*UserRow, error) {
	res, err := db.Exec(
		`INSERT INTO users (username, password_hash, role, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (username) DO NOTHING`,
		username, passwordHash, role.Slug(), db.BindTime(time.Now()))
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, nil
	}
	return TryFindUserByUsername(db, username)
}

func TryFindUserByUsername(db *Db, username string) (*UserRow, error) {
	u, err := scanUserRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM users WHERE username = ?", userSelectCols), username))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func TryFindUserById(db *Db, id core.UserID) (*UserRow, error) {
	u, err := scanUserRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM users WHERE id = ?", userSelectCols), id.Value()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func ListUsers(db *Db) ([]UserRow, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT %s FROM users ORDER BY username", userSelectCols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func UpdateUserPassword(db *Db, id core.UserID, passwordHash string) (bool, error) {
	res, err := db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func UpdateUserRole(db *Db, id core.UserID, role core.UserRole) (bool, error) {
	res, err := db.Exec("UPDATE users SET role = ? WHERE id = ?", role.Slug(), id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func DeleteUser(db *Db, id core.UserID) (bool, error) {
	res, err := db.Exec("DELETE FROM users WHERE id = ?", id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func CountUsers(db *Db) (int64, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func CountAdmins(db *Db) (int64, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = ?", core.UserAdmin.Slug()).Scan(&count)
	return count, err
}
