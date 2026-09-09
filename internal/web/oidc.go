package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// OIDC single sign-on for the dashboard: authorization-code flow with PKCE
// and nonce, against any compliant IdP (Keycloak being the primary target).
// Users are auto-provisioned on first login; the token's groups claim drives
// link-group scoping and the admin role.

const oidcStateCookieName = "gort_oidc"
const oidcStateLifetime = 10 * time.Minute

// oidcClient lazily resolves the provider so the app can start (and tests
// can run) while the IdP is unreachable; discovery errors surface on login.
type oidcClient struct {
	cfg *AppConfig
	// httpClient overrides the HTTP client used for discovery, JWKS and the
	// token exchange (tests inject one that bypasses proxies).
	httpClient *http.Client

	mu       sync.Mutex
	provider *gooidc.Provider
	verifier *gooidc.IDTokenVerifier
}

func newOidcClient(cfg *AppConfig) *oidcClient {
	return &oidcClient{cfg: cfg}
}

// wrapContext threads the override client through go-oidc and oauth2.
func (o *oidcClient) wrapContext(ctx context.Context) context.Context {
	if o.httpClient != nil {
		return gooidc.ClientContext(ctx, o.httpClient)
	}
	return ctx
}

func (o *oidcClient) get(ctx context.Context) (*gooidc.Provider, *gooidc.IDTokenVerifier, error) {
	ctx = o.wrapContext(ctx)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider == nil {
		provider, err := gooidc.NewProvider(ctx, o.cfg.OidcIssuer)
		if err != nil {
			return nil, nil, fmt.Errorf("OIDC discovery against %s failed: %w", o.cfg.OidcIssuer, err)
		}
		o.provider = provider
		o.verifier = provider.Verifier(&gooidc.Config{ClientID: o.cfg.OidcClientID})
	}
	return o.provider, o.verifier, nil
}

func (a *App) oidcRedirectURL(r *http.Request) string {
	if a.Cfg.OidcRedirectURL != "" {
		return a.Cfg.OidcRedirectURL
	}
	return requestScheme(r) + "://" + r.Host + "/admin/oidc/callback"
}

func (a *App) oauth2Config(r *http.Request, provider *gooidc.Provider) *oauth2.Config {
	scopes := append([]string{gooidc.ScopeOpenID}, a.Cfg.OidcScopes...)
	return &oauth2.Config{
		ClientID:     a.Cfg.OidcClientID,
		ClientSecret: a.Cfg.OidcClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  a.oidcRedirectURL(r),
		Scopes:       scopes,
	}
}

// oidcState is the transient login state, carried in a short-lived signed
// cookie between /admin/oidc/login and the callback.
type oidcState struct {
	State     string `json:"s"`
	Nonce     string `json:"n"`
	Verifier  string `json:"v"`
	ReturnUrl string `json:"r"`
	Expires   int64  `json:"exp"`
}

func randomToken() string {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

// GET /admin/oidc/login — start the authorization-code flow.
func (a *App) uiOidcLogin(w http.ResponseWriter, r *http.Request) error {
	if !a.Cfg.OidcEnabled() {
		return errPageNotFound
	}
	provider, _, err := a.oidc.get(r.Context())
	if err != nil {
		a.Logger.Error("OIDC login failed", "error", err)
		return a.renderLogin(w, http.StatusBadGateway,
			"Single sign-on is unavailable: the identity provider could not be reached.", "/admin")
	}

	returnUrl := safeReturnUrl(r.URL.Query().Get("returnUrl"))
	state := oidcState{
		State:     randomToken(),
		Nonce:     randomToken(),
		Verifier:  oauth2.GenerateVerifier(),
		ReturnUrl: returnUrl,
		Expires:   time.Now().Add(oidcStateLifetime).Unix(),
	}
	payload, _ := json.Marshal(state)
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    a.signSession(payload),
		Path:     "/admin/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oidcStateLifetime.Seconds()),
	})

	authUrl := a.oauth2Config(r, provider).AuthCodeURL(state.State,
		gooidc.Nonce(state.Nonce),
		oauth2.S256ChallengeOption(state.Verifier))
	return redirect(w, r, authUrl)
}

