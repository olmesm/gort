package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

func TestAuthorFindIfExistsNeverReusesAnotherAuthorsLink(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	createShort(t, admin, `{"longUrl":"https://example.com/shared","customSlug":"private","title":"Private title","tags":["private-tag"],"group":"private-group"}`)
	author := app.client(t)
	author.headers["X-Api-Key"] = createAPIKey(t, app, core.AuthorRole())
	if r := author.get("/rest/v1/short-urls/private"); r.Code != 404 {
		t.Fatalf("foreign GET: %d", r.Code)
	}
	created := author.post("/rest/v1/short-urls", `{"longUrl":"https://example.com/shared","findIfExists":true}`)
	if created.Code != 201 || strings.Contains(created.Body.String(), "private") {
		t.Fatalf("foreign reuse leaked metadata: %d %s", created.Code, created.Body.String())
	}
	code := parseJSON(t, created.Body.String())["shortCode"]
	again := author.post("/rest/v1/short-urls", `{"longUrl":"https://example.com/shared","findIfExists":true}`)
	if again.Code != 201 || parseJSON(t, again.Body.String())["shortCode"] != code {
		t.Fatalf("own reuse failed: %d %s", again.Code, again.Body.String())
	}
	graph := graphOK(t, author, `mutation {createShortURL(input:{longUrl:"https://example.com/shared",findIfExists:true}) {shortCode title tags group}}`, nil)
	if graph["createShortURL"].(map[string]any)["shortCode"] != code {
		t.Fatalf("GraphQL reused foreign link: %v", graph)
	}
	other := app.client(t)
	other.headers["X-Api-Key"] = createAPIKey(t, app, core.AuthorRole())
	graph = graphOK(t, other, `mutation {createShortURL(input:{longUrl:"https://example.com/shared",findIfExists:true}) {shortCode title tags group}}`, nil)
	otherCode := graph["createShortURL"].(map[string]any)["shortCode"]
	if otherCode == code || otherCode == "private" {
		t.Fatalf("GraphQL reused foreign link: %v", graph)
	}
	// Admin and domain-scoped keys may still reuse any link inside their scope.
	domain, err := data.DefaultDomain(t.Context(), app.DB)
	if err != nil {
		t.Fatal(err)
	}
	domainClient := app.client(t)
	domainClient.headers["X-Api-Key"] = createAPIKey(t, app, core.DomainRole(domain.ID))
	for _, client := range []*testClient{admin, domainClient} {
		r := client.post("/rest/v1/short-urls", `{"longUrl":"https://example.com/shared","domain":"example.test","findIfExists":true}`)
		if r.Code != 201 || parseJSON(t, r.Body.String())["shortCode"] != "private" {
			t.Fatalf("authorized reuse: %d %s", r.Code, r.Body.String())
		}
	}
}

