package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type usersView struct {
	Filters listControlsView
	Pager   pagerView
	Error   string
	Users   []data.UserRow
	// LastAdminId is the sole admin's id when only one is left (0 otherwise);
	// that account can be neither demoted nor deleted.
	LastAdminID core.UserID
}

func (a *App) usersViewModel(ctx context.Context, q url.Values, errorMessage string) (usersView, error) {
	page, err := data.ListUsersPage(ctx, a.DB, listFilters(q), q.Get("role"))
	if err != nil {
		return usersView{}, err
	}
	adminCount, err := data.CountAdmins(ctx, a.DB)
	if err != nil {
		return usersView{}, err
	}

	model := usersView{Error: errorMessage, Users: page.Items}
	model.Pager = newPager(page, func(p int) string { return listPageURL("/admin/users", q, p) })
	model.Filters = listControls("/admin/users", q, "Search username…", listSelect(q, "role", "Role", "admin", "user"))
	if adminCount <= 1 {
		for _, u := range page.Items {
			if u.Role == core.UserAdmin.Slug() {
				model.LastAdminID = u.ID
			}
		}
	}
	return model, nil
}

func (a *App) renderUsersPage(r *http.Request, w http.ResponseWriter, user *CurrentUser, errorMessage string) error {
	model, err := a.usersViewModel(r.Context(), r.URL.Query(), errorMessage)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "users", user, "/admin/users", "Users", model)
}

// GET /admin/users (admin)
func (a *App) uiListUsers(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderUsersPage(r, w, user, "")
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
		return a.renderUsersPage(r, w, user, "Username is required and the password needs at least 8 characters.")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return a.renderUsersPage(r, w, user, "Passwords must be no longer than 72 bytes.")
	}
	created, err := data.InsertUser(r.Context(), a.DB, username, hash, role)
	if err != nil {
		return err
	}
	if created == nil {
		return a.renderUsersPage(r, w, user, fmt.Sprintf("Username '%s' is already taken.", username))
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
	target, err := data.UserByID(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	adminCount, err := data.CountAdmins(r.Context(), a.DB)
	if err != nil {
		return err
	}
	demotingLastAdmin := target != nil &&
		target.Role == core.UserAdmin.Slug() && role == core.UserRegular && adminCount <= 1
	if target != nil && !demotingLastAdmin {
		if _, err := data.UpdateUserRole(r.Context(), a.DB, id, role); err != nil {
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
		return a.renderUsersPage(r, w, user, "Passwords need at least 8 characters.")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return a.renderUsersPage(r, w, user, "Passwords must be no longer than 72 bytes.")
	}
	if _, err := data.UpdateUserPassword(r.Context(), a.DB, id, hash); err != nil {
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
	target, err := data.UserByID(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	adminCount, err := data.CountAdmins(r.Context(), a.DB)
	if err != nil {
		return err
	}
	isSelf := target != nil && target.ID == user.ID
	isLastAdmin := target != nil && target.Role == core.UserAdmin.Slug() && adminCount <= 1
	if target != nil && !isSelf && !isLastAdmin {
		if _, err := data.DeleteUser(r.Context(), a.DB, id); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/users")
}
