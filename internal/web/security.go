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

func HashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err) // only fails on invalid cost
	}
	return string(hash)
}

func VerifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ---- API keys ----

// AuthenticatedKey is an API key that has been authenticated for the current
// request: the stored row plus its successfully-parsed role. A key whose
// stored role cannot be parsed never reaches a handler — unknown roles are
// rejected, not defaulted.
type AuthenticatedKey struct {
	Row  data.ApiKeyRow
	Role core.ApiKeyRole
}

func (k *AuthenticatedKey) Id() core.ApiKeyID { return core.ApiKeyID(k.Row.Id) }

// GenerateApiKey generates a new plaintext API key. Only its hash is stored.
func GenerateApiKey() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return "gort_" + base64.RawURLEncoding.EncodeToString(bytes)
}

func HashApiKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func apiKeyIsUsable(row *data.ApiKeyRow, now time.Time) bool {
	return row.Enabled && (row.ExpiresAt == nil || row.ExpiresAt.After(now))
}

// AuthenticateApiKey authenticates a stored key row: it must be enabled,
// unexpired and carry a parseable role.
func AuthenticateApiKey(now time.Time, row *data.ApiKeyRow) *AuthenticatedKey {
	if row == nil || !apiKeyIsUsable(row, now) {
		return nil
	}
	role, ok := core.ApiKeyRoleOfStored(row.Role, row.DomainId)
	if !ok {
		return nil
	}
	return &AuthenticatedKey{Row: *row, Role: role}
}

func readApiKeyHeader(r *http.Request) string {
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
func (a *App) requireApiKey(handler func(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := readApiKeyHeader(r)
		if key == "" {
			Unauthorized(w, "Expected an API key in the X-Api-Key header.")
			return
		}
		row, err := data.ApiKeyByHash(a.Db, HashApiKey(key))
		if err != nil {
			a.serverError(w, err)
			return
		}
		authenticated := AuthenticateApiKey(time.Now().UTC(), row)
		if authenticated == nil {
			Unauthorized(w, "The provided API key is not valid.")
			return
		}
		handler(authenticated, w, r)
	}
}

// requireAdminKey authenticates and requires the admin role.
func (a *App) requireAdminKey(handler func(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return a.requireApiKey(func(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
		if key.Role.Kind != core.RoleAdmin {
			Forbidden(w, "This operation requires an admin API key.")
			return
		}
		handler(key, w, r)
	})
}

// ---- Cookie sessions for the admin dashboard ----

const sessionCookieName = "gort_session"
const sessionLifetime = 14 * 24 * time.Hour

// CurrentUser is the signed-in dashboard user for the current request.
type CurrentUser struct {
	Id       core.UserID
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
	Uid      int64    `json:"uid"`
	Username string   `json:"u"`
	Role     string   `json:"r"`
	Groups   []string `json:"g,omitempty"`
	Expires  int64    `json:"exp"`
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
	a.SignInWithGroups(w, user, nil)
}

// SignInWithGroups issues the session cookie carrying the user's OIDC groups.
func (a *App) SignInWithGroups(w http.ResponseWriter, user *data.UserRow, groups []string) {
	payload, _ := json.Marshal(sessionPayload{
		Uid:      user.Id,
		Username: user.Username,
		Role:     user.Role,
		Groups:   groups,
		Expires:  time.Now().Add(sessionLifetime).Unix(),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.signSession(payload),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionLifetime.Seconds()),
	})
}

func (a *App) SignOut(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
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
	role, ok := core.UserRoleOfSlug(session.Role)
	if !ok {
		return nil
	}
	return &CurrentUser{
		Id:       core.UserID(session.Uid),
		Username: session.Username,
		Role:     role,
		Groups:   core.NormalizeGroups(session.Groups),
	}
}

// requireUser requires a signed-in user; redirects to the login page
// otherwise.
func (a *App) requireUser(handler func(user *CurrentUser, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := a.currentUser(r)
		if user == nil {
			returnUrl := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/admin/login?returnUrl="+returnUrl, http.StatusFound)
			return
		}
		handler(user, w, r)
	}
}

// requireAdmin requires a signed-in admin.
func (a *App) requireAdmin(handler func(user *CurrentUser, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return a.requireUser(func(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
		if !user.IsAdmin() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Forbidden: admin access required."))
			return
		}
		handler(user, w, r)
	})
}
