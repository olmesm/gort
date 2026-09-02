package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

// loginPageFor renders the login card: the local password form and/or the SSO
// button, depending on configuration.
func loginPageFor(cfg *AppConfig, errorMessage, returnUrl string) string {
	errorNode := h.Empty()
	if errorMessage != "" {
		errorNode = alertError(errorMessage)
	}

	ssoButton := h.Empty()
	if cfg.OidcEnabled() {
		ssoButton = h.E("a", []h.Attr{
			h.A("class", "btn"),
			h.A("style", "display:block;text-align:center"),
			h.A("href", "/admin/oidc/login?returnUrl="+url.QueryEscape(returnUrl)),
		}, h.Text("Continue with "+cfg.OidcProviderName))
	}

	passwordForm := h.Empty()
	if !cfg.OidcEnabled() || !cfg.OidcOnly {
		passwordForm = h.Frag(
			h.IfNode(cfg.OidcEnabled(),
				h.E("p", []h.Attr{h.A("class", "muted"), h.A("style", "text-align:center;margin:0.75rem 0 0")},
					h.Text("or sign in with a local account"))),
			h.E("form", []h.Attr{h.A("class", "stack"), h.A("method", "post"), h.A("action", "/admin/login")},
				h.E("input", []h.Attr{h.A("type", "hidden"), h.A("name", "returnUrl"), h.A("value", returnUrl)}),
				formField("Username",
					h.E("input", []h.Attr{h.A("type", "text"), h.A("name", "username"), h.Flag("required"), h.Flag("autofocus")})),
				formField("Password",
					h.E("input", []h.Attr{h.A("type", "password"), h.A("name", "password"), h.Flag("required")})),
				h.E("button", nil, h.Text("Log in"))))
	}

	return layoutBare("Log in", []h.Node{
		h.E("div", []h.Attr{h.A("class", "login-wrap")},
			h.E("div", []h.Attr{h.A("class", "card login-card")},
				h.E("h1", nil, h.Text("Gort")),
				errorNode,
				ssoButton,
				passwordForm)),
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
	respondHtml(w, http.StatusOK, loginPageFor(a.Cfg, "", returnUrl))
}

// POST /admin/login
func (a *App) uiLogin(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.OidcEnabled() && a.Cfg.OidcOnly {
		respondHtml(w, http.StatusForbidden,
			loginPageFor(a.Cfg, "Password login is disabled; use single sign-on.", "/admin"))
		return
	}
	if err := r.ParseForm(); err != nil {
		respondHtml(w, http.StatusBadRequest, loginPageFor(a.Cfg, "Invalid form submission.", "/admin"))
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
	respondHtml(w, http.StatusUnauthorized, loginPageFor(a.Cfg, "Invalid username or password.", returnUrl))
}

// POST /admin/logout
func (a *App) uiLogout(w http.ResponseWriter, r *http.Request) {
	a.SignOut(w)
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}
