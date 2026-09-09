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
	ReturnURL        string
	ReturnURLParam   string
	OIDCEnabled      bool
	ShowPasswordForm bool
	ProviderName     string
}

func (a *App) renderLogin(w http.ResponseWriter, status int, errorMessage, returnURL string) error {
	return a.renderShared(w, status, "login", loginView{
		Error:            errorMessage,
		ReturnURL:        returnURL,
		ReturnURLParam:   url.QueryEscape(returnURL),
		OIDCEnabled:      a.Cfg.OIDCEnabled(),
		ShowPasswordForm: !a.Cfg.OIDCEnabled() || !a.Cfg.OIDCOnly,
		ProviderName:     a.Cfg.OIDCProviderName,
	})
}

func safeReturnURL(url string) string {
	if strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//") {
		return url
	}
	return "/admin"
}

// GET /admin/login
func (a *App) uiLoginForm(w http.ResponseWriter, r *http.Request) error {
	returnURL := r.URL.Query().Get("returnUrl")
	if returnURL == "" {
		returnURL = "/admin"
	}
	returnURL = safeReturnURL(returnURL)
	if a.currentUser(r) != nil {
		return redirect(w, r, returnURL)
	}
	return a.renderLogin(w, http.StatusOK, "", returnURL)
}

// POST /admin/login
func (a *App) uiLogin(w http.ResponseWriter, r *http.Request) error {
	if a.Cfg.OIDCEnabled() && a.Cfg.OIDCOnly {
		return a.renderLogin(w, http.StatusForbidden, "Password login is disabled; use single sign-on.", "/admin")
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	returnURL := safeReturnURL(r.PostFormValue("returnUrl"))

	user, err := data.UserByUsername(r.Context(), a.DB, username)
	if err != nil {
		return err
	}
	if user != nil && VerifyPassword(password, user.PasswordHash) {
		a.SignIn(w, user)
		return redirect(w, r, returnURL)
	}
	return a.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.", returnURL)
}

// POST /admin/logout
func (a *App) uiLogout(w http.ResponseWriter, r *http.Request) error {
	a.SignOut(w)
	return redirect(w, r, "/admin/login")
}
