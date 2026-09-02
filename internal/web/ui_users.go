package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

func (a *App) usersPageContent(currentUser *CurrentUser, banner h.Node) ([]h.Node, error) {
	users, err := data.ListUsers(a.Db)
	if err != nil {
		return nil, err
	}
	adminCount, err := data.CountAdmins(a.Db)
	if err != nil {
		return nil, err
	}

	var rows []h.Node
	for _, u := range users {
		isSelf := core.UserID(u.Id) == currentUser.Id
		isLastAdmin := u.Role == core.UserAdmin.Slug() && adminCount <= 1

		rows = append(rows, h.E("tr", nil,
			h.E("td", nil,
				h.Text(u.Username),
				h.IfNode(isSelf, h.Text(" ")),
				h.IfNode(isSelf, h.E("span", []h.Attr{h.A("class", "badge gray")}, h.Text("you")))),
			h.E("td", nil,
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/users/%d/role", u.Id)),
				},
					h.E("select", []h.Attr{
						h.A("name", "role"),
						h.If(isLastAdmin, h.Flag("disabled")),
					},
						h.E("option", []h.Attr{
							h.A("value", "user"),
							h.If(u.Role == "user", h.Flag("selected")),
						}, h.Text("user")),
						h.E("option", []h.Attr{
							h.A("value", "admin"),
							h.If(u.Role == "admin", h.Flag("selected")),
						}, h.Text("admin"))),
					h.Text(" "),
					h.IfNode(!isLastAdmin,
						h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text("Set"))))),
			h.E("td", []h.Attr{h.A("class", "muted")}, h.Text(formatDateTime(u.CreatedAt))),
			h.E("td", nil,
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/users/%d/password", u.Id)),
				},
					h.E("input", []h.Attr{
						h.A("type", "password"), h.A("name", "password"),
						h.A("placeholder", "New password"), h.A("style", "width:11rem"),
					}),
					h.Text(" "),
					h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text("Update")))),
			h.E("td", []h.Attr{h.A("class", "actions")},
				h.IfNode(!isSelf && !isLastAdmin,
					h.E("form", []h.Attr{
						h.A("class", "inline"), h.A("method", "post"),
						h.A("action", fmt.Sprintf("/admin/users/%d/delete", u.Id)),
						h.A("onsubmit", "return confirm('Delete this user?')"),
					},
						h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Delete")))))))
	}

	return []h.Node{
		h.E("h1", nil, h.Text("Users")),
		h.E("p", []h.Attr{h.A("class", "muted")},
			h.Text("Dashboard accounts. Admins additionally manage domains, API keys, webhooks and users.")),
		banner,
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("Username")),
						h.E("th", nil, h.Text("Role")),
						h.E("th", nil, h.Text("Created (UTC)")),
						h.E("th", nil, h.Text("Set new password")),
						h.E("th", nil))),
				h.E("tbody", nil, rows...))),
		h.E("h2", nil, h.Text("Create user")),
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("form", []h.Attr{h.A("class", "row"), h.A("method", "post"), h.A("action", "/admin/users")},
				formField("Username", textInput("username", "", "")),
				formField("Password",
					h.E("input", []h.Attr{h.A("type", "password"), h.A("name", "password"), h.Flag("required")})),
				formField("Role",
					h.E("select", []h.Attr{h.A("name", "role")},
						h.E("option", []h.Attr{h.A("value", "user")}, h.Text("user")),
						h.E("option", []h.Attr{h.A("value", "admin")}, h.Text("admin")))),
				h.E("div", nil, h.E("button", nil, h.Text("Create user"))))),
	}, nil
}

func (a *App) respondUsersPage(w http.ResponseWriter, user *CurrentUser, banner h.Node) {
	content, err := a.usersPageContent(user, banner)
	if err != nil {
		a.serverError(w, err)
		return
	}
	respondPage(w, user, "/admin/users", "Users", content)
}

// GET /admin/users (admin)
func (a *App) uiListUsers(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	a.respondUsersPage(w, user, h.Empty())
}

// POST /admin/users (admin)
func (a *App) uiCreateUser(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	role := core.UserRegular
	if r.PostFormValue("role") == "admin" {
		role = core.UserAdmin
	}
	if username == "" || len(password) < 8 {
		a.respondUsersPage(w, user,
			alertError("Username is required and the password needs at least 8 characters."))
		return
	}
	created, err := data.InsertUser(a.Db, username, HashPassword(password), role)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if created == nil {
		a.respondUsersPage(w, user,
			alertError(fmt.Sprintf("Username '%s' is already taken.", username)))
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

// POST /admin/users/{id}/role (admin)
func (a *App) uiSetUserRole(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		if err := r.ParseForm(); err == nil {
			role := core.UserRegular
			if r.PostFormValue("role") == "admin" {
				role = core.UserAdmin
			}
			target, err := data.TryFindUserById(a.Db, core.UserID(id))
			if err != nil {
				a.serverError(w, err)
				return
			}
			adminCount, err := data.CountAdmins(a.Db)
			if err != nil {
				a.serverError(w, err)
				return
			}
			demotingLastAdmin := target != nil &&
				target.Role == core.UserAdmin.Slug() && role == core.UserRegular && adminCount <= 1
			if target != nil && !demotingLastAdmin {
				if _, err := data.UpdateUserRole(a.Db, core.UserID(id), role); err != nil {
					a.serverError(w, err)
					return
				}
			}
		}
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

// POST /admin/users/{id}/password (admin)
func (a *App) uiSetUserPassword(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondPlainNotFound(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	password := r.PostFormValue("password")
	if len(password) < 8 {
		a.respondUsersPage(w, user, alertError("Passwords need at least 8 characters."))
		return
	}
	if _, err := data.UpdateUserPassword(a.Db, core.UserID(id), HashPassword(password)); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

// POST /admin/users/{id}/delete (admin)
func (a *App) uiDeleteUser(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		target, err := data.TryFindUserById(a.Db, core.UserID(id))
		if err != nil {
			a.serverError(w, err)
			return
		}
		adminCount, err := data.CountAdmins(a.Db)
		if err != nil {
			a.serverError(w, err)
			return
		}
		isSelf := target != nil && core.UserID(target.Id) == user.Id
		isLastAdmin := target != nil && target.Role == core.UserAdmin.Slug() && adminCount <= 1
		if target != nil && !isSelf && !isLastAdmin {
			if _, err := data.DeleteUser(a.Db, core.UserID(id)); err != nil {
				a.serverError(w, err)
				return
			}
		}
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}
