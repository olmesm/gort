package web

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/olmesm/gort/internal/data"
)

func TestPasswordByteLimit(t *testing.T) {
	for _, password := range []string{strings.Repeat("a", 72), strings.Repeat("é", 36)} {
		hash, err := HashPassword(password)
		if err != nil || !VerifyPassword(password, hash) {
			t.Fatalf("valid 72-byte password: %v", err)
		}
		if VerifyPassword(password+"x", hash) {
			t.Fatal("accepted an overlong password matching only its first 72 bytes")
		}
		if _, err := HashPassword(password + "x"); !errors.Is(err, bcrypt.ErrPasswordTooLong) {
			t.Fatalf("overlong password: %v", err)
		}
	}
}

func TestDashboardRejectsOverlongPasswords(t *testing.T) {
	app := newTestApp(t)
	ui := dashboardAdmin(t, app)
	admin, err := data.UserByUsername(t.Context(), app.DB, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/users", fmt.Sprintf("/admin/users/%d/password", admin.ID)} {
		form := url.Values{"username": {"too-long"}, "password": {strings.Repeat("é", 37)}}
		r := ui.postForm(path, form.Encode())
		if r.Code != 200 || !strings.Contains(r.Body.String(), "no longer than 72 bytes") {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
	if user, err := data.UserByUsername(t.Context(), app.DB, "too-long"); err != nil || user != nil {
		t.Fatalf("invalid user persisted: %v %v", user, err)
	}
	current, err := data.UserByID(t.Context(), app.DB, admin.ID)
	if err != nil || current.PasswordHash != admin.PasswordHash {
		t.Fatalf("password changed after validation failed: %v", err)
	}
}

func TestBootstrapRejectsOverlongPassword(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.DB.Exec(t.Context(), "DELETE FROM users"); err != nil {
		t.Fatal(err)
	}
	app.Cfg.InitialAdminPassword = strings.Repeat("x", 73)
	if err := app.initialize(t.Context()); !errors.Is(err, bcrypt.ErrPasswordTooLong) {
		t.Fatalf("expected startup error for overlong password: %v", err)
	}
	if count, err := data.CountUsers(t.Context(), app.DB); err != nil || count != 0 {
		t.Fatalf("invalid bootstrap user persisted: count %d, %v", count, err)
	}
}
