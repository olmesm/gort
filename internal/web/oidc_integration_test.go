package web

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIdp is a minimal in-process OIDC provider: discovery document, JWKS
// and a token endpoint that returns an RS256-signed ID token with whatever
// claims the test configured.
type fakeIdp struct {
	t      *testing.T
	key    *rsa.PrivateKey
	server *httptest.Server

	mu         sync.Mutex
	nextClaims map[string]any
}

func newFakeIdp(t *testing.T) *fakeIdp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdp{t: t, key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := idp.server.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/auth",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		pub := &idp.key.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "test",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		claims := idp.nextClaims
		idp.mu.Unlock()
		if claims == nil {
			http.Error(w, "no token prepared", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   300,
			"id_token":     idp.signToken(claims),
		})
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// prepareToken sets the claims of the next ID token (iss/aud/exp/iat are
// filled in automatically).
func (idp *fakeIdp) prepareToken(clientID, nonce, sub, username string, groups []string) {
	claims := map[string]any{
		"iss":                idp.server.URL,
		"aud":                clientID,
		"sub":                sub,
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"iat":                time.Now().Unix(),
		"nonce":              nonce,
		"preferred_username": username,
	}
	if groups != nil {
		claims["groups"] = groups
	}
	idp.mu.Lock()
	idp.nextClaims = claims
	idp.mu.Unlock()
}

func (idp *fakeIdp) signToken(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	if err != nil {
		idp.t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// newTestAppWithOidc boots the app against the fake IdP.
func newTestAppWithOidc(t *testing.T, idp *fakeIdp, extra map[string]string) *App {
	t.Helper()
	dataDir := t.TempDir()
	vars := map[string]string{
		"DEFAULT_DOMAIN":         "example.test",
		"DB_CONNECTION":          dataDir + "/test.db",
		"DATA_DIR":               dataDir,
		"AUTO_RESOLVE_TITLES":    "false",
		"INITIAL_ADMIN_USERNAME": "admin",
		"INITIAL_ADMIN_PASSWORD": "test-password-123",
		"RATE_LIMIT_PER_MINUTE":  "10000",
		"OIDC_ISSUER":            idp.server.URL,
		"OIDC_CLIENT_ID":         "gort-dashboard",
		"OIDC_CLIENT_SECRET":     "secret",
	}
	for k, v := range extra {
		vars[k] = v
	}
	cfg, err := ConfigFromLookup(func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := NewApp(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	// Hermetic HTTP for discovery/JWKS/token: never touch a proxy.
	app.oidc.httpClient = &http.Client{Transport: &http.Transport{Proxy: nil}}
	t.Cleanup(func() { _ = app.Db.Close() })
	return app
}

// oidcLogin drives the full authorization-code flow against the fake IdP and
// returns the resulting session cookie value.
func oidcLogin(t *testing.T, app *App, idp *fakeIdp, sub, username string, groups []string) string {
	t.Helper()
	client := app.client(t)

	start := client.get("/admin/oidc/login?returnUrl=/admin")
	if start.Code != http.StatusFound {
		t.Fatalf("login start status %d: %s", start.Code, start.Body.String())
	}
	authUrl, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authUrl.Query().Get("state")
	nonce := authUrl.Query().Get("nonce")
	if state == "" || nonce == "" || authUrl.Query().Get("code_challenge") == "" {
		t.Fatalf("auth URL missing state/nonce/PKCE: %s", authUrl)
	}
	if got := authUrl.Query().Get("redirect_uri"); got != "http://example.test/admin/oidc/callback" {
		t.Fatalf("redirect_uri: %s", got)
	}

	var stateCookie string
	for _, cookie := range start.Result().Cookies() {
		if cookie.Name == "gort_oidc" {
			stateCookie = cookie.Value
		}
	}
	if stateCookie == "" {
		t.Fatal("no gort_oidc state cookie set")
	}

	idp.prepareToken("gort-dashboard", nonce, sub, username, groups)

	callback := client.getWithHeaders(
		"/admin/oidc/callback?code=fake-code&state="+url.QueryEscape(state),
		map[string]string{"Cookie": "gort_oidc=" + stateCookie})
	if callback.Code != http.StatusFound {
		t.Fatalf("callback status %d: %s", callback.Code, callback.Body.String())
	}
	if loc := callback.Header().Get("Location"); loc != "/admin" {
		t.Fatalf("callback redirect: %s", loc)
	}
	var sessionCookie string
	for _, cookie := range callback.Result().Cookies() {
		if cookie.Name == "gort_session" && cookie.Value != "" {
			sessionCookie = cookie.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("no session cookie after callback")
	}
	return sessionCookie
}

func withSession(client *testClient, session string) *testClient {
	client.headers["Cookie"] = "gort_session=" + session
	return client
}

func TestOidcLoginProvisionsUserAndGrantsAdminByGroup(t *testing.T) {
	idp := newFakeIdp(t)
	app := newTestAppWithOidc(t, idp, nil)

	// Member of the admin group → dashboard admin.
	adminSession := oidcLogin(t, app, idp, "sub-admin", "alice", []string{"/gort-admins", "/marketing"})
	adminClient := withSession(app.client(t), adminSession)
	overview := adminClient.get("/admin")
	if overview.Code != http.StatusOK || !strings.Contains(overview.Body.String(), "alice") {
		t.Fatalf("admin overview: %d", overview.Code)
	}
	users := adminClient.get("/admin/users")
	if users.Code != http.StatusOK {
		t.Fatalf("admin should reach /admin/users, got %d", users.Code)
	}
	if !strings.Contains(users.Body.String(), "alice") {
		t.Error("provisioned user missing from users page")
	}

	// Plain member → regular user, no admin pages.
	userSession := oidcLogin(t, app, idp, "sub-user", "bob", []string{"/marketing"})
	userClient := withSession(app.client(t), userSession)
	if resp := userClient.get("/admin"); resp.Code != http.StatusOK {
		t.Fatalf("user overview: %d", resp.Code)
	}
	if resp := userClient.get("/admin/users"); resp.Code != http.StatusForbidden {
		t.Fatalf("user should be forbidden from /admin/users, got %d", resp.Code)
	}
}

func TestOidcCallbackRejectsBadStateAndNonce(t *testing.T) {
	idp := newFakeIdp(t)
	app := newTestAppWithOidc(t, idp, nil)
	client := app.client(t)

	// No state cookie at all.
	resp := client.get("/admin/oidc/callback?code=x&state=y")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("missing-state status %d", resp.Code)
	}

	// Mismatched nonce inside a valid state.
	start := client.get("/admin/oidc/login")
	authUrl, _ := url.Parse(start.Header().Get("Location"))
	state := authUrl.Query().Get("state")
	var stateCookie string
	for _, cookie := range start.Result().Cookies() {
		if cookie.Name == "gort_oidc" {
			stateCookie = cookie.Value
		}
	}
	idp.prepareToken("gort-dashboard", "wrong-nonce", "sub-x", "mallory", nil)
	callback := client.getWithHeaders(
		"/admin/oidc/callback?code=fake-code&state="+url.QueryEscape(state),
		map[string]string{"Cookie": "gort_oidc=" + stateCookie})
	if callback.Code != http.StatusUnauthorized ||
		!strings.Contains(callback.Body.String(), "expired or was tampered with") {
		t.Fatalf("bad-nonce status %d: %s", callback.Code, callback.Body.String())
	}
}

func TestOidcOnlyDisablesPasswordLogin(t *testing.T) {
	idp := newFakeIdp(t)
	app := newTestAppWithOidc(t, idp, map[string]string{"OIDC_ONLY": "true"})
	client := app.client(t)

	form := client.get("/admin/login")
	if strings.Contains(form.Body.String(), `name="password"`) {
		t.Error("password form should be hidden in OIDC-only mode")
	}
	if !strings.Contains(form.Body.String(), "/admin/oidc/login") {
		t.Error("SSO button missing")
	}

	login := client.do(http.MethodPost, "/admin/login", "")
	if login.Code != http.StatusForbidden {
		t.Fatalf("password login should be forbidden, got %d", login.Code)
	}
}

func TestGroupScopingInDashboard(t *testing.T) {
	idp := newFakeIdp(t)
	app := newTestAppWithOidc(t, idp, nil)
	apiAdmin := app.adminClient(t)

	// Seed one ungrouped link and one per team.
	mk := func(slug, group string) {
		body := fmt.Sprintf(`{"longUrl":"https://example.com/%s","customSlug":"%s"`, slug, slug)
		if group != "" {
			body += fmt.Sprintf(`,"group":"%s"`, group)
		}
		body += "}"
		resp := apiAdmin.post("/rest/v1/short-urls", body)
		if resp.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d %s", slug, resp.Code, resp.Body.String())
		}
	}
	mk("open-link", "")
	mk("team-a-link", "team-a")
	mk("team-b-link", "team-b")

	session := oidcLogin(t, app, idp, "sub-a", "ana", []string{"/team-a"})
	client := withSession(app.client(t), session)

	// List: sees ungrouped + own group, not the other team's.
	list := client.get("/admin/short-urls")
	body := list.Body.String()
	if !strings.Contains(body, "open-link") || !strings.Contains(body, "team-a-link") {
		t.Error("visible links missing from list")
	}
	if strings.Contains(body, "team-b-link") {
		t.Error("foreign-group link leaked into list")
	}

	// Resolve ids via an admin API listing.
	idOf := func(slug string) string {
		resp := apiAdmin.get("/rest/v1/short-urls?searchTerm=" + slug)
		var doc struct {
			Data []struct {
				ShortCode string `json:"shortCode"`
			} `json:"data"`
		}
		_ = json.Unmarshal(resp.Body.Bytes(), &doc)
		if len(doc.Data) != 1 {
			t.Fatalf("idOf(%s): %d matches", slug, len(doc.Data))
		}
		return doc.Data[0].ShortCode
	}
	_ = idOf // codes are the slugs themselves; edit pages need numeric ids

	editUrlFor := func(slug string) string {
		// The list page links to /admin/short-urls/{id}/edit; scrape it from
		// the admin's full listing.
		adminSession := oidcLogin(t, app, idp, "sub-root", "root", []string{"gort-admins"})
		adminUi := withSession(app.client(t), adminSession)
		page := adminUi.get("/admin/short-urls?search=" + slug).Body.String()
		marker := "/edit"
		idx := strings.Index(page, marker)
		if idx < 0 {
			t.Fatalf("no edit link found for %s", slug)
		}
		start := strings.LastIndex(page[:idx], "/admin/short-urls/")
		return page[start : idx+len(marker)]
	}

	// Edit page of the foreign link is a 404; own group opens.
	foreignEdit := editUrlFor("team-b-link")
	if resp := client.get(foreignEdit); resp.Code != http.StatusNotFound {
		t.Fatalf("foreign edit page: %d", resp.Code)
	}
	ownEdit := editUrlFor("team-a-link")
	if resp := client.get(ownEdit); resp.Code != http.StatusOK {
		t.Fatalf("own edit page: %d", resp.Code)
	}

	// Creating into a foreign group is rejected; own group works.
	createBody := "longUrl=" + url.QueryEscape("https://example.com/new") +
		"&customSlug=scoped-new&redirectStatus=302&forwardQuery=true&group=team-b"
	forbidden := client.postForm("/admin/short-urls/new", createBody)
	if forbidden.Code != http.StatusBadRequest ||
		!strings.Contains(forbidden.Body.String(), "member of") {
		t.Fatalf("foreign-group create: %d %s", forbidden.Code, forbidden.Body.String())
	}
	allowed := client.postForm("/admin/short-urls/new",
		strings.Replace(createBody, "group=team-b", "group=team-a", 1))
	if allowed.Code != http.StatusFound {
		t.Fatalf("own-group create: %d %s", allowed.Code, allowed.Body.String())
	}
}

func TestApiGroupFieldAndFilter(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	created := parseJson(t, client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/grouped","customSlug":"grouped","group":"/ops"}`).Body.String())
	if created["group"] != "ops" {
		t.Fatalf("group normalized: %v", created["group"])
	}
	client.post("/rest/v1/short-urls", `{"longUrl":"https://example.com/plain","customSlug":"plain"}`)

	// Filter to one group.
	list := parseJson(t, client.get("/rest/v1/short-urls?group=ops").Body.String())
	if items := list["data"].([]any); len(items) != 1 {
		t.Fatalf("group filter items: %d", len(items))
	}
	// group= (empty) filters to ungrouped links.
	ungrouped := parseJson(t, client.get("/rest/v1/short-urls?group=").Body.String())
	items := ungrouped["data"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["shortCode"] != "plain" {
		t.Fatalf("ungrouped filter items: %v", items)
	}

	// PATCH can move and clear the group; null clears.
	moved := parseJson(t, client.patch("/rest/v1/short-urls/grouped", `{"group":"platform"}`).Body.String())
	if moved["group"] != "platform" {
		t.Fatalf("moved group: %v", moved["group"])
	}
	cleared := parseJson(t, client.patch("/rest/v1/short-urls/grouped", `{"group":null}`).Body.String())
	if _, has := cleared["group"]; has {
		t.Fatalf("group should be cleared: %v", cleared["group"])
	}
}
