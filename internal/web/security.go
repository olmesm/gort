package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// ---- Passwords ----

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func VerifyPassword(password, hash string) bool {
	return len(password) <= 72 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ---- API keys ----

// AuthenticatedKey is an API key that has been authenticated for the current
// request: the stored row plus its successfully-parsed role. A key whose
// stored role cannot be parsed never reaches a handler — unknown roles are
// rejected, not defaulted.
type AuthenticatedKey struct {
	Row  data.APIKeyRow
	Role core.APIKeyRole
}

func (k *AuthenticatedKey) ID() core.APIKeyID { return core.APIKeyID(k.Row.ID) }

// GenerateApiKey generates a new plaintext API key. Only its hash is stored.
func GenerateAPIKey() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return "gort_" + base64.RawURLEncoding.EncodeToString(bytes)
}

func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func apiKeyIsUsable(row *data.APIKeyRow, now time.Time) bool {
	return row.Enabled && (row.ExpiresAt == nil || row.ExpiresAt.After(now))
}

// AuthenticateApiKey authenticates a stored key row: it must be enabled,
// unexpired and carry a parseable role.
func AuthenticateAPIKey(now time.Time, row *data.APIKeyRow) *AuthenticatedKey {
	if row == nil || !apiKeyIsUsable(row, now) {
		return nil
	}
	role, ok := core.APIKeyRoleOfStored(row.Role, row.DomainID)
	if !ok {
		return nil
	}
	return &AuthenticatedKey{Row: *row, Role: role}
}