func (a *App) readOidcState(r *http.Request) *oidcState {
	cookie, err := r.Cookie(oidcStateCookieName)
	if err != nil {
		return nil
	}
	// Reuse the session signing/verification path.
	session := a.verifyRaw(cookie.Value)
	if session == nil {
		return nil
	}
	var state oidcState
	if err := json.Unmarshal(session, &state); err != nil {
		return nil
	}
	if time.Now().Unix() >= state.Expires {
		return nil
	}
	return &state
}

func (a *App) clearOidcState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     "/admin/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *App) oidcLoginError(w http.ResponseWriter, message string) error {
	return a.renderLogin(w, http.StatusUnauthorized, message, "/admin")
}

// GET /admin/oidc/callback — exchange the code, verify the ID token, map
// claims and sign the user in.
func (a *App) uiOidcCallback(w http.ResponseWriter, r *http.Request) error {
	if !a.Cfg.OidcEnabled() {
		return errPageNotFound
	}
	state := a.readOidcState(r)
	a.clearOidcState(w)
	q := r.URL.Query()

	if state == nil || q.Get("state") == "" || q.Get("state") != state.State {
		return a.oidcLoginError(w, "Sign-on failed: the login attempt expired or was tampered with. Try again.")
	}
	if errCode := q.Get("error"); errCode != "" {
		a.Logger.Warn("OIDC callback returned an error", "error", errCode,
			"description", q.Get("error_description"))
		return a.oidcLoginError(w, "Sign-on failed: the identity provider rejected the login.")
	}

	provider, verifier, err := a.oidc.get(r.Context())
	if err != nil {
		a.Logger.Error("OIDC callback failed", "error", err)
		return a.oidcLoginError(w, "Sign-on failed: the identity provider could not be reached.")
	}

	ctx := a.oidc.wrapContext(r.Context())
	token, err := a.oauth2Config(r, provider).Exchange(ctx, q.Get("code"),
		oauth2.VerifierOption(state.Verifier))
	if err != nil {
		a.Logger.Warn("OIDC code exchange failed", "error", err)
		return a.oidcLoginError(w, "Sign-on failed: the authorization code could not be exchanged.")
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return a.oidcLoginError(w, "Sign-on failed: the identity provider returned no ID token.")
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		a.Logger.Warn("OIDC ID token verification failed", "error", err)
		return a.oidcLoginError(w, "Sign-on failed: the ID token could not be verified.")
	}
	if idToken.Nonce != state.Nonce {
		return a.oidcLoginError(w, "Sign-on failed: the login attempt expired or was tampered with. Try again.")
	}

	identity, err := a.identityFromToken(idToken)
	if err != nil {
		a.Logger.Warn("OIDC claims could not be read", "error", err)
		return a.oidcLoginError(w, "Sign-on failed: the ID token carried unreadable claims.")
	}

	user, err := data.UpsertOidcUser(a.Db, identity.Subject, identity.Username, identity.Role)
	if err != nil || user == nil {
		return fmt.Errorf("provisioning OIDC user: %w", err)
	}

	a.SignInWithGroups(w, user, identity.Groups)
	return redirect(w, r, state.ReturnUrl)
}

// oidcIdentity is what Gort keeps from a verified ID token.
type oidcIdentity struct {
	Subject  string
	Username string
	Groups   []string
	Role     core.UserRole
}

// identityFromToken maps ID-token claims onto Gort's model: username from
// preferred_username/email/sub, groups from the configured claim
// (normalized), admin role via membership in the configured admin group.
func (a *App) identityFromToken(idToken *gooidc.IDToken) (*oidcIdentity, error) {
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}

	str := func(name string) string {
		if v, ok := claims[name].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	username := str("preferred_username")
	if username == "" {
		username = str("email")
	}
	if username == "" {
		username = idToken.Subject
	}

	var rawGroups []string
	if list, ok := claims[a.Cfg.OidcGroupsClaim].([]any); ok {
		for _, item := range list {
			if g, ok := item.(string); ok {
				rawGroups = append(rawGroups, g)
			}
		}
	}
	groups := core.NormalizeGroups(rawGroups)

	role := core.UserRegular
	adminGroup := core.NormalizeGroup(a.Cfg.OidcAdminGroup)
	if adminGroup != "" && core.GroupsContain(groups, adminGroup) {
		role = core.UserAdmin
	}

	return &oidcIdentity{
		Subject:  idToken.Subject,
		Username: username,
		Groups:   groups,
		Role:     role,
	}, nil
}
