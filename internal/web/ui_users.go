package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type usersView struct {
	Error string
	Users []data.UserRow
	// LastAdminId is the sole admin's id when only one is left (0 otherwise);
	// that account can be neither demoted nor deleted.
	LastAdminId core.UserID
}

func (a *App) usersViewModel(ctx context.Context, errorMessage string) (usersView, error) {
	users, err := data.ListUsers(ctx, a.Db)
	if err != nil {
		return usersView{}, err
	}
	adminCount, err := data.CountAdmins(ctx, a.Db)
	if err != nil {
		return usersView{}, err
	}

	model := usersView{Error: errorMessage, Users: users}
	if adminCount <= 1 {
		for _, u := range users {
			if u.Role == core.UserAdmin.Slug() {
				model.LastAdminId = u.Id
			}
		}
	}
	return model, nil
}

func (a *App) renderUsersPage(ctx context.Context, w http.ResponseWriter, user *CurrentUser, errorMessage string) error {
	model, err := a.usersViewModel(ctx, errorMessage)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "users", user, "/admin/users", "Users", model)
}

// GET /admin/users (admin)
func (a *App) uiListUsers(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderUsersPage(r.Context(), w, user, "")
}

// POST /admin/users (admin)
func (a *App) uiCreateUser(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	role := core.UserRegular
	if r.PostFormValue("role") == "admin" {
		role = core.UserAdmin
	}
	if username == "" || len(password) < 8 {
		return a.renderUsersPage(r.Context(), w, user, "Username is required and the password needs at least 8 characters.")
	}
	created, err := data.InsertUser(r.Context(), a.Db, username, HashPassword(password), role)
	if err != nil {
		return err
	}
	if created == nil {
		return a.renderUsersPage(r.Context(), w, user, fmt.Sprintf("Username '%s' is already taken.", username))
	}
	return redirect(w, r, "/admin/users")
}

// POST /admin/users/{id}/role (admin)
func (a *App) uiSetUserRole(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.UserID](r, "id")
	if err != nil {
		return err
	}
	role := core.UserRegular
	if r.PostFormValue("role") == "admin" {
		role = core.UserAdmin
	}
	target, err := data.UserByID(r.Context(), a.Db, id)
	if err != nil {
		return err
	}
	adminCount, err := data.CountAdmins(r.Context(), a.Db)
	if err != nil {
		return err
	}
	demotingLastAdmin := target != nil &&
		target.Role == core.UserAdmin.Slug() && role == core.UserRegular && adminCount <= 1
	if target != nil && !demotingLastAdmin {
		if _, err := data.UpdateUserRole(r.Context(), a.Db, id, role); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/users")
}

// POST /admin/users/{id}/password (admin)
func (a *App) uiSetUserPassword(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.UserID](r, "id")
	if err != nil {
		return err
	}
	password := r.PostFormValue("password")
	if len(password) < 8 {
		return a.renderUsersPage(r.Context(), w, user, "Passwords need at least 8 characters.")
	}
	if _, err := data.UpdateUserPassword(r.Context(), a.Db, id, HashPassword(password)); err != nil {
		return err
	}
	return redirect(w, r, "/admin/users")
}

// POST /admin/users/{id}/delete (admin)
func (a *App) uiDeleteUser(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.UserID](r, "id")
	if err != nil {
		return err
	}
	target, err := data.UserByID(r.Context(), a.Db, id)
	if err != nil {
		return err
	}
	adminCount, err := data.CountAdmins(r.Context(), a.Db)
	if err != nil {
		return err
	}
	isSelf := target != nil && target.Id == user.Id
	isLastAdmin := target != nil && target.Role == core.UserAdmin.Slug() && adminCount <= 1
	if target != nil && !isSelf && !isLastAdmin {
		if _, err := data.DeleteUser(r.Context(), a.Db, id); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/users")
}