func readAPIKeyHeader(r *http.Request) string {
	if key := r.Header.Get("X-Api-Key"); key != "" {
		return key
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// requireApiKey authenticates the request by API key and passes the
// authenticated key to the handler.
func (a *App) requireAPIKey(next apiHandler) http.HandlerFunc {
	return a.handle(func(w http.ResponseWriter, r *http.Request) error {
		key := readAPIKeyHeader(r)
		if key == "" {
			return Unauthorized("Expected an API key in the X-Api-Key header.")
		}
		row, err := data.APIKeyByHash(r.Context(), a.DB, HashAPIKey(key))
		if err != nil {
			return err
		}
		authenticated := AuthenticateAPIKey(time.Now().UTC(), row)
		if authenticated == nil {
			return Unauthorized("The provided API key is not valid.")
		}
		return next(authenticated, w, r)
	})
}

// ---- Cookie sessions for the admin dashboard ----

const sessionCookieName = "gort_session"
const sessionLifetime = 14 * 24 * time.Hour

// CurrentUser is the signed-in dashboard user for the current request.
type CurrentUser struct {
	ID       core.UserID
	Username string
	Role     core.UserRole
	// Groups are the normalized OIDC groups from the login token; empty for
	// local users.
	Groups []string
}

func (u *CurrentUser) IsAdmin() bool { return u.Role == core.UserAdmin }

// CanSeeGroup says whether this user may see and manage links carrying the
// given group (nil = ungrouped, visible to everyone signed in).
func (u *CurrentUser) CanSeeGroup(group *string) bool {
	if u.IsAdmin() || group == nil {
		return true
	}
	return core.GroupsContain(u.Groups, *group)
}

// VisibleGroups returns the group scope for list queries: nil means
// unrestricted (admin); otherwise ungrouped links plus these groups.
func (u *CurrentUser) VisibleGroups() []string {
	if u.IsAdmin() {
		return nil
	}
	if u.Groups == nil {
		return []string{}
	}
	return u.Groups
}

type sessionPayload struct {
	UID             int64    `json:"uid"`
	Groups          []string `json:"g,omitempty"`
	Expires         int64    `json:"exp"`
	OIDCExpires     int64    `json:"oidc_exp,omitempty"`
	PasswordVersion string   `json:"pv,omitempty"`
}

// passwordVersion invalidates local sessions when their stored password hash
// changes, without exposing that hash in the readable cookie payload.
func (a *App) passwordVersion(passwordHash string) string {
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte("local-password-version\x00"))
	mac.Write([]byte(passwordHash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// loadOrCreateSessionKey persists the signing key under the data dir so
// sessions survive restarts (the moral equivalent of DataProtection key
// persistence).
func loadOrCreateSessionKey(dataDir string) ([]byte, error) {
	keyDir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(keyDir, "session.key")
	if existing, err := os.ReadFile(keyPath); err == nil && len(existing) >= 32 {
		return existing, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (a *App) signSession(payload []byte) string {
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyRaw checks a signed cookie value and returns its payload bytes, or
// nil when the signature does not verify.
func (a *App) verifyRaw(cookie string) []byte {
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil
	}
	return payload
}

func (a *App) verifySession(cookie string) *sessionPayload {
	payload := a.verifyRaw(cookie)
	if payload == nil {
		return nil
	}
	var session sessionPayload
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil
	}
	if time.Now().Unix() >= session.Expires {
		return nil
	}
	return &session
}

// SignIn issues the session cookie for a local user.
func (a *App) SignIn(w http.ResponseWriter, user *data.UserRow) {
	a.signInUntil(w, user, nil, time.Now().Add(sessionLifetime))
}

// signInUntil caps the session at its authentication source's expiry.
func (a *App) signInUntil(w http.ResponseWriter, user *data.UserRow, groups []string, expires time.Time) {
	if maximum := time.Now().Add(sessionLifetime); expires.After(maximum) {
		expires = maximum
	}
	session := sessionPayload{UID: user.ID.Value(), Groups: groups, Expires: expires.Unix()}
	if user.AuthSource == "oidc" {
		session.OIDCExpires = expires.Unix()
	} else {
		session.PasswordVersion = a.passwordVersion(user.PasswordHash)
	}
	payload, _ := json.Marshal(session)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.signSession(payload),
		Path:     "/",
		HttpOnly: true,
		Secure:   a.Cfg.UseHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   max(1, int(time.Until(expires).Seconds())),
	})
}

func (a *App) SignOut(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.Cfg.UseHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// currentUser resolves the signed-in user from the session cookie, parsing
// the stored role fail-closed.
func (a *App) currentUser(r *http.Request) *CurrentUser {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	session := a.verifySession(cookie.Value)
	if session == nil {
		return nil
	}
	user, err := data.UserByID(r.Context(), a.DB, core.UserID(session.UID))
	if err != nil || user == nil {
		return nil
	}
	if a.Cfg.OIDCEnabled() && a.Cfg.OIDCOnly && user.AuthSource != "oidc" {
		return nil
	}
	// Older OIDC cookies did not carry the verified token's expiry. Require
	// a new login rather than retaining their former 14-day group grants.
	if user.AuthSource == "oidc" && time.Now().Unix() >= session.OIDCExpires {
		return nil
	}
	if user.AuthSource == "local" && !hmac.Equal([]byte(session.PasswordVersion), []byte(a.passwordVersion(user.PasswordHash))) {
		return nil
	}
	role, ok := core.UserRoleOfSlug(user.Role)
	if !ok {
		return nil
	}
	return &CurrentUser{
		ID:       user.ID,
		Username: user.Username,
		Role:     role,
		Groups:   core.NormalizeGroups(session.Groups),
	}
}

// requireUser requires a signed-in user; redirects to the login page
// otherwise.
func (a *App) requireUser(next userHandler) http.HandlerFunc {
	return a.handle(func(w http.ResponseWriter, r *http.Request) error {
		user := a.currentUser(r)
		if user == nil {
			returnURL := url.QueryEscape(r.URL.RequestURI())
			return redirect(w, r, "/admin/login?returnUrl="+returnURL)
		}
		return next(user, w, r)
	})
}

// requireAdmin requires a signed-in admin.
func (a *App) requireAdmin(next userHandler) http.HandlerFunc {
	return a.requireUser(func(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
		if !user.IsAdmin() {
			return errAdminOnly
		}
		return next(user, w, r)
	})
}
