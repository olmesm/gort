package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type usersView struct {
	Error string
	Rows  []userRowView
}

type userRowView struct {
	Username       string
	Role           string
	IsSelf         bool
	IsLastAdmin    bool
	CanDelete      bool
	Created        string
	RoleAction     string
	PasswordAction string
	DeleteAction   string
}

func (a *App) usersViewModel(currentUser *CurrentUser, errorMessage string) (usersView, error) {
	users, err := data.ListUsers(a.Db)
	if err != nil {
		return usersView{}, err
	}
	adminCount, err := data.CountAdmins(a.Db)
	if err != nil {
		return usersView{}, err
	}

	model := usersView{Error: errorMessage}
	for _, u := range users {
		isSelf := core.UserID(u.Id) == currentUser.Id
		isLastAdmin := u.Role == core.UserAdmin.Slug() && adminCount <= 1
		model.Rows = append(model.Rows, userRowView{
			Username:       u.Username,
			Role:           u.Role,
			IsSelf:         isSelf,
			IsLastAdmin:    isLastAdmin,
			CanDelete:      !isSelf && !isLastAdmin,
			Created:        formatDateTime(u.CreatedAt),
			RoleAction:     fmt.Sprintf("/admin/users/%d/role", u.Id),
			PasswordAction: fmt.Sprintf("/admin/users/%d/password", u.Id),
			DeleteAction:   fmt.Sprintf("/admin/users/%d/delete", u.Id),
		})
	}
	return model, nil
}

func (a *App) renderUsersPage(w http.ResponseWriter, user *CurrentUser, errorMessage string) error {
	model, err := a.usersViewModel(user, errorMessage)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "users", user, "/admin/users", "Users", model)
}

// GET /admin/users (admin)
func (a *App) uiListUsers(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderUsersPage(w, user, "")
}

// POST /admin/users (admin)
func (a *App) uiCreateUser(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	role := core.UserRegular
	if r.PostFormValue("role") == "admin" {
		role = core.UserAdmin
	}
	if username == "" || len(password) < 8 {
		return a.renderUsersPage(w, user, "Username is required and the password needs at least 8 characters.")
	}
	created, err := data.InsertUser(a.Db, username, HashPassword(password), role)
	if err != nil {
		return err
	}
	if created == nil {
		return a.renderUsersPage(w, user, fmt.Sprintf("Username '%s' is already taken.", username))
	}
	return redirect(w, r, "/admin/users")
}

// POST /admin/users/{id}/role (admin)
func (a *App) uiSetUserRole(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		if err := r.ParseForm(); err == nil {
			role := core.UserRegular
			if r.PostFormValue("role") == "admin" {
				role = core.UserAdmin
			}
			target, err := data.UserByID(a.Db, core.UserID(id))
			if err != nil {
				return err
			}
			adminCount, err := data.CountAdmins(a.Db)
			if err != nil {
				return err
			}
			demotingLastAdmin := target != nil &&
				target.Role == core.UserAdmin.Slug() && role == core.UserRegular && adminCount <= 1
			if target != nil && !demotingLastAdmin {
				if _, err := data.UpdateUserRole(a.Db, core.UserID(id), role); err != nil {
					return err
				}
			}
		}
	}
	return redirect(w, r, "/admin/users")
}

// POST /admin/users/{id}/password (admin)
func (a *App) uiSetUserPassword(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errPageNotFound
	}
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	password := r.PostFormValue("password")
	if len(password) < 8 {
		return a.renderUsersPage(w, user, "Passwords need at least 8 characters.")
	}
	if _, err := data.UpdateUserPassword(a.Db, core.UserID(id), HashPassword(password)); err != nil {
		return err
	}
	return redirect(w, r, "/admin/users")
}

// POST /admin/users/{id}/delete (admin)
func (a *App) uiDeleteUser(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		target, err := data.UserByID(a.Db, core.UserID(id))
		if err != nil {
			return err
		}
		adminCount, err := data.CountAdmins(a.Db)
		if err != nil {
			return err
		}
		isSelf := target != nil && core.UserID(target.Id) == user.Id
		isLastAdmin := target != nil && target.Role == core.UserAdmin.Slug() && adminCount <= 1
		if target != nil && !isSelf && !isLastAdmin {
			if _, err := data.DeleteUser(a.Db, core.UserID(id)); err != nil {
				return err
			}
		}
	}
	return redirect(w, r, "/admin/users")
}
