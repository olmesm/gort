package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/olmesm/gort/internal/data"
)

// loginView drives the login card: the local password form and/or the SSO
// button, depending on configuration.
type loginView struct {
	Error            string
	ReturnUrl        string
	ReturnUrlParam   string
	OidcEnabled      bool
	ShowPasswordForm bool
	ProviderName     string
}

func (a *App) renderLogin(w http.ResponseWriter, status int, errorMessage, returnUrl string) {
	a.renderShared(w, status, "login", loginView{
		Error:            errorMessage,
		ReturnUrl:        returnUrl,
		ReturnUrlParam:   url.QueryEscape(returnUrl),
		OidcEnabled:      a.Cfg.OidcEnabled(),
		ShowPasswordForm: !a.Cfg.OidcEnabled() || !a.Cfg.OidcOnly,
		ProviderName:     a.Cfg.OidcProviderName,
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
	a.renderLogin(w, http.StatusOK, "", returnUrl)
}

// POST /admin/login
func (a *App) uiLogin(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.OidcEnabled() && a.Cfg.OidcOnly {
		a.renderLogin(w, http.StatusForbidden, "Password login is disabled; use single sign-on.", "/admin")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderLogin(w, http.StatusBadRequest, "Invalid form submission.", "/admin")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	returnUrl := safeReturnUrl(r.PostFormValue("returnUrl"))

	user, err := data.UserByUsername(a.Db, username)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if user != nil && VerifyPassword(password, user.PasswordHash) {
		a.SignIn(w, user)
		http.Redirect(w, r, returnUrl, http.StatusFound)
		return
	}
	a.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.", returnUrl)
}

// POST /admin/logout
func (a *App) uiLogout(w http.ResponseWriter, r *http.Request) {
	a.SignOut(w)
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}
