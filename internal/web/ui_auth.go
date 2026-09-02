package web

import (
	"net/http"
	"strings"

	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

func loginPage(errorMessage, returnUrl string) string {
	errorNode := h.Empty()
	if errorMessage != "" {
		errorNode = alertError(errorMessage)
	}
	return layoutBare("Log in", []h.Node{
		h.E("div", []h.Attr{h.A("class", "login-wrap")},
			h.E("div", []h.Attr{h.A("class", "card login-card")},
				h.E("h1", nil, h.Text("Gort")),
				errorNode,
				h.E("form", []h.Attr{h.A("class", "stack"), h.A("method", "post"), h.A("action", "/admin/login")},
					h.E("input", []h.Attr{h.A("type", "hidden"), h.A("name", "returnUrl"), h.A("value", returnUrl)}),
					formField("Username",
						h.E("input", []h.Attr{h.A("type", "text"), h.A("name", "username"), h.Flag("required"), h.Flag("autofocus")})),
					formField("Password",
						h.E("input", []h.Attr{h.A("type", "password"), h.A("name", "password"), h.Flag("required")})),
					h.E("button", nil, h.Text("Log in"))))),
	})
}

func safeReturnUrl(url string) string {
	if strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//") {
		return url
	}
	return "/admin"
}

// GET /admin/login
func (a *App) uiLoginForm(w http.ResponseWriter, r *http.Request) {
	returnUrl := r.URL.Query().Get("returnUrl")
	if returnUrl == "" {
		returnUrl = "/admin"
	}
	returnUrl = safeReturnUrl(returnUrl)
	if a.currentUser(r) != nil {
		http.Redirect(w, r, returnUrl, http.StatusFound)
		return
	}
	respondHtml(w, http.StatusOK, loginPage("", returnUrl))
}

// POST /admin/login
func (a *App) uiLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		respondHtml(w, http.StatusBadRequest, loginPage("Invalid form submission.", "/admin"))
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	returnUrl := safeReturnUrl(r.PostFormValue("returnUrl"))
	if returnUrl == "" {
		returnUrl = "/admin"
	}

	user, err := data.TryFindUserByUsername(a.Db, username)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if user != nil && VerifyPassword(password, user.PasswordHash) {
		a.SignIn(w, user)
		http.Redirect(w, r, returnUrl, http.StatusFound)
		return
	}
	respondHtml(w, http.StatusUnauthorized, loginPage("Invalid username or password.", returnUrl))
}

// POST /admin/logout
func (a *App) uiLogout(w http.ResponseWriter, r *http.Request) {
	a.SignOut(w)
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}