func TestDashboardSessionsRefreshDeletedUsersAndRoles(t *testing.T) {
	app := newTestApp(t)
	client := dashboardAdmin(t, app)
	user, err := data.UserByUsername(t.Context(), app.DB, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.UpdateUserRole(t.Context(), app.DB, user.ID, core.UserRegular); err != nil {
		t.Fatal(err)
	}
	if r := client.postForm("/admin/api-keys", "name=backdoor&role=admin"); r.Code != 403 {
		t.Fatalf("demoted session can mint admin keys: %d", r.Code)
	}
	if r := client.get("/admin/short-urls"); r.Code != 200 {
		t.Fatalf("demoted user lost regular access: %d", r.Code)
	}
	if _, err := data.DeleteUser(t.Context(), app.DB, user.ID); err != nil {
		t.Fatal(err)
	}
	if r := client.get("/admin/short-urls"); r.Code != 302 || !strings.HasPrefix(r.Header().Get("Location"), "/admin/login") {
		t.Fatalf("deleted session still works: %d", r.Code)
	}
}

func TestPasswordResetInvalidatesExistingLocalSessions(t *testing.T) {
	app := newTestApp(t)
	oldClient := dashboardAdmin(t, app)
	user, err := data.UserByUsername(t.Context(), app.DB, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if r := oldClient.get("/admin/users"); r.Code != 200 {
		t.Fatalf("initial session: %d", r.Code)
	}
	// Exercise the password-reset endpoint, then replay the original cookie.
	if r := oldClient.postForm("/admin/users/"+strconv.FormatInt(user.ID.Value(), 10)+"/password", "password=replacement-password-123"); r.Code != 302 {
		t.Fatalf("reset: %d", r.Code)
	}
	if r := oldClient.get("/admin/users"); r.Code != 302 || !strings.HasPrefix(r.Header().Get("Location"), "/admin/login") {
		t.Fatalf("old password session retained: %d", r.Code)
	}
	freshClient := app.client(t)
	login := freshClient.postForm("/admin/login", "username=admin&password=replacement-password-123")
	if login.Code != 302 || len(login.Result().Cookies()) == 0 {
		t.Fatalf("replacement login: %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	freshClient.headers["Cookie"] = cookie.String()
	if r := freshClient.get("/admin/users"); r.Code != 200 {
		t.Fatalf("fresh session: %d", r.Code)
	}
	session := app.verifySession(cookie.Value)
	if session == nil || session.PasswordVersion == "" {
		t.Fatal("new cookie has no password version")
	}
	// Legacy local cookies without the version must also reauthenticate.
	session.PasswordVersion = ""
	payload, _ := json.Marshal(session)
	freshClient.headers["Cookie"] = sessionCookieName + "=" + app.signSession(payload)
	if r := freshClient.get("/admin/users"); r.Code != 302 {
		t.Fatalf("legacy session retained: %d", r.Code)
	}
}

func TestGroupUsersCannotAccessGlobalDashboardResources(t *testing.T) {
	idp := newFakeIdp(t)
	app := newTestAppWithOIDC(t, idp, nil)
	admin := app.adminClient(t)
	createShort(t, admin, `{"longUrl":"https://example.com/private","customSlug":"private","group":"private","tags":["private-tag"]}`)
	createShort(t, admin, `{"longUrl":"https://example.com/own","customSlug":"own","group":"team"}`)
	client := withSession(app.client(t), oidcLogin(t, app, idp, "member", "member", []string{"team"}))
	for _, path := range []string{"/admin/tags", "/admin/visits/orphan"} {
		if r := client.get(path); r.Code != 403 || strings.Contains(r.Body.String(), "private-tag") {
			t.Fatalf("global resource %s: %d", path, r.Code)
		}
	}
	for _, operation := range []struct{ path, form string }{
		{"/admin/tags/rename", "oldName=private-tag&newName=stolen"},
		{"/admin/tags/delete", "name=private-tag"},
	} {
		if r := client.postForm(operation.path, operation.form); r.Code != 403 {
			t.Fatalf("global mutation %s: %d", operation.path, r.Code)
		}
	}
	if exists, err := data.TagExists(t.Context(), app.DB, "private-tag"); err != nil || !exists {
		t.Fatalf("foreign tag changed: %v", err)
	}
	if r := client.get("/admin"); r.Code != 302 || r.Header().Get("Location") != "/admin/short-urls" {
		t.Fatalf("global overview exposed: %d", r.Code)
	}
	r := client.get("/admin/short-urls")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "example.com/own") || strings.Contains(r.Body.String(), "example.com/private") {
		t.Fatalf("scoped list changed: %d", r.Code)
	}
	for _, path := range []string{"/admin/tags", "/admin/domains", "/admin/visits/orphan"} {
		if strings.Contains(r.Body.String(), `href="`+path+`"`) {
			t.Fatalf("inaccessible nav link: %s", path)
		}
	}
}

func TestOIDCSessionsExpireWithVerifiedToken(t *testing.T) {
	idp := newFakeIdp(t)
	app := newTestAppWithOIDC(t, idp, nil)
	cookie := oidcLogin(t, app, idp, "member", "member", []string{"team"})
	session := app.verifySession(cookie)
	idp.mu.Lock()
	expires := idp.nextClaims["exp"].(int64)
	idp.mu.Unlock()
	if session == nil || session.Expires != expires || session.OIDCExpires != expires {
		t.Fatalf("OIDC lifetime not capped: %+v", session)
	}
	// A cookie from before token-expiry enforcement must require a new login.
	session.OIDCExpires = 0
	session.Expires = time.Now().Add(sessionLifetime).Unix()
	payload, _ := json.Marshal(session)
	client := withSession(app.client(t), app.signSession(payload))
	if r := client.get("/admin/short-urls"); r.Code != 302 {
		t.Fatalf("legacy OIDC grants retained: %d", r.Code)
	}
}

func TestSecureCookiesMatchConfiguredHTTPS(t *testing.T) {
	for _, secure := range []bool{false, true} {
		app := newTestApp(t)
		app.Cfg.UseHTTPS = secure
		user, err := data.UserByUsername(t.Context(), app.DB, "admin")
		if err != nil {
			t.Fatal(err)
		}
		for _, issue := range []func(http.ResponseWriter){func(w http.ResponseWriter) { app.SignIn(w, user) }, app.SignOut, app.clearOIDCState} {
			r := httptest.NewRecorder()
			issue(r)
			cookie := r.Result().Cookies()[0]
			if cookie.Secure != secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("cookie flags: %+v", cookie)
			}
		}
	}
}

func TestSQLInjectionPayloadsRemainValues(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)
	createShort(t, client, `{"longUrl":"https://example.com/retained","customSlug":"retained","tags":["retained"]}`)
	payload := `' OR 1=1; DROP TABLE short_urls; --`
	for _, field := range []string{"searchTerm", "group", "domain", "tags[]"} {
		r := client.get("/rest/v1/short-urls?" + url.Values{field: {payload}}.Encode())
		if r.Code != 200 || len(parseJSON(t, r.Body.String())["data"].([]any)) != 0 {
			t.Fatalf("%s injection changed query: %d %s", field, r.Code, r.Body.String())
		}
	}
	if r := client.get("/rest/v1/short-urls?" + url.Values{"orderBy": {payload}}.Encode()); r.Code != 200 {
		t.Fatalf("orderBy not allowlisted: %d", r.Code)
	}
	if r := client.delete("/rest/v1/tags?" + url.Values{"tags[]": {payload}}.Encode()); r.Code != 200 {
		t.Fatalf("tag literal delete: %d", r.Code)
	}
	if r := client.get("/rest/v1/short-urls/retained"); r.Code != 200 || !strings.Contains(r.Body.String(), `"tags":["retained"]`) {
		t.Fatalf("injection modified stored data: %d %s", r.Code, r.Body.String())
	}
	if _, err := data.Breakdown(t.Context(), app.DB, data.GlobalScope(), payload, nil, nil, 10); err == nil {
		t.Fatal("untrusted SQL column accepted")
	}
}
